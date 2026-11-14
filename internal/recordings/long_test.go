package recordings

import (
	"context"
	"fmt"
	"os"
	"testing"
	"time"
)

// TestComposeFourHours (task 3.1): 25 participants who mute every 30 seconds for four hours — 6 000 files — compose
// into one master of the right length with each voice in place. It takes minutes: MEETHUB_TEST_LONG=1 runs it.
func TestComposeFourHours(t *testing.T) {
	if os.Getenv("MEETHUB_TEST_LONG") == "" {
		t.Skip("MEETHUB_TEST_LONG not set: a four-hour composition")
	}
	ff := testFFmpeg(t)
	dir := t.TempDir()
	const people, total = 25, int64(4 * 3600 * 1000)
	for p := range people {
		name := fmt.Sprintf("voice%02d.ogg", p)
		tone(t, ff, dir, name, 300+p*100, 30_000) // a distinct tone per person, 30 s with the microphone on
		var segs []Segment
		for at := int64(p) * 1000; at+30_000 <= total; at += 60_000 { // on 30 s, muted 30 s
			segs = append(segs, Segment{File: name, PosMS: at, DurMS: 30_000})
		}
		writeTrack(t, dir, fmt.Sprintf("t%03d.jsonl", p+1), Header{Kind: KindAudio, Participant: fmt.Sprintf("p%02d", p)}, segs...)
	}
	start := time.Now()
	out, err := (&Composer{FFmpeg: ff, Mode: "audio"}).Compose(context.Background(), dir, total, nil)
	if err != nil {
		t.Fatal(logOf(err))
	}
	took := time.Since(start)
	st, _ := os.Stat(out.Path)
	t.Logf("composed 25 people × 240 files, 4 h, in %s: %d bytes", took, st.Size())
	// the end of the recording: decode its last two minutes and find person 0's and person 24's tones in place
	tail := dir + "/tail.m4a"
	if err := ff.Run(context.Background(), dir, "tail", time.Minute, "-ss", sec(total-120_000), "-i", out.Path, "-c", "copy", tail); err != nil {
		t.Fatal(logOf(err))
	}
	x := pcm(t, ff, dir, tail)
	near(t, "the last two minutes", int64(len(x))/48, 120_000, 100)
	for _, p := range []int{0, 24} {
		got := toneSpans(x, float64(300+p*100))
		// in the last two minutes person p speaks from (total−120 s) at 60 s steps, offset p s
		if len(got) == 0 {
			t.Errorf("person %d is not heard at the end", p)
			continue
		}
		first := (int64(p)*1000 + ((total-120_000-int64(p)*1000+59_999)/60_000)*60_000) - (total - 120_000)
		near(t, fmt.Sprintf("person %d at the end", p), got[0][0], first, 100)
	}
}
