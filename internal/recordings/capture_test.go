package recordings

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

// opus20 is a 20 ms CELT packet (TOC config 31, one frame); opus10 a 10 ms one.
var (
	opus20 = []byte{0xf8, 0x01, 0x02}
	opus10 = []byte{0xf0, 0x01, 0x02}
)

// ntpOf is the NTP 32.32 time of a Unix ns time.
func ntpOf(ns int64) uint64 {
	sec := uint64(ns/1e9) + 2208988800
	frac := (uint64(ns%1e9) << 32) / 1e9
	return sec<<32 | frac
}

func newTestTrack(t *testing.T, start time.Time) (*audioTrack, string) {
	t.Helper()
	dir := t.TempDir()
	man, err := newManifest(filepath.Join(dir, "t001.jsonl"), Header{Kind: KindAudio, Codec: "opus", Clock: 48000, Participant: "u1"})
	if err != nil {
		t.Fatal(err)
	}
	return &audioTrack{dir: dir, prefix: "t001", man: man, clk: &clockMap{clock: 48000}, startNS: start.UnixNano(),
		countBytes: func(int64) {}}, dir
}

func readTrack(t *testing.T, dir string) *Track {
	t.Helper()
	tr, err := ReadManifest(filepath.Join(dir, "t001.jsonl"))
	if err != nil {
		t.Fatal(err)
	}
	return tr
}

func TestOpusSamples(t *testing.T) {
	t.Parallel()
	for _, tt := range []struct {
		b    []byte
		want int64
	}{
		{opus20, 960}, {opus10, 480}, {[]byte{0x08}, 960 /* SILK 20 ms */}, {[]byte{0x18}, 2880 /* SILK 60 ms */},
		{[]byte{0xf9}, 1920 /* two 20 ms frames */}, {[]byte{0xfb, 0x03}, 2880 /* code 3, three frames */}, {nil, 960},
	} {
		if got := opusSamples(tt.b); got != tt.want {
			t.Errorf("opusSamples(% x) = %d, want %d", tt.b, got, tt.want)
		}
	}
}

func TestExtendedTimestampsWrap(t *testing.T) {
	t.Parallel()
	var e extTS
	if a, b := e.next(0xffffff00), e.next(0x00000100); b-a != 0x200 {
		t.Fatalf("a wrap must be a step of 0x200, got %d", b-a)
	}
	if late := e.next(0xfffffff0); late >= 0x100000100 {
		t.Fatalf("a late packet before the wrap must stay before it: %d", late)
	}
}

// TestAudioRules drives the capture rules of design D4 through a sequence of packets and checks the files and
// positions the manifest ends with.
func TestAudioRules(t *testing.T) {
	t.Parallel()
	start := time.Unix(1_800_000_000, 0)
	a, dir := newTestTrack(t, start)
	// the sender's clock: RTP time 1000 is start+1 s (sender report on the same host)
	base := uint32(1000)
	at := func(ms int64) time.Time { return start.Add(time.Duration(ms) * time.Millisecond) }
	ts := func(ms int64) uint32 { return base + uint32(ms*48) }
	if err := a.senderReport(ntpOf(at(1000).UnixNano()), base, at(1000)); err != nil {
		t.Fatal(err)
	}
	push := func(ms int64, p []byte) {
		t.Helper()
		if err := a.push(ts(ms), p, at(1000+ms)); err != nil {
			t.Fatal(err)
		}
	}
	for ms := int64(0); ms < 1000; ms += 20 { // 1 s at 1 s
		push(ms, opus20)
	}
	push(980, opus20)                            // a duplicate: dropped
	for ms := int64(1500); ms < 2000; ms += 20 { // a 500 ms gap (DTX): filled with silence, same file
		push(ms, opus20)
	}
	for ms := int64(5000); ms < 5200; ms += 20 { // a 3 s gap: a new file at 6 s
		push(ms, opus20)
	}
	push(5205, opus20) // a jump of 5 ms: not whole frames, a new file
	for ms := int64(5225); ms < 5400; ms += 20 {
		push(ms, opus20)
	}
	a.unmuted = true // an unmute: a new file
	for ms := int64(6000); ms < 6200; ms += 20 {
		push(ms, opus20)
	}
	if err := a.close(); err != nil {
		t.Fatal(err)
	}
	tr := readTrack(t, dir)
	want := []struct{ pos, dur int64 }{{1000, 2000}, {6000, 200}, {6205, 200}, {7000, 200}}
	if len(tr.Segments) != len(want) {
		t.Fatalf("files %+v, want %d", tr.Segments, len(want))
	}
	for i, w := range want {
		s := tr.Segments[i]
		if s.PosMS != w.pos || s.DurMS != w.dur || s.Approx {
			t.Errorf("file %d: pos %d dur %d approx %v, want pos %d dur %d", i+1, s.PosMS, s.DurMS, s.Approx, w.pos, w.dur)
		}
		if st, err := os.Stat(filepath.Join(dir, s.File)); err != nil || st.Size() == 0 {
			t.Errorf("file %s: %v", s.File, err)
		}
	}
	if len(tr.Partial) != 0 {
		t.Errorf("partial %v", tr.Partial)
	}
}

