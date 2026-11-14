package recordings

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"time"
)

// Composition (design D5): each participant's audio files flattened into one track in batches, the tracks mixed
// into one master, and in video mode the layout spans encoded as segments, concatenated and muxed with the master.

// Composition limits and encodings.
const (
	maxBatch        = 50 // files per ffmpeg run when flattening
	maxMixed        = 25 // participant tracks in the mix (= MaxTracks)
	participantRate = "64k"
	masterRate      = "128k"
	videoRate       = "2M"
	frameRate       = 25
	videoW, videoH  = 1280, 720
	minDwell        = 10 * time.Second
	maxSegments     = 200
)

// Output is a composed recording.
type Output struct {
	Path     string
	Name     string // the file's name in the channel
	MimeType string
	Silent   bool // nobody spoke: the file holds silence
}

// Composer turns a capture directory into the recording's file.
type Composer struct {
	FFmpeg *FFmpeg
	Mode   string // audio or video
	Format string // webm or mp4, video mode
}

// LoadTracks reads every manifest of a capture directory, in the order they were created.
func LoadTracks(dir string) ([]*Track, error) {
	names, err := filepath.Glob(filepath.Join(dir, "t*.jsonl"))
	if err != nil {
		return nil, err
	}
	slices.Sort(names)
	out := make([]*Track, 0, len(names))
	for _, n := range names {
		t, err := ReadManifest(n)
		if err != nil {
			continue // a manifest torn before its header: that track has nothing to give
		}
		out = append(out, t)
	}
	return out, nil
}

// placed is a media file with its span on the recording (ms), after clamping and overlap trimming; skip is how
// far into the file that span begins.
type placed struct {
	path       string
	start, end int64
	skip       int64
}

// layOut places a track kind's files of one participant (given in capture order) on [0, total): a file captured
// later owns any overlap with an earlier one (a backward re-anchor, design D4) — the earlier ends where the later
// begins —, and a file that begins before the recording skips its first part.
func layOut(dir string, segs []Segment, total int64) []placed {
	var out []placed
	for i, s := range segs {
		end := s.PosMS + s.DurMS
		for _, later := range segs[i+1:] {
			if later.PosMS < end {
				end = later.PosMS
			}
		}
		start := max(s.PosMS, 0)
		end = min(end, total)
		if end-start < 20 {
			continue
		}
		out = append(out, placed{path: filepath.Join(dir, s.File), start: start, end: end, skip: start - s.PosMS})
	}
	slices.SortStableFunc(out, func(a, b placed) int { return int(a.start - b.start) })
	return out
}

// participantAudio groups the audio files by participant, in the order participants first appear.
func participantAudio(dir string, tracks []*Track, total int64) [][]placed {
	byID := map[string][]Segment{}
	var order []string
	for _, t := range tracks {
		if t.Kind != KindAudio || len(t.Segments) == 0 {
			continue
		}
		if _, ok := byID[t.Participant]; !ok {
			order = append(order, t.Participant)
		}
		byID[t.Participant] = append(byID[t.Participant], t.Segments...)
	}
	var out [][]placed
	for _, id := range order {
		if p := layOut(dir, byID[id], total); len(p) > 0 {
			out = append(out, p)
		}
	}
	return out
}

func sec(ms int64) string { return strconv.FormatFloat(float64(ms)/1000, 'f', 3, 64) }

// flattenArgs builds one ffmpeg run that places files on [from, to) (each delayed to its start and trimmed to its
// span) and sums them into one mono track of to−from, as Opus at participantRate.
func flattenArgs(files []placed, from, to int64, out string) []string {
	var args, chains []string
	mix := ""
	for i, f := range files {
		args = append(args, "-i", f.path)
		chains = append(chains, fmt.Sprintf(
			"[%d:a]atrim=start=%s:end=%s,asetpts=PTS-STARTPTS,aresample=48000,aformat=sample_fmts=fltp:channel_layouts=mono,adelay=delays=%dS:all=1[a%d]",
			i, sec(f.skip), sec(f.skip+f.end-f.start), (f.start-from)*48, i))
		mix += fmt.Sprintf("[a%d]", i)
	}
	graph := strings.Join(chains, ";") + fmt.Sprintf(
		";%samix=inputs=%d:normalize=0:duration=longest,apad=whole_dur=%s,atrim=end=%s[out]",
		mix, len(files), sec(to-from), sec(to-from))
	return append(args, "-filter_complex", graph, "-map", "[out]", "-ac", "1", "-c:a", "libopus",
		"-b:a", participantRate, "-threads", ffmpegThreads, out)
}

// part is an intermediate track covering [from, to) of the recording (ms).
type part struct {
	path     string
	from, to int64
}

// sumArgs mixes parts, each delayed to its start relative to from, without attenuation into one track of to−from.
func sumArgs(parts []part, from, to int64, out string) []string {
	var args, chains []string
	mix := ""
	for i, p := range parts {
		args = append(args, "-i", p.path)
		chains = append(chains, fmt.Sprintf("[%d:a]adelay=delays=%dS:all=1[a%d]", i, (p.from-from)*48, i))
		mix += fmt.Sprintf("[a%d]", i)
	}
	graph := strings.Join(chains, ";") + fmt.Sprintf(
		";%samix=inputs=%d:normalize=0:duration=longest,apad=whole_dur=%s,atrim=end=%s[out]",
		mix, len(parts), sec(to-from), sec(to-from))
	return append(args, "-filter_complex", graph, "-map", "[out]", "-ac", "1", "-c:a", "libopus",
		"-b:a", participantRate, "-threads", ffmpegThreads, out)
}

