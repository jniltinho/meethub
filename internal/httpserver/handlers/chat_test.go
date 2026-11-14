package handlers_test

import (
	"context"
	"encoding/json"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/jniltinho/meethub/internal/chat"
	"github.com/jniltinho/meethub/internal/database"
	"github.com/jniltinho/meethub/internal/database/dbtest"
	"github.com/jniltinho/meethub/internal/presence"
)

// memStatuses is an in-memory StatusStore.
type memStatuses struct {
	mu  sync.Mutex
	dnd map[string]time.Time
}

func (m *memStatuses) Get(_ context.Context, ids []string) ([]presence.Status, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	out := make([]presence.Status, len(ids))
	for i, id := range ids {
		out[i] = presence.Status{UserID: id, Status: presence.Online}
		if until, ok := m.dnd[id]; ok {
			out[i].Status, out[i].DNDUntil = presence.DND, until.UnixMilli()
		}
	}
	return out, nil
}

func (m *memStatuses) SetDND(_ context.Context, id string, until time.Time) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.dnd[id] = until
	return nil
}

func (m *memStatuses) ClearDND(_ context.Context, id string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	delete(m.dnd, id)
	return nil
}

func (m *memStatuses) Activity(context.Context, string) error { return nil }

type denyAll struct{}

func (denyAll) Allow(context.Context, string) (bool, time.Duration) { return false, 2 * time.Second }

// chatFlow drives the API as ana (owner), bia (member) and gus (guest) and
// checks that no ordinary response contains another user's e-mail.
type chatFlow struct {
	t      *testing.T
	emails map[*client]string // each client's own e-mail
	all    []string
}

func (f *chatFlow) call(c *client, method, path string, body any, want int, code ...string) resp {
	f.t.Helper()
	r := c.do(method, path, body)
	if r.Code != want || (len(code) > 0 && r.code() != code[0]) {
		f.t.Fatalf("%s %s: %d %s, want %d %v", method, path, r.Code, r.Body.String(), want, code)
	}
	for _, e := range f.all {
		if e != f.emails[c] && strings.Contains(r.Body.String(), e) {
			f.t.Fatalf("%s %s leaked %s", method, path, e)
		}
	}
	return r
}

func items(t *testing.T, r resp) []map[string]any {
	t.Helper()
	var p struct {
		Items []map[string]any `json:"items"`
	}
	if err := json.Unmarshal(r.Body.Bytes(), &p); err != nil {
		t.Fatal(err)
	}
	return p.Items
}