// TestReanchor: when the sender-report clock and the file's own clock part by more than 40 ms, a new file starts.
func TestReanchor(t *testing.T) {
	t.Parallel()
	start := time.Unix(1_800_000_000, 0)
	a, dir := newTestTrack(t, start)
	if err := a.senderReport(ntpOf(start.UnixNano()), 0, start); err != nil {
		t.Fatal(err)
	}
	for i := range 50 { // 1 s
		if err := a.push(uint32(i*960), opus20, start.Add(time.Duration(i)*20*time.Millisecond)); err != nil {
			t.Fatal(err)
		}
	}
	// a new report: the same RTP time is now 100 ms later on the sender's clock (its clock was corrected)
	if err := a.senderReport(ntpOf(start.Add(1100*time.Millisecond).UnixNano()), 50*960, start.Add(1100*time.Millisecond)); err != nil {
		t.Fatal(err)
	}
	if err := a.push(50*960, opus20, start.Add(1100*time.Millisecond)); err != nil {
		t.Fatal(err)
	}
	if err := a.close(); err != nil {
		t.Fatal(err)
	}
	tr := readTrack(t, dir)
	if len(tr.Segments) != 2 || tr.Segments[1].PosMS < 1099 || tr.Segments[1].PosMS > 1100 { // NTP fractions round down
		t.Fatalf("want a second file at 1100 ms: %+v", tr.Segments)
	}
}

// TestClockOffset: LiveKit on another host with its clock 5 s ahead; the tracks land on the recorder's clock.
func TestClockOffset(t *testing.T) {
	t.Parallel()
	start := time.Unix(1_800_000_000, 0)
	a, dir := newTestTrack(t, start)
	ahead := 5 * time.Second
	delay := 3 * time.Millisecond // one-way, SFU to recorder
	// the report says RTP 0 is at start+2 s on the SFU's clock, i.e. start+2 s−5 s+… on ours: it arrives at
	// (SFU time − 5 s + delay)
	sfu := start.Add(2*time.Second + ahead)
	if err := a.senderReport(ntpOf(sfu.UnixNano()), 0, sfu.Add(-ahead+delay)); err != nil {
		t.Fatal(err)
	}
	if err := a.push(0, opus20, start.Add(2*time.Second+delay)); err != nil {
		t.Fatal(err)
	}
	if err := a.close(); err != nil {
		t.Fatal(err)
	}
	if got := readTrack(t, dir).Segments[0].PosMS; got != 2003 {
		t.Fatalf("position %d ms, want 2003 (the recorder's clock, plus the delay)", got)
	}
}

