package recordings

import (
	"bytes"
	"context"
	"encoding/binary"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
	"time"
)

// These tests run the real ffmpeg (make test mounts the image's own build); without one they are skipped.

func testFFmpeg(t *testing.T) *FFmpeg {
	t.Helper()
	p, err := exec.LookPath("ffmpeg")
	if err != nil {
		t.Skip("no ffmpeg in PATH")
	}
	return &FFmpeg{Path: p}
}

// tone writes an Ogg Opus file of a sine at freq, ms long.
func tone(t *testing.T, ff *FFmpeg, dir, name string, freq int, ms int64) {
	t.Helper()
	if err := ff.Run(context.Background(), dir, "gen", time.Minute, "-filter_complex",
		fmt.Sprintf("sine=f=%d:sample_rate=48000:d=%s[o]", freq, sec(ms)), "-map", "[o]", "-c:a", "libopus", "-b:a", "64k", name); err != nil {
		t.Fatal(err)
	}
}

// writeTrack writes a manifest placing files at the given positions.
func writeTrack(t *testing.T, dir, name string, h Header, segs ...Segment) {
	t.Helper()
	var b bytes.Buffer
	enc := json.NewEncoder(&b)
	_ = enc.Encode(h)
	for i, s := range segs {
		_ = enc.Encode(Line{Seg: i + 1, File: s.File, PosMS: s.PosMS})
		_ = enc.Encode(Line{Seg: i + 1, Dur: true, DurMS: s.DurMS})
	}
	if err := os.WriteFile(filepath.Join(dir, name), b.Bytes(), 0o600); err != nil {
		t.Fatal(err)
	}
}

// pcm decodes a file's audio to mono 48 kHz samples.
func pcm(t *testing.T, ff *FFmpeg, dir, path string) []float64 {
	t.Helper()
	out := filepath.Join(dir, "decoded.wav")
	if err := ff.Run(context.Background(), dir, "decode", time.Minute, "-i", path, "-map", "0:a", "-ac", "1", "-ar", "48000",
		"-c:a", "pcm_s16le", "-f", "wav", out); err != nil {
		t.Fatal(err)
	}
	b, err := os.ReadFile(out)
	if err != nil {
		t.Fatal(err)
	}
	if i := bytes.Index(b, []byte("data")); i >= 0 {
		b = b[i+8:]
	}
	x := make([]float64, len(b)/2)
	for i := range x {
		x[i] = float64(int16(binary.LittleEndian.Uint16(b[2*i:]))) / 32768 //nolint:gosec // PCM
	}
	return x
}

// toneSpans returns where freq sounds in x, as [start, end) ms (10 ms windows, 2.5 ms steps).
func toneSpans(x []float64, freq float64) [][2]int64 {
	const win, hop = 480, 120
	k := 2 * math.Cos(2*math.Pi*freq/48000)
	var pw []float64
	peak := 0.0
	for s := 0; s+win <= len(x); s += hop {
		var s1, s2 float64
		for i := s; i < s+win; i++ {
			s0 := x[i] + k*s1 - s2
			s2, s1 = s1, s0
		}
		p := math.Sqrt(math.Max(s1*s1+s2*s2-k*s1*s2, 0)) / win
		pw = append(pw, p)
		peak = math.Max(peak, p)
	}
	var out [][2]int64
	on := -1
	for i, p := range append(pw, 0) {
		ms := int64((i*hop + win/2) / 48)
		switch {
		case p >= peak*0.3 && on < 0:
			on = i
		case p < peak*0.3 && on >= 0:
			out = append(out, [2]int64{int64((on*hop + win/2) / 48), ms})
			on = -1
		}
	}
	return out
}

func near(t *testing.T, what string, got, want, tol int64) {
	t.Helper()
	if got < want-tol || got > want+tol {
		t.Errorf("%s = %d ms, want %d ± %d", what, got, want, tol)
	}
}

