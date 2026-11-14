package recordings

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/livekit/protocol/livekit"
	lksdk "github.com/livekit/server-sdk-go/v2"
	"github.com/pion/rtp"
	"github.com/pion/webrtc/v4"
	"github.com/pion/webrtc/v4/pkg/media"
	"github.com/pion/webrtc/v4/pkg/media/ivfreader"
	"github.com/pion/webrtc/v4/pkg/media/oggreader"
)

// The recorder against a real LiveKit (the test stack's): Go publishers send tone markers at known times through
// DTX gaps, a mute, a reconnect and 5 % packet loss, and a VP8 camera at a variable frame rate with a flash; the recorder captures them, the composition
// places every marker within 100 ms of when it was sent (design D4, spec "What is recorded").

// liveKit is the test stack's LiveKit and its key (compose.test.yml); MEETHUB_TEST_LIVEKIT_KEY and _SECRET point the
// test at another one (the all-in-one image's, for the network path of design D11).
func liveKit(t *testing.T) (url, key, secret string) {
	t.Helper()
	url = os.Getenv("MEETHUB_TEST_LIVEKIT_URL")
	if url == "" {
		t.Skip("MEETHUB_TEST_LIVEKIT_URL not set (run make test)")
	}
	key, secret = "meethub-test", "meethub-test-livekit-secret-0123456789"
	if k := os.Getenv("MEETHUB_TEST_LIVEKIT_KEY"); k != "" {
		key, secret = k, os.Getenv("MEETHUB_TEST_LIVEKIT_SECRET")
	}
	return url, key, secret
}

// opusPackets reads the packets of an Ogg Opus file written by ffmpeg with one packet per page.
func opusPackets(t *testing.T, path string) [][]byte {
	t.Helper()
	f, err := os.Open(path) //nolint:gosec // test
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = f.Close() }()
	r, _, err := oggreader.NewWith(f)
	if err != nil {
		t.Fatal(err)
	}
	var out [][]byte
	for {
		p, _, err := r.ParseNextPage()
		if err != nil {
			break
		}
		if len(p) >= 8 && (string(p[:8]) == "OpusHead" || string(p[:8]) == "OpusTags") {
			continue
		}
		out = append(out, p)
	}
	return out
}

type publisher struct {
	t       *testing.T
	key     string
	secret  string
	url     string
	room    string
	id      string
	r       *lksdk.Room
	mic     *lksdk.LocalTrack
	micPub  *lksdk.LocalTrackPublication
	markers []time.Time
	// lossy sends RTP itself and loses every 20th packet (5 %) the way a network does: a gap in the sequence
	lossy bool
	seq   uint16
	ts    uint32
	sent  int
}

func (p *publisher) connect() {
	p.t.Helper()
	var err error
	for range 20 { // LiveKit may still be starting
		p.r, err = lksdk.ConnectToRoom(p.url, lksdk.ConnectInfo{APIKey: p.key, APISecret: p.secret, RoomName: p.room,
			ParticipantIdentity: p.id, ParticipantName: strings.ToUpper(p.id[:1]) + p.id[1:]}, lksdk.NewRoomCallback())
		if err == nil {
			break
		}
		time.Sleep(500 * time.Millisecond)
	}
	if err != nil {
		p.t.Fatalf("publisher %s: %v", p.id, err)
	}
	newTrack := lksdk.NewLocalSampleTrack
	if p.lossy {
		newTrack = lksdk.NewLocalTrack
	}
	if p.mic, err = newTrack(webrtc.RTPCodecCapability{MimeType: webrtc.MimeTypeOpus, ClockRate: 48000, Channels: 2}); err != nil {
		p.t.Fatal(err)
	}
	if p.micPub, err = p.r.LocalParticipant.PublishTrack(p.mic, &lksdk.TrackPublicationOptions{Name: "mic", Source: livekit.TrackSource_MICROPHONE}); err != nil {
		p.t.Fatal(err)
	}
}