// TestNoSenderReport: packets placed by arrival, the recording marked partial; a report that comes later places
// the file by it.
func TestNoSenderReport(t *testing.T) {
	t.Parallel()
	start := time.Unix(1_800_000_000, 0)
	a, dir := newTestTrack(t, start)
	if err := a.push(0, opus20, start.Add(700*time.Millisecond)); err != nil {
		t.Fatal(err)
	}
	if err := a.close(); err != nil {
		t.Fatal(err)
	}
	tr := readTrack(t, dir)
	if !tr.Segments[0].Approx || tr.Segments[0].PosMS != 700 || len(tr.Partial) != 1 || tr.Partial[0] != PartialTimingApproximate {
		t.Fatalf("by arrival and partial: %+v", tr)
	}

	b, dir2 := newTestTrack(t, start)
	if err := b.push(0, opus20, start.Add(700*time.Millisecond)); err != nil {
		t.Fatal(err)
	}
	if err := b.senderReport(ntpOf(start.Add(650*time.Millisecond).UnixNano()), 0, start.Add(800*time.Millisecond)); err != nil {
		t.Fatal(err)
	}
	if err := b.close(); err != nil {
		t.Fatal(err)
	}
	tr = readTrack(t, dir2)
	if tr.Segments[0].Approx || len(tr.Partial) != 0 {
		t.Fatalf("placed by the report: %+v", tr)
	}
}

// TestTornManifest: a crash mid-line leaves a torn last line; everything before it is used, and a file without a
// durable length is left out.
func TestTornManifest(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	path := filepath.Join(dir, "t001.jsonl")
	body := `{"kind":"audio","codec":"opus","clock":48000,"participant":"u1"}
{"seg":1,"file":"t001-001.ogg","pos_ms":500}
{"seg":1,"dur":true,"dur_ms":4000}
{"seg":2,"file":"t001-002.ogg","pos_ms":9000}
{"seg":1,"dur":true,"dur_m`
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	tr, err := ReadManifest(path)
	if err != nil {
		t.Fatal(err)
	}
	if len(tr.Segments) != 1 || tr.Segments[0].DurMS != 4000 || tr.Segments[0].PosMS != 500 {
		t.Fatalf("segments %+v", tr.Segments)
	}
}

func TestLayOutTrimsOverlaps(t *testing.T) {
	t.Parallel()
	// in capture order: a, then b (re-anchored 100 ms back), then c; d starts before the recording
	got := layOut("/d", []Segment{{File: "d", PosMS: -500, DurMS: 600}, {File: "a", PosMS: 200, DurMS: 800},
		{File: "b", PosMS: 900, DurMS: 500}, {File: "c", PosMS: 1300, DurMS: 5000}}, 3000)
	want := []placed{{"/d/d", 0, 100, 500}, {"/d/a", 200, 900, 0}, {"/d/b", 900, 1300, 0}, {"/d/c", 1300, 3000, 0}}
	if len(got) != len(want) {
		t.Fatalf("%+v", got)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("%d: %+v, want %+v", i, got[i], want[i])
		}
	}
	// a later file that begins before an earlier one owns all of it (the earlier is gone)
	if got := layOut("/d", []Segment{{File: "old", PosMS: 1000, DurMS: 100}, {File: "new", PosMS: 900, DurMS: 300}}, 3000); len(got) != 1 || got[0].path != "/d/new" {
		t.Fatalf("the later file must own the overlap: %+v", got)
	}
	// a later file that does not overlap leaves the earlier one whole
	if got := layOut("/d", []Segment{{File: "a", PosMS: 1000, DurMS: 100}, {File: "b", PosMS: 900, DurMS: 50}}, 3000); len(got) != 2 || got[1].end != 1100 {
		t.Fatalf("no overlap, nothing cut: %+v", got)
	}
}

func TestKeyframe(t *testing.T) {
	t.Parallel()
	for _, tt := range []struct {
		b    []byte
		vp9  bool
		want bool
	}{
		{[]byte{0x10, 0x02}, false, true}, {[]byte{0x11}, false, false},
		{[]byte{0x82, 0x49}, true, true},  // frame marker 2, profile 0, show_existing 0, frame_type 0
		{[]byte{0x86, 0x49}, true, false}, // frame_type 1
		{[]byte{0x88}, true, false},       // show_existing_frame
	} {
		if got := keyframe(tt.b, tt.vp9); got != tt.want {
			t.Errorf("keyframe(% x, vp9=%v) = %v", tt.b, tt.vp9, got)
		}
	}
}