func TestComposeAudioPositions(t *testing.T) {
	t.Parallel()
	ff := testFFmpeg(t)
	dir := t.TempDir()
	tone(t, ff, dir, "t001-001.ogg", 440, 1000)
	tone(t, ff, dir, "t002-001.ogg", 880, 500)
	tone(t, ff, dir, "t002-002.ogg", 880, 500)
	writeTrack(t, dir, "t001.jsonl", Header{Kind: KindAudio, Participant: "ana"}, Segment{File: "t001-001.ogg", PosMS: 1000, DurMS: 1000})
	writeTrack(t, dir, "t002.jsonl", Header{Kind: KindAudio, Participant: "bruno"},
		Segment{File: "t002-001.ogg", PosMS: 3000, DurMS: 500}, Segment{File: "t002-002.ogg", PosMS: 5000, DurMS: 500})
	c := &Composer{FFmpeg: ff, Mode: "audio"}
	out, err := c.Compose(context.Background(), dir, 7000, nil)
	if err != nil {
		t.Fatal(err)
	}
	if out.Silent || out.MimeType != "audio/mp4" || filepath.Ext(out.Path) != ".m4a" {
		t.Fatalf("output %+v", out)
	}
	x := pcm(t, ff, dir, out.Path)
	near(t, "duration", int64(len(x))/48, 7000, 25)
	a := toneSpans(x, 440)
	b := toneSpans(x, 880)
	if len(a) != 1 || len(b) != 2 {
		t.Fatalf("tones: 440 Hz %v, 880 Hz %v", a, b)
	}
	near(t, "Ana's start", a[0][0], 1000, 20)
	near(t, "Ana's end", a[0][1], 2000, 20)
	near(t, "Bruno's first start", b[0][0], 3000, 20)
	near(t, "Bruno's second start", b[1][0], 5000, 20)
	for _, f := range []string{"p00.ogg", "p01.ogg"} {
		if _, err := os.Stat(filepath.Join(dir, f)); err == nil {
			t.Errorf("intermediate %s left behind", f)
		}
	}
}

func TestComposeNobodySpoke(t *testing.T) {
	t.Parallel()
	ff := testFFmpeg(t)
	dir := t.TempDir()
	out, err := (&Composer{FFmpeg: ff, Mode: "audio"}).Compose(context.Background(), dir, 3000, nil)
	if err != nil {
		t.Fatal(err)
	}
	if !out.Silent {
		t.Fatal("a recording where nobody spoke must say so")
	}
	near(t, "duration", int64(len(pcm(t, ff, dir, out.Path)))/48, 3000, 25)
}

// TestComposeBatches: more files than one ffmpeg run takes (a participant who muted often) still land in place.
func TestComposeBatches(t *testing.T) {
	t.Parallel()
	ff := testFFmpeg(t)
	dir := t.TempDir()
	tone(t, ff, dir, "beep.ogg", 1000, 100)
	var segs []Segment
	for i := range 120 { // 120 files of 100 ms, one every 250 ms: three batches
		segs = append(segs, Segment{File: "beep.ogg", PosMS: int64(i) * 250, DurMS: 100})
	}
	writeTrack(t, dir, "t001.jsonl", Header{Kind: KindAudio, Participant: "ana"}, segs...)
	out, err := (&Composer{FFmpeg: ff, Mode: "audio"}).Compose(context.Background(), dir, 30_500, nil)
	if err != nil {
		t.Fatal(err)
	}
	got := toneSpans(pcm(t, ff, dir, out.Path), 1000)
	if len(got) != 120 {
		t.Fatalf("%d beeps, want 120", len(got))
	}
	for _, i := range []int{0, 49, 50, 99, 100, 119} { // at the batch edges
		near(t, fmt.Sprintf("beep %d", i), got[i][0], int64(i)*250, 20)
	}
}