// speak sends 20 ms frames for ms: silence, with a beep (10 frames) starting at each mark (ms from now). gapAfter
// stops sending (DTX) between two times.
func (p *publisher) speak(beep, silence [][]byte, ms int, marks []int, quiet [2]int) {
	start := time.Now()
	isMark := map[int]bool{}
	for _, m := range marks {
		isMark[m/20] = true
	}
	beepAt, dropped := -1, 0
	for i := range ms / 20 {
		due := start.Add(time.Duration(i) * 20 * time.Millisecond)
		time.Sleep(time.Until(due))
		if i*20 >= quiet[0] && i*20 < quiet[1] {
			dropped++
			continue
		}
		if isMark[i] {
			beepAt = i
			p.markers = append(p.markers, due)
		}
		data := silence[i%len(silence)]
		if beepAt >= 0 && i-beepAt < 10 && i-beepAt < len(beep) {
			data = beep[i-beepAt]
		}
		if p.lossy {
			p.ts += uint32(960 * (dropped + 1)) //nolint:gosec // small
			p.seq++
			if p.sent++; p.sent%20 != 7 { // the 7th of every 20 is lost
				_ = p.mic.WriteRTP(&rtp.Packet{Header: rtp.Header{Version: 2, SequenceNumber: p.seq, Timestamp: p.ts}, Payload: data}, nil)
			}
		} else {
			_ = p.mic.WriteSample(media.Sample{Data: data, Duration: 20 * time.Millisecond, PrevDroppedPackets: uint16(dropped)}, nil) //nolint:gosec // small
		}
		dropped = 0
	}
}

