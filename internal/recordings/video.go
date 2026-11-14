package recordings

import (
	"context"
	"fmt"
	"math"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"time"
)

// Video mode (design D5): layout spans of at least minDwell, each encoded as one segment (the span's name tiles as
// a background, cameras and the shared screen over their slots), concatenated video-only and muxed with the
// audio master.

// person is someone heard or seen in the recording, in order of arrival.
type person struct {
	id, name string
	audio    []placed
	camera   []placed
	screen   []placed
	first    int64
}

// span is a stretch with one layout.
type span struct {
	start, end int64
	screen     *person // whose screen fills the frame
	people     []*person
}

func gatherPeople(dir string, tracks []*Track, total int64) []*person {
	byID := map[string]*person{}
	segs := map[string]map[string][]Segment{} // id → source → segments
	var order []string
	for _, t := range tracks {
		p := byID[t.Participant]
		if p == nil {
			p = &person{id: t.Participant, name: t.Name, first: math.MaxInt64}
			byID[t.Participant] = p
			segs[t.Participant] = map[string][]Segment{}
			order = append(order, t.Participant)
		}
		if p.name == "" {
			p.name = t.Name
		}
		key := t.Kind
		if t.Kind == KindVideo {
			key = t.Source
		}
		segs[t.Participant][key] = append(segs[t.Participant][key], t.Segments...)
	}
	var out []*person
	for _, id := range order {
		p := byID[id]
		p.audio = layOut(dir, segs[id][KindAudio], total)
		p.camera = layOut(dir, segs[id][SourceCamera], total)
		p.screen = layOut(dir, segs[id][SourceScreen], total)
		for _, list := range [][]placed{p.audio, p.camera, p.screen} {
			if len(list) > 0 {
				p.first = min(p.first, list[0].start)
			}
		}
		if p.first != math.MaxInt64 {
			out = append(out, p)
		}
	}
	slices.SortStableFunc(out, func(a, b *person) int { return int(a.first - b.first) })
	return out
}

func activeAt(list []placed, at int64) *placed {
	for i := range list {
		if list[i].start <= at && at < list[i].end {
			return &list[i]
		}
	}
	return nil
}

func overlaps(list []placed, a, b int64) bool {
	for _, p := range list {
		if p.start < b && a < p.end {
			return true
		}
	}
	return false
}

// spans cuts [0, total) where a camera or a screen starts or stops, never more often than minDwell, at most
// maxSegments (the rest is one span).
func spans(people []*person, total int64) []span {
	var cuts []int64
	for _, p := range people {
		for _, list := range [][]placed{p.camera, p.screen} {
			for _, f := range list {
				cuts = append(cuts, f.start, f.end)
			}
		}
	}
	cuts = append(cuts, total)
	slices.Sort(cuts)
	cuts = slices.Compact(cuts)
	dwell := minDwell.Milliseconds()
	var out []span
	for start := int64(0); start < total; {
		end := total
		if len(out) < maxSegments-1 {
			for _, c := range cuts {
				if c >= start+dwell && c < total {
					end = c
					break
				}
			}
		}
		s := span{start: start, end: end}
		var screenStart int64 = math.MaxInt64
		for _, p := range people {
			if overlaps(p.audio, start, end) || overlaps(p.camera, start, end) || overlaps(p.screen, start, end) {
				s.people = append(s.people, p)
			}
			if f := activeAt(p.screen, start); f != nil && f.start < screenStart {
				s.screen, screenStart = p, f.start
			}
		}
		out = append(out, s)
		start = end
	}
	return out
}

// slots places the span's people: the screen on the left with up to four cameras in a strip, or a grid of up to
// nine.
func (s *span) slots() (screen Slot, tiles []Slot, shown []*person) {
	if s.screen != nil {
		stripW := videoW / 4
		screen = Slot{X: 0, Y: 0, W: videoW - stripW, H: videoH}
		h := videoH / 4
		for i, p := range s.people[:min(4, len(s.people))] {
			tiles = append(tiles, Slot{X: videoW - stripW, Y: i * h, W: stripW, H: h, Name: p.name, Key: p.id})
			shown = append(shown, p)
		}
		return screen, tiles, shown
	}
	n := min(9, len(s.people))
	if n == 0 {
		return screen, nil, nil
	}
	cols := int(math.Ceil(math.Sqrt(float64(n))))
	rows := (n + cols - 1) / cols
	w, h := videoW/cols, videoH/rows
	for i, p := range s.people[:n] {
		tiles = append(tiles, Slot{X: (i % cols) * w, Y: (i / cols) * h, W: w, H: h, Name: p.name, Key: p.id})
		shown = append(shown, p)
	}
	return screen, tiles, shown
}

// videoCodec is the encoding of every segment (identical, so they concatenate) and the container's extension.
func (c *Composer) videoCodec() ([]string, string) {
	if c.Format == "mp4" {
		return []string{"-c:v", "libx264", "-preset", "veryfast", "-b:v", videoRate, "-maxrate", videoRate,
			"-bufsize", "4M", "-g", "50", "-pix_fmt", "yuv420p"}, "mp4"
	}
	return []string{"-c:v", "libvpx", "-b:v", videoRate, "-maxrate", videoRate, "-bufsize", "4M",
		"-deadline", "realtime", "-cpu-used", "8", "-g", "50", "-pix_fmt", "yuv420p"}, "webm"
}