// TestComposeVideo: a camera and a screen share; the layout changes when the screen starts, a camera ends early,
// and a flash in the camera stays in sync with a beep of the audio at the last stitch.
func TestComposeVideo(t *testing.T) {
	t.Parallel()
	ff := testFFmpeg(t)
	dir := t.TempDir()
	ctx := context.Background()
	gen := func(name, graph string) {
		t.Helper()
		if err := ff.Run(ctx, dir, "gen", time.Minute, "-filter_complex", graph+"[o]", "-map", "[o]", "-c:v", "libvpx",
			"-b:v", "800k", "-deadline", "realtime", "-g", "25", "-f", "ivf", name); err != nil {
			t.Fatal(err)
		}
	}
	// Ana's camera for 20 s, dark with a white flash at 17 s; Bruno's camera for 6 s; Bruno's screen from 10 s
	gen("t002-001.ivf", "color=c=0x202020:s=640x360:r=25:d=17,format=yuv420p[a];color=c=white:s=640x360:r=25:d=0.4,format=yuv420p[b];color=c=0x202020:s=640x360:r=25:d=2.6,format=yuv420p[c];[a][b][c]concat=n=3:v=1")
	gen("t003-001.ivf", "color=c=0x406080:s=640x360:r=25:d=6")
	gen("t004-001.ivf", "color=c=0x808040:s=960x540:r=25:d=10")
	tone(t, ff, dir, "t001-001.ogg", 1000, 400)
	writeTrack(t, dir, "t001.jsonl", Header{Kind: KindAudio, Participant: "ana", Name: "Ana Silva"},
		Segment{File: "t001-001.ogg", PosMS: 17000, DurMS: 400})
	writeTrack(t, dir, "t002.jsonl", Header{Kind: KindVideo, Participant: "ana", Name: "Ana Silva", Source: SourceCamera},
		Segment{File: "t002-001.ivf", PosMS: 0, DurMS: 20000})
	writeTrack(t, dir, "t003.jsonl", Header{Kind: KindVideo, Participant: "bruno", Name: "Bruno", Source: SourceCamera},
		Segment{File: "t003-001.ivf", PosMS: 0, DurMS: 6000})
	writeTrack(t, dir, "t004.jsonl", Header{Kind: KindVideo, Participant: "bruno", Name: "Bruno", Source: SourceScreen},
		Segment{File: "t004-001.ivf", PosMS: 10000, DurMS: 10000})
	out, err := (&Composer{FFmpeg: ff, Mode: "video", Format: "webm"}).Compose(ctx, dir, 20000, nil)
	if err != nil {
		t.Fatal(logOf(err))
	}
	if out.MimeType != "video/webm" {
		t.Fatalf("output %+v", out)
	}
	// the streams: VP8 1280×720 yuv420p and Opus
	info := exec.Command(ff.Path, "-hide_banner", "-i", out.Path) //nolint:gosec // test
	b, _ := info.CombinedOutput()
	for _, want := range []string{`Video: vp8.*yuv420p.*1280x720`, `Audio: opus`} {
		if !regexp.MustCompile(want).Match(b) {
			t.Errorf("no %q in\n%s", want, b)
		}
	}
	// frames: grey levels of a 16×9 thumbnail of each; the flash is white
	raw := filepath.Join(dir, "frames.raw")
	if err := ff.Run(ctx, dir, "frames", time.Minute, "-i", out.Path, "-vf", "scale=16:9,format=gray", "-f", "rawvideo", raw); err != nil {
		t.Fatal(err)
	}
	fr, err := os.ReadFile(raw)
	if err != nil {
		t.Fatal(err)
	}
	frames := len(fr) / (16 * 9)
	near(t, "video duration", int64(frames)*40, 20000, 80)
	flash := -1
	for i := range frames {
		sum := 0
		// Ana's slot: the strip on the right, first tile (x 12–15, y 0–1 of 16×9)
		for y := range 2 {
			for x := 12; x < 16; x++ {
				sum += int(fr[i*144+y*16+x])
			}
		}
		if sum/8 > 200 && flash < 0 {
			flash = i
		}
	}
	if flash < 0 {
		t.Fatal("the flash is not in the video")
	}
	beeps := toneSpans(pcm(t, ff, dir, out.Path), 1000)
	if len(beeps) != 1 {
		t.Fatalf("beeps %v", beeps)
	}
	near(t, "flash", int64(flash)*40, 17000, 40)
	near(t, "beep", beeps[0][0], 17000, 20)
	if d := int64(flash)*40 - beeps[0][0]; d < -60 || d > 60 {
		t.Errorf("audio and video apart by %d ms at the last stitch", d)
	}
	if left, _ := filepath.Glob(filepath.Join(dir, "seg*")); len(left) > 0 {
		t.Errorf("segments left behind: %v", left)
	}
}