func TestRecorderLive(t *testing.T) {
	url, key, secret := liveKit(t)
	ff := testFFmpeg(t)
	dir := t.TempDir()
	tone(t, ff, dir, "beep700.ogg", 700, 200)
	tone(t, ff, dir, "beep1100.ogg", 1100, 200)
	if err := ff.Run(context.Background(), dir, "gen", time.Minute, "-filter_complex", "sine=f=100:sample_rate=48000:d=1,volume=0[o]",
		"-map", "[o]", "-c:a", "libopus", "-b:a", "32k", "-page_duration", "20000", "silence.ogg"); err != nil {
		t.Fatal(logOf(err))
	}
	for _, f := range []string{"beep700", "beep1100"} { // one packet per page
		if err := ff.Run(context.Background(), dir, "gen", time.Minute, "-i", f+".ogg", "-c", "copy", "-page_duration", "20000", f+"p.ogg"); err != nil {
			t.Fatal(logOf(err))
		}
	}
	// a camera: dark, white flash at 11 s (after the first layout's 10 s), for 13 s
	if err := ff.Run(context.Background(), dir, "gen", time.Minute, "-filter_complex",
		"color=c=0x202020:s=640x360:r=25:d=11,format=yuv420p[a];color=c=white:s=640x360:r=25:d=0.4,format=yuv420p[b];color=c=0x202020:s=640x360:r=25:d=1.6,format=yuv420p[c];[a][b][c]concat=n=3:v=1[o]",
		"-map", "[o]", "-c:v", "libvpx", "-b:v", "600k", "-deadline", "realtime", "-g", "25", "-f", "ivf", "camera.ivf"); err != nil {
		t.Fatal(logOf(err))
	}
	silence := opusPackets(t, filepath.Join(dir, "silence.ogg"))
	beeps := map[string][][]byte{"ana": opusPackets(t, filepath.Join(dir, "beep700p.ogg")), "bruno": opusPackets(t, filepath.Join(dir, "beep1100p.ogg"))}

	room := fmt.Sprintf("rec-test-%d", time.Now().UnixNano())
	ana := &publisher{t: t, key: key, secret: secret, url: url, room: room, id: "ana"}
	bruno := &publisher{t: t, key: key, secret: secret, url: url, room: room, id: "bruno", lossy: true}
	ana.connect()
	bruno.connect()
	defer ana.r.Disconnect()

	capDir := filepath.Join(t.TempDir(), "capture")
	if err := os.MkdirAll(capDir, 0o750); err != nil {
		t.Fatal(err)
	}
	token, err := RecorderToken(key, secret, room, "01JTESTREC0000000000000000", time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	c := &Capture{Dir: capDir, URL: url, Token: token, Mode: "video", Start: time.Now(), MaxBytes: 1 << 30, MaxTime: time.Minute}
	stop := make(chan string, 1)
	var res CaptureResult
	var runErr error
	var wg sync.WaitGroup
	wg.Go(func() { res, runErr = c.Run(context.Background(), stop) })
	time.Sleep(1500 * time.Millisecond) // subscribed

	// Ana: a camera with a flash; markers at 0.5, 1.5 s, a 3 s quiet (a new file), markers after it
	camTrack, err := lksdk.NewLocalSampleTrack(webrtc.RTPCodecCapability{MimeType: webrtc.MimeTypeVP8, ClockRate: 90000})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := ana.r.LocalParticipant.PublishTrack(camTrack, &lksdk.TrackPublicationOptions{Name: "camera", Source: livekit.TrackSource_CAMERA,
		VideoWidth: 640, VideoHeight: 360}); err != nil {
		t.Fatal(err)
	}
	var flashAt time.Time
	var wgp sync.WaitGroup
	wgp.Go(func() {
		f, err := os.Open(filepath.Join(dir, "camera.ivf")) //nolint:gosec // test
		if err != nil {
			return
		}
		defer func() { _ = f.Close() }()
		r, _, err := ivfreader.NewWith(f)
		if err != nil {
			return
		}
		start := time.Now()
		var at time.Duration
		for i := 0; ; i++ {
			frame, _, err := r.ParseNextFrame()
			if err != nil {
				return
			}
			step := time.Duration(30+(i*7)%21) * time.Millisecond // a variable frame rate around 25 fps
			due := start.Add(at)
			at += step
			time.Sleep(time.Until(due))
			if i == 275 {
				flashAt = due
			}
			_ = camTrack.WriteSample(media.Sample{Data: frame, Duration: step}, nil)
		}
	})
	wgp.Go(func() { ana.speak(beeps["ana"], silence, 9000, []int{500, 1500, 6000, 7500}, [2]int{2500, 5500}) })
	// Bruno: markers, a mute (the SDK's), then he leaves and comes back (a new publication)
	wgp.Go(func() {
		bruno.speak(beeps["bruno"], silence, 3000, []int{1000, 2000}, [2]int{})
		bruno.micPub.SetMuted(true)
		time.Sleep(2 * time.Second)
		bruno.micPub.SetMuted(false)
		bruno.speak(beeps["bruno"], silence, 1500, []int{500}, [2]int{})
		bruno.r.Disconnect()
		time.Sleep(time.Second)
		bruno.connect()
		bruno.speak(beeps["bruno"], silence, 2000, []int{800}, [2]int{})
		bruno.r.Disconnect()
	})
	wgp.Wait()
	time.Sleep(500 * time.Millisecond)
	stop <- EndStopped
	wg.Wait()
	if runErr != nil {
		t.Fatal(runErr)
	}
	total := res.Ended.Sub(c.Start).Milliseconds()

	out, err := (&Composer{FFmpeg: ff, Mode: "video", Format: "webm"}).Compose(context.Background(), capDir, total, nil)
	if err != nil {
		t.Fatal(logOf(err))
	}
	x := pcm(t, ff, capDir, out.Path)
	near(t, "duration", int64(len(x))/48, total, 60)
	for who, freq := range map[string]float64{"ana": 700, "bruno": 1100} {
		p := ana
		if who == "bruno" {
			p = bruno
		}
		got := joinSpans(toneSpans(x, freq), 100) // a lost packet leaves a 20 ms hole in a beep
		if len(got) != len(p.markers) {
			t.Errorf("%s: %d beeps heard, %d sent: %v", who, len(got), len(p.markers), got)
			continue
		}
		for i, m := range p.markers {
			want := m.Sub(c.Start).Milliseconds()
			// the marker's real time; the beep's onset in the recording must be within 100 ms of it
			if d := got[i][0] - want; d < -100 || d > 100 {
				t.Errorf("%s marker %d at %d ms heard at %d ms (%+d)", who, i, want, got[i][0], d)
			}
		}
	}
	tracks, err := LoadTracks(capDir)
	if err != nil {
		t.Fatal(err)
	}
	var audioFiles, videoFiles int
	for _, tr := range tracks {
		t.Logf("track %s %s %s %s: %+v %v", tr.Participant, tr.Kind, tr.Source, tr.Codec, tr.Segments, tr.Partial)
		if tr.Kind == KindAudio {
			audioFiles += len(tr.Segments)
		} else {
			videoFiles += len(tr.Segments)
		}
		if tr.Name == "" {
			t.Errorf("track of %s without a name", tr.Participant)
		}
	}
	// Ana: one file, a new one after the 3 s quiet; Bruno: one, one after unmuting, one after coming back
	if audioFiles < 5 {
		t.Errorf("%d audio files, want at least 5", audioFiles)
	}
	if flashAt.IsZero() {
		t.Fatal("no flash sent")
	}
	if videoFiles != 1 {
		t.Fatalf("%d video files, want Ana's camera", videoFiles)
	}
	// the flash: Ana's camera tile (the first of a grid of two, left half) turns white within 100 ms of when sent
	raw := filepath.Join(capDir, "frames.raw")
	if err := ff.Run(context.Background(), capDir, "frames", time.Minute, "-i", out.Path, "-vf", "scale=16:9,format=gray",
		"-f", "rawvideo", raw); err != nil {
		t.Fatal(logOf(err))
	}
	fr, err := os.ReadFile(raw)
	if err != nil {
		t.Fatal(err)
	}
	flash := -1
	for i := range len(fr) / 144 {
		// the middle of either half of the grid of two (who is first depends on who was heard first)
		if (fr[i*144+4*16+4] > 200 || fr[i*144+4*16+12] > 200) && flash < 0 {
			flash = i
		}
	}
	if flash < 0 {
		t.Fatal("the flash is not in the recording")
	}
	if d := int64(flash)*40 - flashAt.Sub(c.Start).Milliseconds(); d < -100 || d > 100 {
		t.Errorf("the flash sent at %d ms is seen at %d ms", flashAt.Sub(c.Start).Milliseconds(), flash*40)
	}
	t.Logf("recorded %d ms, %d bytes captured, partial %v", total, c.Bytes(), res.Partial)
}

// joinSpans merges spans less than gap ms apart.
func joinSpans(spans [][2]int64, gap int64) [][2]int64 {
	var out [][2]int64
	for _, sp := range spans {
		if n := len(out); n > 0 && sp[0]-out[n-1][1] < gap {
			out[n-1][1] = sp[1]
			continue
		}
		out = append(out, sp)
	}
	return out
}

// TestRecorderTrackLimit (design D8): a recording takes at most MaxTracks tracks; the rest are not captured and the
// recording says it is partial.
func TestRecorderTrackLimit(t *testing.T) {
	url, key, secret := liveKit(t)
	room := fmt.Sprintf("rec-limit-%d", time.Now().UnixNano())
	for i := range MaxTracks + 1 { // one microphone each, one more than a recording takes
		p := &publisher{t: t, key: key, secret: secret, url: url, room: room, id: fmt.Sprintf("p%02d", i)}
		p.connect()
		defer p.r.Disconnect()
	}
	dir := t.TempDir()
	token, err := RecorderToken(key, secret, room, "01JTESTLIMIT00000000000000", time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	c := &Capture{Dir: dir, URL: url, Token: token, Mode: "audio", Start: time.Now(), MaxBytes: 1 << 30, MaxTime: time.Minute}
	stop := make(chan string, 1)
	go func() {
		time.Sleep(4 * time.Second)
		stop <- EndStopped
	}()
	res, err := c.Run(context.Background(), stop)
	if err != nil {
		t.Fatal(err)
	}
	manifests, _ := filepath.Glob(filepath.Join(dir, "t*.jsonl"))
	if len(manifests) != MaxTracks {
		t.Errorf("%d tracks captured, want %d", len(manifests), MaxTracks)
	}
	if !slices.Contains(res.Partial, PartialTrackLimit) {
		t.Errorf("partial %v, want %s", res.Partial, PartialTrackLimit)
	}
}
