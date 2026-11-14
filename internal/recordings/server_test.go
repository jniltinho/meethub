package recordings

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/cookiejar"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/livekit/protocol/livekit"
	lksdk "github.com/livekit/server-sdk-go/v2"
	"github.com/pion/webrtc/v4"
)

// TestServerRecording records through a running MeetHub server (design D11, task 0.4): it signs in, makes a
// channel, joins its room as a publisher with the server's token, starts and stops a recording through the API and
// finds its tone markers in the posted file. It runs against the Compose stack or inside the all-in-one image's
// network namespace: MEETHUB_TEST_SERVER_URL (and _EMAIL, _PASSWORD of an owner, _ORIGIN when the server's public
// URL is not http://localhost:8080); MEETHUB_TEST_LIVEKIT_URL, when
// set, replaces the browser URL of the server's tokens (the host's localhost in the Compose stack).
func TestServerRecording(t *testing.T) {
	base := os.Getenv("MEETHUB_TEST_SERVER_URL")
	if base == "" {
		t.Skip("MEETHUB_TEST_SERVER_URL not set")
	}
	origin := os.Getenv("MEETHUB_TEST_SERVER_ORIGIN") // the server's http.public_url origin
	if origin == "" {
		origin = "http://localhost:8080"
	}
	ff := testFFmpeg(t)
	jar, _ := cookiejar.New(nil)
	hc := &http.Client{Jar: jar, Timeout: 30 * time.Second}
	csrf := ""
	call := func(method, path string, body any, out any) int {
		t.Helper()
		var rd io.Reader
		if body != nil {
			b, _ := json.Marshal(body)
			rd = bytes.NewReader(b)
		}
		req, _ := http.NewRequestWithContext(context.Background(), method, base+"/api/v1"+path, rd)
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("Origin", origin)
		if csrf != "" {
			req.Header.Set("X-CSRF-Token", csrf)
		}
		res, err := hc.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		defer func() { _ = res.Body.Close() }()
		b, _ := io.ReadAll(res.Body)
		if out != nil && len(b) > 0 {
			_ = json.Unmarshal(b, out)
		}
		if res.StatusCode >= 400 {
			t.Logf("%s %s: %d %s", method, path, res.StatusCode, b)
		}
		return res.StatusCode
	}
	var me struct {
		CSRF string `json:"csrf_token"`
		User struct {
			ID string `json:"id"`
		} `json:"user"`
	}
	if code := call("POST", "/auth/login", map[string]string{"email": os.Getenv("MEETHUB_TEST_SERVER_EMAIL"),
		"password": os.Getenv("MEETHUB_TEST_SERVER_PASSWORD")}, nil); code != 200 {
		t.Fatalf("login: %d", code)
	}
	call("GET", "/me", nil, &me)
	csrf = me.CSRF
	var teams struct {
		Items []struct {
			ID string `json:"id"`
		} `json:"items"`
	}
	call("GET", "/users/me/teams", nil, &teams)
	if len(teams.Items) == 0 {
		t.Fatal("no team")
	}
	var ch struct {
		ID string `json:"id"`
	}
	if code := call("POST", "/channels", map[string]string{"team_id": teams.Items[0].ID,
		"display_name": fmt.Sprintf("rec-%d", time.Now().UnixNano()%1_000_000), "type": "O"}, &ch); code != 201 && code != 200 {
		t.Fatalf("channel: %d", code)
	}
	var tok struct {
		URL   string `json:"url"`
		Token string `json:"token"`
	}
	if code := call("POST", "/channels/"+ch.ID+"/meeting/token", map[string]string{}, &tok); code != 200 {
		t.Fatalf("token: %d", code)
	}
	if u := os.Getenv("MEETHUB_TEST_LIVEKIT_URL"); u != "" {
		tok.URL = u
	}

	dir := t.TempDir()
	tone(t, ff, dir, "beep.ogg", 900, 200)
	if err := ff.Run(context.Background(), dir, "gen", time.Minute, "-i", "beep.ogg", "-c", "copy", "-page_duration", "20000", "beepp.ogg"); err != nil {
		t.Fatal(logOf(err))
	}
	if err := ff.Run(context.Background(), dir, "gen", time.Minute, "-filter_complex", "sine=f=100:sample_rate=48000:d=1,volume=0[o]",
		"-map", "[o]", "-c:a", "libopus", "-b:a", "32k", "-page_duration", "20000", "silence.ogg"); err != nil {
		t.Fatal(logOf(err))
	}
	r, err := lksdk.ConnectToRoomWithToken(tok.URL, tok.Token, lksdk.NewRoomCallback())
	if err != nil {
		t.Fatalf("publisher: %v", err)
	}
	defer r.Disconnect()
	mic, err := lksdk.NewLocalSampleTrack(webrtc.RTPCodecCapability{MimeType: webrtc.MimeTypeOpus, ClockRate: 48000, Channels: 2})
	if err != nil {
		t.Fatal(err)
	}
	pub, err := r.LocalParticipant.PublishTrack(mic, &lksdk.TrackPublicationOptions{Name: "mic", Source: livekit.TrackSource_MICROPHONE})
	if err != nil {
		t.Fatal(err)
	}
	p := &publisher{t: t, r: r, mic: mic, micPub: pub}
	beep, silence := opusPackets(t, filepath.Join(dir, "beepp.ogg")), opusPackets(t, filepath.Join(dir, "silence.ogg"))

	// in the room (LiveKit's webhook reached the server)
	deadline := time.Now().Add(30 * time.Second)
	for {
		var room struct {
			Participants []struct {
				UserID string `json:"user_id"`
			} `json:"participants"`
		}
		call("GET", "/channels/"+ch.ID+"/meeting", nil, &room)
		if len(room.Participants) == 1 {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("the server never saw the publisher in the room")
		}
		time.Sleep(time.Second)
	}
	var rec struct {
		ID        string `json:"id"`
		StartedAt int64  `json:"started_at"`
	}
	if code := call("POST", "/channels/"+ch.ID+"/meeting/recording", map[string]string{}, &rec); code != 201 {
		t.Fatalf("start: %d", code)
	}
	time.Sleep(2 * time.Second) // the recorder joins and subscribes
	p.speak(beep, silence, 6000, []int{1000, 3000, 5000}, [2]int{})
	if code := call("DELETE", "/channels/"+ch.ID+"/meeting/recording", nil, nil); code != 202 {
		t.Fatalf("stop: %d", code)
	}
	deadline = time.Now().Add(2 * time.Minute)
	var list struct {
		Recordings []struct {
			ID         string   `json:"id"`
			Status     string   `json:"status"`
			FileID     string   `json:"file_id"`
			DurationMS int64    `json:"duration_ms"`
			Reasons    []string `json:"partial_reasons"`
		} `json:"recordings"`
	}
	for {
		call("GET", "/channels/"+ch.ID+"/meeting/recordings", nil, &list)
		if len(list.Recordings) > 0 && (list.Recordings[0].Status == StatusReady || list.Recordings[0].Status == StatusFailed) {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("not posted within 2 minutes: %+v", list)
		}
		time.Sleep(time.Second)
	}
	got := list.Recordings[0]
	if got.Status != StatusReady || got.ID != rec.ID {
		t.Fatalf("recording %+v", got)
	}
	req, _ := http.NewRequestWithContext(context.Background(), "GET", base+"/api/v1/files/"+got.FileID, nil)
	res, err := hc.Do(req)
	if err != nil || res.StatusCode != 200 {
		t.Fatalf("download: %v %v", err, res)
	}
	file := filepath.Join(dir, "recording.m4a")
	b, _ := io.ReadAll(res.Body)
	_ = res.Body.Close()
	if err := os.WriteFile(file, b, 0o600); err != nil {
		t.Fatal(err)
	}
	beeps := toneSpans(pcm(t, ff, dir, file), 900)
	if len(beeps) != len(p.markers) {
		t.Fatalf("%d beeps in the posted file, %d sent", len(beeps), len(p.markers))
	}
	for i, m := range p.markers {
		want := m.UnixMilli() - rec.StartedAt
		if d := beeps[i][0] - want; d < -100 || d > 100 {
			t.Errorf("marker %d sent at %d ms is heard at %d ms", i, want, beeps[i][0])
		}
	}
	t.Logf("posted %d ms, partial %v, markers %d", got.DurationMS, got.Reasons, len(p.markers))
}
