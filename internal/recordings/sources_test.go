package recordings

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/jniltinho/meethub/internal/chat"
	"github.com/jniltinho/meethub/internal/database"
	"github.com/jniltinho/meethub/internal/database/dbtest"
	"github.com/jniltinho/meethub/internal/org"
)

func TestCutPieces(t *testing.T) {
	t.Parallel()
	const min5 = 300_000
	tests := []struct {
		name     string
		silences [][2]int64
		total    int64
		want     [][2]int64
	}{
		{name: "silent", silences: [][2]int64{{0, 60_000}}, total: 60_000},
		{name: "one sentence", silences: [][2]int64{{0, 1000}, {3000, 60_000}}, total: 60_000, want: [][2]int64{{850, 3150}}},
		{name: "sentences joined while they fit", silences: [][2]int64{{0, 1000}, {3000, 4000}, {6000, 60_000}}, total: 60_000,
			want: [][2]int64{{850, 6150}}},
		{name: "a long silence is not sent", silences: [][2]int64{{2000, 400_000}}, total: 402_000,
			want: [][2]int64{{0, 2150}, {399_850, 402_000}}},
		// twelve minutes with ordinary pauses every 50 s: every cut inside a pause
		{name: "cut in pauses", total: 720_000, silences: func() [][2]int64 {
			var out [][2]int64
			for at := int64(50_000); at < 720_000; at += 50_000 {
				out = append(out, [2]int64{at, at + 600})
			}
			return out
		}(), want: [][2]int64{{0, 250_150}, {250_450, 550_150}, {550_450, 720_000}}},
		// five minutes without a pause: cut mid-speech at the limit, the pieces contiguous
		{name: "no pause", total: 420_000, want: [][2]int64{{0, min5}, {min5, 420_000}}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got := cutPieces(tt.silences, tt.total, min5)
			if fmt.Sprint(got) != fmt.Sprint(tt.want) {
				t.Fatalf("pieces %v, want %v", got, tt.want)
			}
			for i, p := range got {
				if p[1]-p[0] > min5 || p[0] >= p[1] || (i > 0 && p[0] < got[i-1][1]) {
					t.Fatalf("piece %d %v: too long, empty or overlapping", i, p)
				}
			}
		})
	}
	// with the pauses of "cut in pauses", every cut lands inside one
	for _, p := range cutPieces(tests[4].silences, 720_000, min5)[1:] {
		in := false
		for _, s := range tests[4].silences {
			in = in || (p[0] >= s[0] && p[0] <= s[1])
		}
		if !in {
			t.Fatalf("a cut at %d ms is not in a pause", p[0])
		}
	}
}

// TestMakeSources cuts a real track: speech-like tone bursts with pauses, and loud continuous noise.
func TestMakeSources(t *testing.T) {
	ff := testFFmpeg(t)
	dbtest.Each(t, func(t *testing.T, db *database.DB) {
		w := newWorld(t, db)
		s := w.service(t, w.dir, defaultConfig(), nil)
		s.PieceBytes = 200_000 // pieces of at most 33 s
		rec := w.recordingRow(t, s, StatusProcessing, true)
		dir := t.TempDir()
		// 70 s: tone bursts of 4 s every 5 s (1 s pauses), as a participant's flattened track
		track := filepath.Join(dir, "p00.ogg")
		if err := ff.Run(context.Background(), dir, "gen", time.Minute, "-f", "lavfi", "-i",
			"sine=f=300:sample_rate=48000:d=70", "-af", "volume='if(lt(mod(t,5),4),1,0)':eval=frame", "-ac", "1", "-c:a", "libopus",
			"-b:a", "64k", track); err != nil {
			t.Fatal(logOf(err))
		}
		if err := s.makeSources(context.Background(), &rec, w.ana.UserID, track, 70_000); err != nil {
			t.Fatal(logOf(err))
		}
		var objs []Object
		db.Gorm.Where("recording_id = ?", rec.ID).Order("offset_ms").Find(&objs)
		if len(objs) < 3 {
			t.Fatalf("pieces: %+v", objs)
		}
		for i, o := range objs {
			o.Trim()
			if o.State != ObjectStored || o.Kind != ObjectSource || o.Speaker != w.ana.UserID || o.DurationMS > s.pieceLimit() {
				t.Fatalf("piece %d: %+v", i, o)
			}
			// a cut lands in a pause: 4..5 s of every 5 s
			if i > 0 && o.OffsetMS%5000 < 3900 {
				t.Fatalf("piece %d starts at %d ms, inside speech", i, o.OffsetMS)
			}
			// each piece decodes alone, as mono 16 kHz
			f, err := w.store.Open(bg, o.ObjectKey)
			if err != nil {
				t.Fatal(err)
			}
			data, _ := io.ReadAll(f)
			_ = f.Close()
			if int64(len(data)) > s.PieceBytes {
				t.Fatalf("piece %d: %d bytes", i, len(data))
			}
			p := filepath.Join(dir, fmt.Sprintf("piece%d.ogg", i))
			_ = os.WriteFile(p, data, 0o600)
			x := pcm(t, ff, dir, p)
			if got := int64(len(x)) * 1000 / 48000; got < o.DurationMS-100 || got > o.DurationMS+100 {
				t.Fatalf("piece %d decodes to %d ms, want %d", i, got, o.DurationMS)
			}
		}

		// loud continuous noise for 100 s: no pause, so cut at the limit, and every piece under the byte limit
		noise := filepath.Join(dir, "p01.ogg")
		if err := ff.Run(context.Background(), dir, "gen2", time.Minute, "-f", "lavfi", "-i",
			"aevalsrc=0.9*(random(0)*2-1):s=48000:d=100", "-ac", "1", "-c:a", "libopus", "-b:a", "64k", noise); err != nil {
			t.Fatal(logOf(err))
		}
		if err := s.makeSources(context.Background(), &rec, w.bob.UserID, noise, 100_000); err != nil {
			t.Fatal(logOf(err))
		}
		var loud []Object
		db.Gorm.Where("recording_id = ? AND speaker = ?", rec.ID, w.bob.UserID).Order("offset_ms").Find(&loud)
		if len(loud) < 3 {
			t.Fatalf("loud pieces: %+v", loud)
		}
		for i, o := range loud {
			if i > 0 && o.OffsetMS != loud[i-1].OffsetMS+loud[i-1].DurationMS {
				t.Fatalf("mid-speech pieces must be contiguous: %+v", loud)
			}
		}
	})
}