// logOf is the error output of a failed ffmpeg run, for the test's own report.
func logOf(err error) string {
	var fe *FFmpegError
	if errors.As(err, &fe) && fe.Log != "" {
		b, _ := os.ReadFile(fe.Log)
		return fmt.Sprintf("%v\n%s", err, b)
	}
	return fmt.Sprint(err)
}

func TestSpans(t *testing.T) {
	t.Parallel()
	cam := func(from, to int64) []placed { return []placed{{path: "c", start: from, end: to}} }
	ana := &person{id: "ana", name: "Ana", camera: cam(0, 60_000), audio: cam(0, 60_000)}
	bruno := &person{id: "bruno", name: "Bruno", screen: []placed{{path: "s", start: 5_000, end: 8_000}, {path: "s2", start: 30_000, end: 40_000}}}
	got := spans([]*person{ana, bruno}, 60_000)
	// the screen's start and stop at 5 and 8 s wait for the 10 s dwell; then 30 s, 40 s and the end
	want := [][2]int64{{0, 10_000}, {10_000, 30_000}, {30_000, 40_000}, {40_000, 60_000}}
	if len(got) != len(want) {
		t.Fatalf("spans %+v", got)
	}
	for i, w := range want {
		if got[i].start != w[0] || got[i].end != w[1] {
			t.Errorf("span %d: %d–%d, want %v", i, got[i].start, got[i].end, w)
		}
	}
	if got[2].screen != bruno || got[0].screen != nil || got[1].screen != nil {
		t.Errorf("the screen fills the frame only while shared")
	}
	// 201 layout changes: at most maxSegments spans, the rest one fixed layout
	var many []placed
	for i := range 402 {
		many = append(many, placed{path: "s", start: int64(i) * 20_000, end: int64(i)*20_000 + 10_000})
	}
	carla := &person{id: "carla", screen: many}
	total := int64(402 * 20_000)
	got = spans([]*person{ana, carla}, total)
	if len(got) != maxSegments || got[len(got)-1].end != total {
		t.Fatalf("%d spans, last ends at %d", len(got), got[len(got)-1].end)
	}
}

func TestSlotsGridAndStrip(t *testing.T) {
	t.Parallel()
	var people []*person
	for i := range 11 {
		people = append(people, &person{id: fmt.Sprint(i), name: strings.Repeat("x", i+1)})
	}
	s := span{people: people}
	_, tiles, shown := s.slots()
	if len(tiles) != 9 || len(shown) != 9 || tiles[8].X != 2*(videoW/3) || tiles[8].Y != 2*videoH/3 {
		t.Fatalf("a 3×3 grid of the first nine: %+v", tiles)
	}
	s.screen = people[0]
	screen, tiles, _ := s.slots()
	if len(tiles) != 4 || screen.W != videoW*3/4 || tiles[3].Y != 3*videoH/4 {
		t.Fatalf("the screen with four cameras in a strip: %+v %+v", screen, tiles)
	}
}

// TestProbeRelativeDirectory: the default recordings.dir is relative (data/recordings) and ffmpeg runs inside it.
func TestProbeRelativeDirectory(t *testing.T) {
	ff := testFFmpeg(t)
	t.Chdir(t.TempDir())
	if err := Probe(context.Background(), ff, "data/recordings", "audio", "webm"); err != nil {
		t.Fatal(logOf(err))
	}
}

func TestProbe(t *testing.T) {
	t.Parallel()
	ff := testFFmpeg(t)
	for _, mode := range []string{"audio", "video"} {
		if err := Probe(context.Background(), ff, t.TempDir(), mode, "webm"); err != nil {
			t.Errorf("%s: %s", mode, logOf(err))
		}
	}
	err := Probe(context.Background(), ff, t.TempDir(), "video", "mp4")
	if err == nil || !strings.Contains(err.Error(), "recordings.video_format") || !strings.Contains(err.Error(), "libx264") {
		t.Errorf("mp4 without libx264: %v", err)
	}
	err = Probe(context.Background(), &FFmpeg{Path: "/nonexistent/ffmpeg"}, t.TempDir(), "audio", "webm")
	if err == nil || !strings.Contains(err.Error(), "recordings.ffmpeg_path") {
		t.Errorf("no ffmpeg: %v", err)
	}
}