// flatten turns one participant's files into one track of the recording's length (design D5): at most maxBatch
// files per run, each batch covering only its own stretch, then the batches summed in rounds of at most maxBatch;
// consumed batches are removed.
func (c *Composer) flatten(ctx context.Context, dir string, n int, files []placed, total int64, limit time.Duration) (string, error) {
	out := filepath.Join(dir, fmt.Sprintf("p%02d.ogg", n))
	if len(files) <= maxBatch {
		return out, c.FFmpeg.Run(ctx, dir, fmt.Sprintf("flatten-p%02d", n), limit, flattenArgs(files, 0, total, out)...)
	}
	var parts []part
	for i := 0; i < len(files); i += maxBatch {
		batch := files[i:min(i+maxBatch, len(files))]
		p := part{filepath.Join(dir, fmt.Sprintf("p%02d-b%03d.ogg", n, len(parts))), batch[0].start, batch[len(batch)-1].end}
		if err := c.FFmpeg.Run(ctx, dir, fmt.Sprintf("flatten-p%02d-b%03d", n, len(parts)), limit,
			flattenArgs(batch, p.from, p.to, p.path)...); err != nil {
			return "", err
		}
		parts = append(parts, p)
	}
	for round := 0; len(parts) > 1; round++ {
		last := len(parts) <= maxBatch
		var next []part
		for i := 0; i < len(parts); i += maxBatch {
			group := parts[i:min(i+maxBatch, len(parts))]
			p := part{out, 0, total}
			if !last { // another round follows: this group covers its own stretch
				p = part{filepath.Join(dir, fmt.Sprintf("p%02d-r%d-%03d.ogg", n, round, len(next))), group[0].from, group[0].to}
				for _, g := range group {
					p.to = max(p.to, g.to)
				}
			}
			if err := c.FFmpeg.Run(ctx, dir, fmt.Sprintf("sum-p%02d-r%d-%03d", n, round, len(next)), limit,
				sumArgs(group, p.from, p.to, p.path)...); err != nil {
				return "", err
			}
			for _, g := range group {
				removeAll(g.path)
			}
			next = append(next, p)
		}
		parts = next
	}
	return out, nil
}

func removeAll(paths ...string) {
	for _, p := range paths {
		_ = os.Remove(p)
	}
}

// master mixes the participants' tracks into the recording's one audio stream, loudness-normalized, as the codec
// arguments say; with nobody heard it is silence of the recording's length.
func (c *Composer) master(ctx context.Context, dir string, tracks []*Track, total int64, out string, codec []string) (bool, error) {
	limit := timeLimit(time.Duration(total) * time.Millisecond)
	parts := participantAudio(dir, tracks, total)
	if len(parts) > maxMixed {
		parts = parts[:maxMixed] // the capture takes at most MaxTracks tracks
	}
	var flat []string
	for i, p := range parts {
		f, err := c.flatten(ctx, dir, i, p, total, limit)
		if err != nil {
			return false, err
		}
		flat = append(flat, f)
	}
	var args []string
	var graph string
	if len(flat) == 0 {
		graph = fmt.Sprintf("anullsrc=r=48000:cl=mono,atrim=end=%s[out]", sec(total))
	} else {
		mix := ""
		for i, f := range flat {
			args = append(args, "-i", f)
			mix += fmt.Sprintf("[%d:a]", i)
		}
		graph = fmt.Sprintf(
			"%samix=inputs=%d:normalize=0:duration=longest,loudnorm=I=-16:TP=-1.5:LRA=11,aresample=48000,apad=whole_dur=%s,atrim=end=%s[out]",
			mix, len(flat), sec(total), sec(total))
	}
	args = append(args, "-filter_complex", graph, "-map", "[out]", "-ac", "1")
	args = append(append(args, codec...), "-threads", ffmpegThreads, out)
	err := c.FFmpeg.Run(ctx, dir, "master", limit, args...)
	removeAll(flat...)
	return len(flat) == 0, err
}

// Compose produces the recording of the capture in dir, total ms long, under dir/out.
func (c *Composer) Compose(ctx context.Context, dir string, total int64, names map[string]string) (*Output, error) {
	dir, err := filepath.Abs(dir) // ffmpeg runs inside dir: the paths it is given must be absolute
	if err != nil {
		return nil, err
	}
	tracks, err := LoadTracks(dir)
	if err != nil {
		return nil, err
	}
	total = max(total, 1000)
	if c.Mode != "video" {
		out := filepath.Join(dir, "recording.m4a")
		silent, err := c.master(ctx, dir, tracks, total, out,
			[]string{"-c:a", "aac", "-b:a", masterRate, "-movflags", "+faststart", "-f", "ipod"})
		if err != nil {
			return nil, err
		}
		return &Output{Path: out, Name: "recording.m4a", MimeType: "audio/mp4", Silent: silent}, nil
	}
	return c.composeVideo(ctx, dir, tracks, total, names)
}

// ErrNoEncoder is returned by Check for an ffmpeg that lacks an encoder the configured mode needs.
var ErrNoEncoder = errors.New("encoder missing")