// TestSourcesGone: an object registered or written for a recording that went away is not kept.
func TestSourcesGone(t *testing.T) {
	dbtest.Each(t, func(t *testing.T, db *database.DB) {
		w := newWorld(t, db)
		s := w.service(t, w.dir, defaultConfig(), nil)
		rec := w.recordingRow(t, s, StatusProcessing, true)
		o := &Object{RecordingID: rec.ID, Generation: 1, Kind: ObjectSource, ObjectKey: "summaries/" + rec.ID + "/g1/sources/a.ogg"}
		if err := s.Register(bg, o); err != nil {
			t.Fatal(err)
		}
		db.Gorm.Model(&Recording{}).Where("id = ?", rec.ID).Update("expired_at", 1)
		if err := s.PutObject(bg, o, []byte("x")); !errors.Is(err, ErrGone) {
			t.Fatalf("put after expiry: %v", err)
		}
		var n int64
		db.Gorm.Model(&Object{}).Where("recording_id = ?", rec.ID).Count(&n)
		if _, err := w.store.Open(bg, o.ObjectKey); err == nil || n != 0 {
			t.Fatalf("left: %d rows, object %v", n, err)
		}
		if err := s.Register(bg, &Object{RecordingID: rec.ID, ObjectKey: "summaries/x"}); !errors.Is(err, ErrGone) {
			t.Fatalf("register after expiry: %v", err)
		}
	})
}

// TestSummaryPolicy: a recording keeps the setting in force at its start; one made under "automatic" is queued in
// the transaction that makes it ready, and only with sources.
func TestSummaryPolicy(t *testing.T) {
	dbtest.Each(t, func(t *testing.T, db *database.DB) {
		w := newWorld(t, db)
		s := w.service(t, w.dir, defaultConfig(), nil)
		policy := org.SummariesAutomatic
		s.Policy = func(context.Context) string { return policy }
		var queued []string
		s.OnQueued = func(_ context.Context, id string) { queued = append(queued, id) }
		w.join(t, w.public.ID, w.ana) // no Run: the stopped capture is not composed here
		v, err := s.Start(bg, w.ana, w.public.ID)
		if err != nil {
			t.Fatal(err)
		}
		w.started(t)
		if r := w.row(t, v.ID); r.SummaryPolicy != org.SummariesAutomatic {
			t.Fatalf("policy: %+v", r)
		}
		room, err := w.meet.Room(bg, w.ana, w.public.ID)
		if err != nil || room.Recording == nil || room.Recording.SummaryPolicy != org.SummariesAutomatic {
			t.Fatalf("the room does not say it: %+v %v", room, err)
		}
		if err := s.Stop(bg, w.ana, w.public.ID); err != nil {
			t.Fatal(err)
		}
		<-w.captured

		out := filepath.Join(t.TempDir(), "recording.m4a")
		_ = os.WriteFile(out, []byte("x"), 0o600)
		publish := func(policyAtStart string, sources bool) Recording {
			r := w.recordingRow(t, s, StatusProcessing, true)
			r.SummaryPolicy = policyAtStart
			if err := s.publish(bg, &r, &Output{Path: out, Name: "recording.m4a", MimeType: "audio/mp4"}, sources); err != nil {
				t.Fatal(err)
			}
			return w.row(t, r.ID)
		}
		if r := publish(org.SummariesAutomatic, true); r.SummaryStatus != SummaryQueued || r.SummarySource != SourceAvailable ||
			r.SummarySourceExpiresAt != r.EndedAt+SourceKeep.Milliseconds() || len(queued) != 1 || queued[0] != r.ID {
			t.Fatalf("automatic: %+v %v", r, queued)
		}
		if r := publish(org.SummariesManual, true); r.SummaryStatus == SummaryQueued || r.SummarySource != SourceAvailable {
			t.Fatalf("manual: %+v", r)
		}
		policy = org.SummariesOff // off prevails over the policy at the start
		if r := publish(org.SummariesAutomatic, true); r.SummaryStatus == SummaryQueued || len(queued) != 1 {
			t.Fatalf("off now: %+v", r)
		}
		if r := publish(org.SummariesOff, false); r.SummarySource == SourceAvailable {
			t.Fatalf("made under off: %+v", r)
		}
		var p chat.Post
		db.Gorm.Where("type = ?", chat.PostTypeRecording).Order("create_at DESC").First(&p)
		if p.Props == nil {
			t.Fatal("no props")
		}
	})
}