// segmentArgs encodes one span: its background with each shown source laid over its slot from the right offset;
// a source that ends early leaves its name tile (or the dark background) underneath.
func (c *Composer) segmentArgs(s span, bg, out string) []string {
	d := s.end - s.start
	args := []string{"-loop", "1", "-framerate", fmt.Sprint(frameRate), "-t", sec(d), "-i", bg}
	graph := []string{"[0:v]format=yuv420p,setsar=1[b0]"}
	screen, tiles, shown := s.slots()
	type layer struct {
		f    *placed
		slot Slot
	}
	var layers []layer
	if s.screen != nil {
		layers = append(layers, layer{activeAt(s.screen.screen, s.start), screen})
	}
	for i, p := range shown {
		if f := activeAt(p.camera, s.start); f != nil {
			layers = append(layers, layer{f, tiles[i]})
		}
	}
	last := "b0"
	n := 0
	for _, l := range layers {
		if l.f == nil {
			continue
		}
		n++
		args = append(args, "-ss", sec(s.start-l.f.start), "-t", sec(min(d, l.f.end-s.start)), "-i", l.f.path)
		graph = append(graph,
			fmt.Sprintf("[%d:v]fps=%d,scale=%d:%d:force_original_aspect_ratio=decrease,setsar=1,setpts=PTS-STARTPTS[v%d]",
				n, frameRate, l.slot.W-4, l.slot.H-4, n),
			fmt.Sprintf("[%s][v%d]overlay=x=%d+(%d-w)/2:y=%d+(%d-h)/2:eof_action=pass[b%d]",
				last, n, l.slot.X, l.slot.W, l.slot.Y, l.slot.H, n))
		last = fmt.Sprintf("b%d", n)
	}
	codec, _ := c.videoCodec()
	args = append(args, "-filter_complex", strings.Join(graph, ";"), "-map", "["+last+"]", "-an",
		"-r", fmt.Sprint(frameRate), "-t", sec(d))
	return append(append(args, codec...), "-threads", ffmpegThreads, out)
}

func (c *Composer) composeVideo(ctx context.Context, dir string, tracks []*Track, total int64, names map[string]string) (*Output, error) {
	limit := timeLimit(time.Duration(total) * time.Millisecond)
	_, ext := c.videoCodec()
	master := filepath.Join(dir, "master.ogg")
	masterCodec := []string{"-c:a", "libopus", "-b:a", masterRate}
	if ext == "mp4" {
		master = filepath.Join(dir, "master.m4a")
		masterCodec = []string{"-c:a", "aac", "-b:a", masterRate}
	}
	silent, err := c.master(ctx, dir, tracks, total, master, masterCodec)
	if err != nil {
		return nil, err
	}
	people := gatherPeople(dir, tracks, total)
	for _, p := range people {
		if n := names[p.id]; n != "" {
			p.name = n
		}
	}
	var list strings.Builder
	var segs []string
	for i, s := range spans(people, total) {
		_, tiles, _ := s.slots()
		bg := filepath.Join(dir, fmt.Sprintf("bg%03d.png", i))
		if err := DrawBackground(bg, videoW, videoH, tiles); err != nil {
			return nil, err
		}
		seg := filepath.Join(dir, fmt.Sprintf("seg%03d.%s", i, ext))
		if err := c.FFmpeg.Run(ctx, dir, fmt.Sprintf("segment%03d", i), limit, c.segmentArgs(s, bg, seg)...); err != nil {
			return nil, err
		}
		_ = os.Remove(bg)
		segs = append(segs, seg)
		fmt.Fprintf(&list, "file '%s'\n", filepath.Base(seg))
	}
	listPath := filepath.Join(dir, "segments.txt")
	if err := os.WriteFile(listPath, []byte(list.String()), 0o640); err != nil {
		return nil, fmt.Errorf("segment list: %w", err)
	}
	joined := filepath.Join(dir, "video."+ext)
	if err := c.FFmpeg.Run(ctx, dir, "concat", limit,
		"-f", "concat", "-safe", "0", "-i", listPath, "-c", "copy", joined); err != nil {
		return nil, err
	}
	removeAll(append(segs, listPath)...)
	out := filepath.Join(dir, "recording."+ext)
	muxArgs := []string{"-i", joined, "-i", master, "-map", "0:v", "-map", "1:a", "-c", "copy", "-t", sec(total)}
	mime := "video/webm"
	if ext == "mp4" {
		muxArgs = append(muxArgs, "-movflags", "+faststart")
		mime = "video/mp4"
	}
	if err := c.FFmpeg.Run(ctx, dir, "mux", limit, append(muxArgs, out)...); err != nil {
		return nil, err
	}
	removeAll(joined, master)
	return &Output{Path: out, Name: "recording." + ext, MimeType: mime, Silent: silent}, nil
}
