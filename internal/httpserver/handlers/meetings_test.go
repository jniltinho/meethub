package handlers_test

import (
	"crypto/sha256"
	"encoding/base64"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/livekit/protocol/auth"

	"github.com/jniltinho/meethub/internal/chat"
	"github.com/jniltinho/meethub/internal/database"
	"github.com/jniltinho/meethub/internal/database/dbtest"
	"github.com/jniltinho/meethub/internal/meetings"
)

const (
	lkKey    = "test-key"
	lkSecret = "a-test-secret-of-at-least-32-bytes!!"
)

// postWebhook sends a LiveKit webhook body signed with secret ("" sends it unsigned).
func postWebhook(e *env, body, secret string) int {
	e.t.Helper()
	r := httptest.NewRequest(http.MethodPost, "/api/v1/meetings/webhook", strings.NewReader(body))
	r.Header.Set("Content-Type", "application/webhook+json")
	if secret != "" {
		sum := sha256.Sum256([]byte(body))
		tok, err := auth.NewAccessToken(lkKey, secret).SetValidFor(time.Minute).
			SetSha256(base64.StdEncoding.EncodeToString(sum[:])).ToJWT()
		if err != nil {
			e.t.Fatal(err)
		}
		r.Header.Set("Authorization", tok)
	}
	w := httptest.NewRecorder()
	e.srv.Handler().ServeHTTP(w, r)
	return w.Code
}

func TestMeetingsAPI(t *testing.T) {
	dbtest.Each(t, func(t *testing.T, db *database.DB) {
		e := newEnv(t, db, false)
		chatSvc := &chat.Service{DB: db}
		e.api.Chat = chatSvc
		ana := e.setupOwner()
		ana.createUser("bia@acme.io", "member")
		biaC := e.login("bia@acme.io", "a long password!")

		// meetings off: the list says so, the rest answers 503
		if r := ana.do("GET", "/meetings", nil); r.Code != 200 || r.body["enabled"] != false {
			t.Fatalf("list while disabled: %d %s", r.Code, r.Body.String())
		}
		e.api.Meetings = &meetings.Service{DB: db, Chat: chatSvc}
		team := ana.do("POST", "/teams", map[string]string{"name": "eng", "display_name": "Eng"}).body["id"].(string)
		ch := ana.do("POST", "/channels", map[string]string{"team_id": team, "display_name": "Projects", "type": "O"}).body["id"].(string)
		if r := ana.do("POST", "/channels/"+ch+"/meeting/token", nil); r.Code != 503 || r.code() != "meetings_unavailable" {
			t.Fatalf("token while disabled: %d %s", r.Code, r.Body.String())
		}
		if code := postWebhook(e, `{"event":"room_finished"}`, lkSecret); code != 503 {
			t.Fatalf("webhook while disabled: %d", code)
		}

		e.api.Meetings = &meetings.Service{DB: db, Chat: chatSvc, URL: "ws://livekit:7880", APIKey: lkKey, APISecret: lkSecret}
		r := ana.do("POST", "/channels/"+ch+"/meeting/token", nil)
		if r.Code != 200 || r.body["url"] != "ws://livekit:7880" || r.body["token"] == "" || r.Header().Get("Cache-Control") != "no-store" {
			t.Fatalf("token: %d %s", r.Code, r.Body.String())
		}
		if strings.Contains(r.Body.String(), lkSecret) {
			t.Fatal("the response contains the API secret")
		}
		v, err := auth.ParseAPIToken(r.body["token"].(string))
		if err != nil {
			t.Fatal(err)
		}
		_, grants, err := v.Verify(lkSecret)
		if err != nil || grants.Name != "Ana" {
			t.Fatalf("grants %+v %v", grants, err)
		}
		room := grants.Video.Room

		// bia is in the team but not in the channel; then outside the team entirely
		biaC.do("POST", "/teams/"+team+"/members", map[string]string{})
		if r := biaC.do("POST", "/channels/"+ch+"/meeting/token", nil); r.Code != 403 || r.code() != "forbidden" {
			t.Fatalf("non-member token: %d %s", r.Code, r.Body.String())
		}
		if r := biaC.do("GET", "/channels/"+ch+"/meeting", nil); r.Code != 403 {
			t.Fatalf("non-member room: %d", r.Code)
		}
		// CSRF still applies to the token route
		if r := biaC.do("POST", "/channels/"+ch+"/meeting/token", nil, func(r *http.Request) { r.Header.Del("X-CSRF-Token") }); r.Code != 403 || r.code() != "csrf_failed" {
			t.Fatalf("token without CSRF: %d %s", r.Code, r.Body.String())
		}

		anaID := ana.do("GET", "/me", nil).body["user"].(map[string]any)["id"].(string)
		joined := fmt.Sprintf(`{"event":"participant_joined","room":{"name":%q},"participant":{"identity":%q,"sid":"PA_1"}}`, room, anaID)
		for name, secret := range map[string]string{"unsigned": "", "wrong secret": "another-secret-of-at-least-32-bytes!!"} {
			if code := postWebhook(e, joined, secret); code != 401 {
				t.Fatalf("%s webhook: %d", name, code)
			}
		}
		if r := ana.do("GET", "/channels/"+ch+"/meeting", nil); r.Code != 200 || len(r.body["participants"].([]any)) != 0 {
			t.Fatalf("a rejected webhook changed the room: %s", r.Body.String())
		}
		if code := postWebhook(e, joined, lkSecret); code != 200 {
			t.Fatalf("signed webhook: %d", code)
		}
		r = ana.do("GET", "/channels/"+ch+"/meeting", nil)
		if ps := r.body["participants"].([]any); len(ps) != 1 || ps[0].(map[string]any)["user_id"] != anaID {
			t.Fatalf("room after join: %s", r.Body.String())
		}
		r = ana.do("GET", "/meetings", nil)
		if rooms := r.body["rooms"].([]any); r.body["enabled"] != true || len(rooms) != 1 || rooms[0].(map[string]any)["channel_id"] != ch {
			t.Fatalf("list: %s", r.Body.String())
		}
		if r := biaC.do("GET", "/meetings", nil); len(r.body["rooms"].([]any)) != 0 {
			t.Fatalf("bia sees a room she cannot read: %s", r.Body.String())
		}
		huge := `{"event":"room_finished","room":{"name":"` + strings.Repeat("x", 70<<10) + `"}}`
		if code := postWebhook(e, huge, lkSecret); code == 200 {
			t.Fatal("an oversized webhook was accepted")
		}
	})
}

func TestTamperedWebhookBody(t *testing.T) {
	db := dbtest.Open(t, "sqlite")
	e := newEnv(t, db, false)
	e.api.Meetings = &meetings.Service{DB: db, Chat: &chat.Service{DB: db}, URL: "ws://lk", APIKey: lkKey, APISecret: lkSecret}
	sum := sha256.Sum256([]byte(`{"event":"room_started"}`))
	tok, err := auth.NewAccessToken(lkKey, lkSecret).SetValidFor(time.Minute).SetSha256(base64.StdEncoding.EncodeToString(sum[:])).ToJWT()
	if err != nil {
		t.Fatal(err)
	}
	r := httptest.NewRequest(http.MethodPost, "/api/v1/meetings/webhook", strings.NewReader(`{"event":"room_finished"}`))
	r.Header.Set("Authorization", tok)
	w := httptest.NewRecorder()
	e.srv.Handler().ServeHTTP(w, r)
	if w.Code != 401 {
		t.Fatalf("tampered body: %d", w.Code)
	}
}