func TestChatAPI(t *testing.T) {
	dbtest.Each(t, func(t *testing.T, db *database.DB) {
		e := newEnv(t, db, false)
		svc := &chat.Service{DB: db}
		e.api.Chat, e.api.Presence = svc, &memStatuses{dnd: map[string]time.Time{}}
		ana := e.setupOwner()
		mk := func(name, role string) string { // display names differ from e-mails, so the leak check is meaningful
			r := ana.do("POST", "/admin/users", map[string]string{"email": name + "@acme.io", "display_name": strings.ToUpper(name), "role": role, "password": "a long password!"})
			if r.Code != 201 {
				t.Fatalf("create %s: %d %s", name, r.Code, r.Body.String())
			}
			return r.body["id"].(string)
		}
		biaID, gusID, carlID := mk("bia", "member"), mk("gus", "guest"), mk("carl", "member")
		bia, gus := e.login("bia@acme.io", "a long password!"), e.login("gus@acme.io", "a long password!")
		f := &chatFlow{t: t, emails: map[*client]string{ana: "ana@acme.io", bia: "bia@acme.io", gus: "gus@acme.io"},
			all: []string{"ana@acme.io", "bia@acme.io", "gus@acme.io", "carl@acme.io"}}
		anaID := ana.do("GET", "/me", nil).body["user"].(map[string]any)["id"].(string)

		// teams
		team := f.call(ana, "POST", "/teams", map[string]string{"name": "eng", "display_name": "Engineering"}, 201).body["id"].(string)
		f.call(ana, "POST", "/teams", map[string]string{"name": "eng", "display_name": "Again"}, 409, "name_taken")
		f.call(bia, "POST", "/teams", map[string]string{"name": "mine", "display_name": "Mine"}, 403, "forbidden")
		f.call(bia, "GET", "/teams/"+team, nil, 200)
		f.call(gus, "GET", "/teams/"+team, nil, 404, "not_found")
		if got := items(t, f.call(bia, "GET", "/teams", nil, 200)); len(got) != 1 {
			t.Fatalf("joinable teams: %v", got)
		}
		f.call(bia, "POST", "/teams/"+team+"/members", map[string]string{}, 204)
		f.call(gus, "POST", "/teams/"+team+"/members", map[string]string{}, 403, "forbidden")
		f.call(ana, "POST", "/teams/"+team+"/members", map[string]string{"user_id": gusID}, 204)
		if mine := items(t, f.call(bia, "GET", "/users/me/teams", nil, 200)); len(mine) != 1 || mine[0]["role"] != chat.TeamUser {
			t.Fatalf("my teams: %v", mine)
		}
		if m := items(t, f.call(bia, "GET", "/teams/"+team+"/members", nil, 200)); len(m) != 3 || m[0]["username"] == "" {
			t.Fatalf("team members: %v", m)
		}
		f.call(ana, "PUT", "/teams/"+team+"/members/"+biaID+"/role", map[string]string{"role": "boss"}, 400, "invalid_input")
		f.call(bia, "PUT", "/teams/"+team+"/members/"+biaID+"/role", map[string]string{"role": chat.TeamAdmin}, 403, "forbidden")
		desc := "Builders"
		f.call(ana, "PATCH", "/teams/"+team, map[string]any{"description": desc}, 200)
		f.call(ana, "PATCH", "/teams/"+team, map[string]any{"color": "red"}, 400, "invalid_input")

		// channels
		var general string
		for _, ch := range items(t, f.call(bia, "GET", "/users/me/teams/"+team+"/channels", nil, 200)) {
			if ch["is_default"] == true {
				general = ch["id"].(string)
			}
		}
		f.call(bia, "DELETE", "/channels/"+general+"/members/"+biaID, nil, 409, "default_channel")
		proj := f.call(bia, "POST", "/channels", map[string]string{"team_id": team, "display_name": "Projects", "type": "O"}, 201).body["id"].(string)
		f.call(bia, "POST", "/channels", map[string]string{"team_id": team, "name": "projects", "display_name": "P", "type": "O"}, 409, "name_taken")
		f.call(gus, "POST", "/channels", map[string]string{"team_id": team, "display_name": "G", "type": "O"}, 403, "forbidden")
		if j := items(t, f.call(ana, "GET", "/teams/"+team+"/channels", nil, 200)); len(j) != 1 {
			t.Fatalf("joinable channels: %v", j)
		}
		f.call(ana, "POST", "/channels/"+proj+"/members", map[string]string{}, 204)
		f.call(bia, "POST", "/channels/"+proj+"/members", map[string]string{"user_id": carlID}, 400, "not_team_member")
		f.call(gus, "GET", "/channels/"+proj+"/members", nil, 404, "not_found") // guests do not discover public channels
		if m := items(t, f.call(ana, "GET", "/channels/"+proj+"/members", nil, 200)); len(m) != 2 {
			t.Fatalf("channel members: %v", m)
		}
		header := "Deploys on Fridays: no"
		f.call(bia, "PATCH", "/channels/"+proj, map[string]any{"header": header}, 200)
		f.call(gus, "PATCH", "/channels/"+proj, map[string]any{"header": "x"}, 404, "not_found")
		f.call(bia, "PUT", "/channels/"+proj+"/members/"+anaID+"/role", map[string]string{"role": chat.ChannelAdmin}, 204)
		if ch := f.call(ana, "GET", "/channels/"+proj, nil, 200).body; ch["header"] != header || ch["member_count"] != float64(2) {
			t.Fatalf("channel: %v", ch)
		}
		priv := f.call(ana, "POST", "/channels", map[string]string{"team_id": team, "display_name": "Finance", "type": "P"}, 201).body["id"].(string)
		f.call(bia, "GET", "/channels/"+priv, nil, 404, "not_found")
		f.call(bia, "GET", "/users?in_channel="+priv, nil, 404, "not_found")

		// posts, threads, reactions, read state
		post := f.call(bia, "POST", "/posts", map[string]string{"channel_id": proj, "message": "Deploy at 17h", "pending_post_id": "p-1"}, 201).body
		pid := post["id"].(string)
		f.call(bia, "POST", "/posts", map[string]string{"channel_id": proj, "message": "Deploy at 17h", "pending_post_id": "p-1"}, 201)
		f.call(bia, "POST", "/posts", map[string]string{"channel_id": proj, "message": "changed", "pending_post_id": "p-1"}, 409, "pending_post_conflict")
		f.call(gus, "POST", "/posts", map[string]string{"channel_id": proj, "message": "hi", "pending_post_id": "g-1"}, 404, "not_found")
		f.call(bia, "POST", "/posts", map[string]string{"channel_id": proj, "message": " ", "pending_post_id": "p-2"}, 400, "invalid_input")
		f.call(ana, "POST", "/posts", map[string]string{"channel_id": proj, "root_id": pid, "message": "ok", "pending_post_id": "a-1"}, 201)
		if th := items(t, f.call(bia, "GET", "/posts/"+pid+"/thread", nil, 200)); len(th) != 2 || th[0]["reply_count"] != float64(1) {
			t.Fatalf("thread: %v", th)
		}
		f.call(ana, "PATCH", "/posts/"+pid, map[string]string{"message": "hacked"}, 403, "forbidden")
		f.call(bia, "PATCH", "/posts/"+pid, map[string]string{"message": "Deploy at 18h"}, 200)
		f.call(ana, "POST", "/reactions", map[string]string{"post_id": pid, "emoji_name": "rocket"}, 204)
		f.call(ana, "POST", "/reactions", map[string]string{"post_id": pid, "emoji_name": "not_an_emoji"}, 400, "invalid_input")
		hist := items(t, f.call(ana, "GET", "/channels/"+proj+"/posts", nil, 200))
		if len(hist) != 2 || hist[1]["message"] != "Deploy at 18h" || hist[1]["reactions"] == nil {
			t.Fatalf("history: %v", hist)
		}
		f.call(ana, "DELETE", "/users/me/posts/"+pid+"/reactions/rocket", nil, 204)
		changes := f.call(ana, "GET", "/channels/"+proj+"/posts?since_revision=0", nil, 200).body
		if len(changes["posts"].([]any)) != 2 || changes["has_more"] != false {
			t.Fatalf("changes: %v", changes)
		}
		f.call(ana, "GET", "/channels/"+proj+"/posts?since_revision=x", nil, 400, "invalid_input")
		f.call(ana, "POST", "/channels/"+proj+"/view", nil, 204)
		// favorites, a window of history around a message, search
		f.call(bia, "PUT", "/users/me/channels/"+proj+"/favorite", map[string]bool{"favorite": true}, 204)
		f.call(bia, "PUT", "/users/me/channels/01J0000000000000000000NONE/favorite", map[string]bool{"favorite": true}, 404, "not_found")
		if win := items(t, f.call(bia, "GET", "/channels/"+proj+"/posts?around="+pid, nil, 200)); len(win) == 0 {
			t.Fatal("empty window around a message")
		}
		f.call(bia, "GET", "/channels/"+proj+"/posts?around=01J0000000000000000000NONE", nil, 404, "not_found")
		f.call(bia, "GET", "/channels/"+proj+"/posts?after=bad", nil, 400, "invalid_input")
		found := f.call(bia, "POST", "/posts/search", map[string]any{"terms": "DEPLOY from:bia"}, 200).body
		if posts, _ := found["posts"].([]any); len(posts) != 1 || found["channels"].(map[string]any)[proj] == nil {
			t.Fatalf("search: %v", found)
		}
		short := f.call(bia, "POST", "/posts/search", map[string]any{"terms": "a"}, 400, "invalid_input").body
		if msg, _ := short["error"].(map[string]any)["message"].(string); !strings.Contains(msg, "at least 2") {
			t.Fatalf("the search error does not say what is wrong: %v", short)
		}
		// pins: any member, idempotent, listed, gone with the message
		if p := f.call(ana, "POST", "/posts/"+pid+"/pin", nil, 200).body; p["is_pinned"] != true || p["pinned_by"] != anaID {
			t.Fatalf("pin: %v", p)
		}
		if p := f.call(bia, "POST", "/posts/"+pid+"/pin", nil, 200).body; p["pinned_by"] != anaID {
			t.Fatalf("pin twice: %v", p)
		}
		if pins := items(t, f.call(bia, "GET", "/channels/"+proj+"/pinned", nil, 200)); len(pins) != 1 || pins[0]["id"] != pid {
			t.Fatalf("pinned: %v", pins)
		}
		if p := f.call(bia, "POST", "/posts/"+pid+"/unpin", nil, 200).body; p["is_pinned"] != false || p["pinned_by"] != nil {
			t.Fatalf("unpin: %v", p)
		}
		f.call(bia, "POST", "/posts/"+pid+"/pin", nil, 200)
		f.call(ana, "POST", "/posts/01J0000000000000000000NONE/pin", nil, 404, "not_found")
		f.call(ana, "DELETE", "/posts/"+pid, nil, 204) // channel admin moderating
		if pins := items(t, f.call(bia, "GET", "/channels/"+proj+"/pinned", nil, 200)); len(pins) != 0 {
			t.Fatalf("pinned after the delete: %v", pins)
		}
		svc.Limiter = denyAll{}
		r := f.call(bia, "POST", "/posts", map[string]string{"channel_id": proj, "message": "flood", "pending_post_id": "p-9"}, 429, "rate_limited")
		if r.Header().Get("Retry-After") == "" {
			t.Fatal("429 without Retry-After")
		}
		svc.Limiter = nil
		f.call(bia, "POST", "/channels/"+proj+"/archive", nil, 204)
		f.call(bia, "POST", "/posts", map[string]string{"channel_id": proj, "message": "late", "pending_post_id": "p-3"}, 403, "channel_archived")
		f.call(bia, "POST", "/channels/"+proj+"/restore", nil, 204)
		f.call(bia, "DELETE", "/channels/"+proj+"/members/"+anaID, nil, 204)

		// conversations
		dm := f.call(bia, "POST", "/channels/direct", map[string]string{"user_id": anaID}, 200).body["id"].(string)
		f.call(bia, "POST", "/channels/"+dm+"/members", map[string]string{"user_id": gusID}, 400, "direct_channel")
		f.call(bia, "POST", "/channels/group", map[string]any{"user_ids": []string{anaID}}, 400, "invalid_input")
		f.call(bia, "POST", "/channels/group", map[string]any{"user_ids": []string{anaID, gusID}}, 200)
		if conv := items(t, f.call(bia, "GET", "/users/me/conversations", nil, 200)); len(conv) != 2 || conv[0]["members"] == nil {
			t.Fatalf("conversations: %v", conv)
		}
		f.call(bia, "PUT", "/users/me/conversations/"+dm+"/visibility", map[string]bool{"visible": false}, 204)
		if r := ana.do("PATCH", "/admin/users/"+carlID, map[string]any{"status": "disabled"}); r.Code != 200 { // administration shows e-mails
			t.Fatalf("disable: %d", r.Code)
		}
		f.call(bia, "POST", "/channels/direct", map[string]string{"user_id": carlID}, 400, "user_inactive")

		// directory and statuses
		if u := items(t, f.call(bia, "GET", "/users?q=gu", nil, 200)); len(u) != 1 || u[0]["id"] != gusID {
			t.Fatalf("directory search: %v", u)
		}
		if u := items(t, f.call(gus, "GET", "/users", nil, 200)); len(u) != 3 { // general/group mates and himself
			t.Fatalf("guest directory: %v", u)
		}
		if u := items(t, f.call(bia, "GET", "/users?in_team="+team, nil, 200)); len(u) != 3 {
			t.Fatalf("team directory: %v", u)
		}
		// profiles by id: a disabled author is still shown; the guest only sees people he shares a channel with
		var prof []map[string]any
		_ = json.Unmarshal(f.call(bia, "POST", "/users/ids", []string{carlID, anaID}, 200).Body.Bytes(), &prof)
		if len(prof) != 2 {
			t.Fatalf("profiles: %v", prof)
		}
		_ = json.Unmarshal(f.call(gus, "POST", "/users/ids", []string{carlID, anaID}, 200).Body.Bytes(), &prof)
		if len(prof) != 1 || prof[0]["id"] != anaID {
			t.Fatalf("guest profiles: %v", prof)
		}
		st := f.call(bia, "POST", "/users/status/ids", []string{anaID, gusID, "01UNKNOWN0000000000000000X"}, 200)
		var sts []presence.Status
		_ = json.Unmarshal(st.Body.Bytes(), &sts)
		if len(sts) != 2 {
			t.Fatalf("statuses: %s", st.Body.String())
		}
		f.call(bia, "PUT", "/users/me/status", map[string]any{"status": "dnd"}, 200)
		f.call(bia, "PUT", "/users/me/status", map[string]any{"status": "sleeping"}, 400, "invalid_input")
		f.call(bia, "PUT", "/users/me/status", map[string]any{"status": "online"}, 200)

		// team administration
		f.call(ana, "DELETE", "/teams/"+team+"/members/"+gusID, nil, 204)
		db.Gorm.Exec(`UPDATE teams SET is_default = ? WHERE id = ?`, true, team)
		f.call(ana, "POST", "/teams/"+team+"/archive", nil, 409, "default_team")
		db.Gorm.Exec(`UPDATE teams SET is_default = ? WHERE id = ?`, false, team)
		f.call(ana, "POST", "/teams/"+team+"/archive", nil, 204)
		f.call(ana, "POST", "/teams/"+team+"/restore", nil, 204)
		_ = biaID
	})
}
