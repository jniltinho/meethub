package meetings_test

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/livekit/protocol/auth"
	"github.com/livekit/protocol/livekit"

	"github.com/jniltinho/meethub/internal/chat"
	"github.com/jniltinho/meethub/internal/database"
	"github.com/jniltinho/meethub/internal/database/dbtest"
	"github.com/jniltinho/meethub/internal/identity"
	"github.com/jniltinho/meethub/internal/meetings"
	"github.com/jniltinho/meethub/internal/org"
)

var ctx = context.Background()

const (
	key    = "test-key"
	secret = "a-test-secret-of-at-least-32-bytes!!"
)

// recorder keeps the published events.
type recorder struct {
	mu     sync.Mutex
	events []chat.Event
}

func (r *recorder) Publish(_ context.Context, events ...chat.Event) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.events = append(r.events, events...)
}

func (r *recorder) last() (chat.Event, int) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if len(r.events) == 0 {
		return chat.Event{}, 0
	}
	return r.events[len(r.events)-1], len(r.events)
}

// fakeRooms is a LiveKit room service in memory.
type fakeRooms struct {
	mu      sync.Mutex
	live    map[string][]meetings.LiveParticipant
	removed []string // room/identity
	fail    bool
}

func (f *fakeRooms) ListRooms(context.Context) ([]string, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	var out []string
	for r := range f.live {
		out = append(out, r)
	}
	return out, nil
}

func (f *fakeRooms) ListParticipants(_ context.Context, room string) ([]meetings.LiveParticipant, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.live[room], nil
}

func (f *fakeRooms) RemoveParticipant(_ context.Context, room, identity string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.fail {
		return errors.New("livekit down")
	}
	f.removed = append(f.removed, room+"/"+identity)
	return nil
}

type world struct {
	db                  *database.DB
	chat                *chat.Service
	svc                 *meetings.Service
	rec                 *recorder
	rooms               *fakeRooms
	owner, ana, bob, cy chat.Subject
	team                *chat.Team
	public, private     *chat.Channel
}

func newWorld(t *testing.T, db *database.DB) *world {
	t.Helper()
	if _, err := org.Create(ctx, db.Gorm, "Acme"); err != nil {
		t.Fatal(err)
	}
	users := &org.Users{DB: db, Hasher: identity.NewHasher(identity.Argon2Params{MemoryKiB: 1024, Time: 1, Threads: 1}, 8, time.Second)}
	mk := func(name, role string) chat.Subject {
		t.Helper()
		m, err := users.Create(ctx, org.SystemActor, org.NewUser{Email: name + "@acme.io", DisplayName: name, Role: role, Password: "a long password!"})
		if err != nil {
			t.Fatal(err)
		}
		return chat.Subject{UserID: m.ID, OrgRole: role}
	}
	w := &world{db: db, rec: &recorder{}, rooms: &fakeRooms{live: map[string][]meetings.LiveParticipant{}}}
	w.chat = &chat.Service{DB: db}
	w.owner, w.ana, w.bob, w.cy = mk("owner", org.RoleOwner), mk("ana", org.RoleMember), mk("bob", org.RoleMember), mk("cy", org.RoleMember)
	w.svc = &meetings.Service{
		DB: db, Chat: w.chat, Publisher: w.rec, Rooms: w.rooms,
		URL: "ws://livekit:7880", APIKey: key, APISecret: secret,
	}
	team, err := w.chat.CreateTeam(ctx, w.owner, chat.NewTeam{Name: "eng", DisplayName: "Eng"})
	if err != nil {
		t.Fatal(err)
	}
	w.team = team
	for _, s := range []chat.Subject{w.ana, w.bob, w.cy} {
		if err := w.chat.AddTeamMember(ctx, w.owner, team.ID, s.UserID); err != nil {
			t.Fatal(err)
		}
	}
	if w.public, err = w.chat.CreateChannel(ctx, w.ana, team.ID, chat.NewChannel{Name: "projects", DisplayName: "Projects", Type: chat.ChannelOpen}); err != nil {
		t.Fatal(err)
	}
	if err := w.chat.JoinChannel(ctx, w.bob, w.public.ID); err != nil {
		t.Fatal(err)
	}
	if w.private, err = w.chat.CreateChannel(ctx, w.ana, team.ID, chat.NewChannel{Name: "board", DisplayName: "Board", Type: chat.ChannelPrivate}); err != nil {
		t.Fatal(err)
	}
	return w
}

func (w *world) token(t *testing.T, sub chat.Subject, channelID string) *auth.ClaimGrants {
	t.Helper()
	tok, err := w.svc.IssueToken(ctx, sub, "Name", channelID)
	if err != nil {
		t.Fatal(err)
	}
	v, err := auth.ParseAPIToken(tok.Token)
	if err != nil {
		t.Fatal(err)
	}
	_, grants, err := v.Verify(secret)
	if err != nil {
		t.Fatal(err)
	}
	return grants
}

func TestTokens(t *testing.T) {
	dbtest.Each(t, func(t *testing.T, db *database.DB) {
		w := newWorld(t, db)
		dm, err := w.chat.OpenDirect(ctx, w.ana, w.bob.UserID)
		if err != nil {
			t.Fatal(err)
		}

		// claims: identity, room only, short expiry, no administration
		before := time.Now()
		tok, err := w.svc.IssueToken(ctx, w.ana, "Ana", w.public.ID)
		if err != nil || tok.URL != "ws://livekit:7880" {
			t.Fatalf("token: %+v %v", tok, err)
		}
		if exp := time.UnixMilli(tok.ExpiresAt); exp.Before(before) || exp.After(before.Add(meetings.TokenTTL+time.Second)) {
			t.Fatalf("expires_at %v", exp)
		}
		v, err := auth.ParseAPIToken(tok.Token)
		if err != nil {
			t.Fatal(err)
		}
		claims, g, err := v.Verify(secret)
		if err != nil || v.Identity() != w.ana.UserID || v.APIKey() != key {
			t.Fatalf("verify: %v identity %q", err, v.Identity())
		}
		if claims.ExpiresAt == nil || claims.ExpiresAt.After(before.Add(meetings.TokenTTL+time.Second)) {
			t.Fatalf("exp %v", claims.ExpiresAt)
		}
		vg := g.Video
		if !vg.RoomJoin || vg.Room == "" || vg.RoomAdmin || vg.RoomCreate || vg.RoomRecord || vg.RoomList || vg.Hidden ||
			!vg.GetCanPublish() || !vg.GetCanSubscribe() || !vg.GetCanPublishData() || !vg.GetCanUpdateOwnMetadata() {
			t.Fatalf("grants: %+v", vg)
		}
		if !strings.HasPrefix(vg.Room, "mh_") || len(vg.Room) != 35 ||
			strings.Contains(vg.Room, w.public.ID) || strings.Contains(vg.Room, "projects") {
			t.Fatalf("room name %q", vg.Room)
		}

		// same room for every member, another for another channel
		if r := w.token(t, w.bob, w.public.ID).Video.Room; r != vg.Room {
			t.Fatalf("bob's room %q, ana's %q", r, vg.Room)
		}
		if r := w.token(t, w.ana, w.private.ID).Video.Room; r == vg.Room {
			t.Fatal("two channels share a room")
		}
		if r := w.token(t, w.bob, dm.ID).Video.Room; r == "" || r == vg.Room {
			t.Fatalf("conversation room %q", r)
		}

		// refusals follow the chat's read rule
		if err := w.chat.SetChannelArchived(ctx, w.ana, w.private.ID, true); err != nil {
			t.Fatal(err)
		}
		for _, c := range []struct {
			name    string
			sub     chat.Subject
			channel string
			want    error
		}{
			{"non-member of a public channel", w.cy, w.public.ID, chat.ErrForbidden},
			{"private channel of others", w.bob, w.private.ID, chat.ErrNotFound},
			{"conversation of others", w.cy, dm.ID, chat.ErrNotFound},
			{"unknown channel", w.ana, "01J00000000000000000000000", chat.ErrNotFound},
			{"archived channel", w.ana, w.private.ID, chat.ErrArchived},
		} {
			if _, err := w.svc.IssueToken(ctx, c.sub, "x", c.channel); !errors.Is(err, c.want) {
				t.Errorf("%s: %v, want %v", c.name, err, c.want)
			}
		}

		off := &meetings.Service{DB: db, Chat: w.chat}
		if _, err := off.IssueToken(ctx, w.ana, "x", w.public.ID); !errors.Is(err, meetings.ErrDisabled) {
			t.Fatalf("disabled: %v", err)
		}
		if rooms, err := off.List(ctx, w.ana); err != nil || len(rooms) != 0 {
			t.Fatalf("disabled list: %v %v", rooms, err)
		}
	})
}

func TestConcurrentFirstToken(t *testing.T) {
	dbtest.Each(t, func(t *testing.T, db *database.DB) {
		w := newWorld(t, db)
		names := make([]string, 8)
		var wg sync.WaitGroup
		for i := range names {
			wg.Go(func() {
				sub := w.ana
				if i%2 == 1 {
					sub = w.bob
				}
				tok, err := w.svc.IssueToken(ctx, sub, "x", w.public.ID)
				if err != nil {
					t.Error(err)
					return
				}
				v, _ := auth.ParseAPIToken(tok.Token)
				_, g, _ := v.Verify(secret)
				names[i] = g.Video.Room
			})
		}
		wg.Wait()
		var n int64
		if err := db.Gorm.Model(&meetings.Room{}).Where("channel_id = ?", w.public.ID).Count(&n).Error; err != nil || n != 1 {
			t.Fatalf("rooms: %d %v", n, err)
		}
		for _, name := range names {
			if name != names[0] {
				t.Fatalf("different rooms: %v", names)
			}
		}
	})
}

func event(name, room, identity, sid string) *livekit.WebhookEvent {
	ev := &livekit.WebhookEvent{Event: name, Room: &livekit.Room{Name: room}}
	if identity != "" {
		ev.Participant = &livekit.ParticipantInfo{Identity: identity, Sid: sid, JoinedAtMs: 1_800_000_000_000}
	}
	return ev
}

func people(t *testing.T, w *world, sub chat.Subject, channelID string) []string {
	t.Helper()
	r, err := w.svc.Room(ctx, sub, channelID)
	if err != nil {
		t.Fatal(err)
	}
	var out []string
	for _, p := range r.Participants {
		out = append(out, p.UserID)
	}
	slices.Sort(out)
	return out
}

func TestWebhookPresence(t *testing.T) {
	dbtest.Each(t, func(t *testing.T, db *database.DB) {
		w := newWorld(t, db)
		room := w.token(t, w.ana, w.public.ID).Video.Room
		apply := func(ev *livekit.WebhookEvent) {
			t.Helper()
			if err := w.svc.HandleWebhook(ctx, ev); err != nil {
				t.Fatal(err)
			}
		}

		apply(event("participant_joined", room, w.ana.UserID, "PA_1"))
		if got := people(t, w, w.bob, w.public.ID); !slices.Equal(got, []string{w.ana.UserID}) {
			t.Fatalf("after join: %v", got)
		}
		ev, _ := w.rec.last()
		recipients := slices.Sorted(slices.Values(ev.Recipients))
		if ev.Name != "meeting_updated" || ev.ChannelID != w.public.ID || ev.TeamID != w.team.ID || ev.Data["channel_id"] != w.public.ID ||
			!slices.Equal(recipients, slices.Sorted(slices.Values([]string{w.ana.UserID, w.bob.UserID}))) {
			t.Fatalf("event %+v", ev)
		}
		if ps, ok := ev.Data["participants"].([]meetings.ParticipantView); !ok || len(ps) != 1 || ps[0].JoinedAt != 1_800_000_000_000 {
			t.Fatalf("event participants %+v", ev.Data["participants"])
		}

		// a second tab replaces the session; the first session's late leave keeps her listed
		apply(event("participant_joined", room, w.ana.UserID, "PA_2"))
		apply(event("participant_left", room, w.ana.UserID, "PA_1"))
		if got := people(t, w, w.ana, w.public.ID); len(got) != 1 {
			t.Fatalf("stale leave removed the new session: %v", got)
		}
		apply(event("participant_joined", room, w.bob.UserID, "PB_1"))
		apply(event("participant_connection_aborted", room, w.ana.UserID, "PA_2"))
		if got := people(t, w, w.ana, w.public.ID); !slices.Equal(got, []string{w.bob.UserID}) {
			t.Fatalf("after aborted: %v", got)
		}
		apply(event("room_finished", room, "", ""))
		if got := people(t, w, w.ana, w.public.ID); len(got) != 0 {
			t.Fatalf("after room finished: %v", got)
		}

		// unknown rooms, foreign identities and other events are ignored
		_, before := w.rec.last()
		apply(event("participant_joined", "not-ours", w.ana.UserID, "PX"))
		apply(event("participant_joined", room, "agent-1", "PX"))
		apply(event("track_published", room, w.ana.UserID, "PX"))
		if got := people(t, w, w.ana, w.public.ID); len(got) != 0 {
			t.Fatalf("ignored events changed the room: %v", got)
		}
		if _, after := w.rec.last(); after != before {
			t.Fatalf("ignored events published %d events", after-before)
		}
	})
}

func TestListOnlyReadableRooms(t *testing.T) {
	dbtest.Each(t, func(t *testing.T, db *database.DB) {
		w := newWorld(t, db)
		pub := w.token(t, w.ana, w.public.ID).Video.Room
		priv := w.token(t, w.ana, w.private.ID).Video.Room
		for _, ev := range []*livekit.WebhookEvent{
			event("participant_joined", pub, w.ana.UserID, "P1"),
			event("participant_joined", pub, w.bob.UserID, "P2"),
			event("participant_joined", priv, w.ana.UserID, "P3"),
		} {
			if err := w.svc.HandleWebhook(ctx, ev); err != nil {
				t.Fatal(err)
			}
		}
		rooms, err := w.svc.List(ctx, w.bob)
		if err != nil || len(rooms) != 1 || rooms[0].ChannelID != w.public.ID || rooms[0].TeamID != w.team.ID || len(rooms[0].Participants) != 2 {
			t.Fatalf("bob's rooms: %+v %v", rooms, err)
		}
		if rooms, _ := w.svc.List(ctx, w.ana); len(rooms) != 2 {
			t.Fatalf("ana's rooms: %+v", rooms)
		}
		if rooms, _ := w.svc.List(ctx, w.cy); len(rooms) != 0 {
			t.Fatalf("cy's rooms: %+v", rooms)
		}
		if _, err := w.svc.Room(ctx, w.cy, w.private.ID); !errors.Is(err, chat.ErrNotFound) {
			t.Fatalf("room of a hidden channel: %v", err)
		}
		if got := people(t, w, w.ana, w.private.ID); len(got) != 1 {
			t.Fatalf("private room: %v", got)
		}
	})
}

func TestEviction(t *testing.T) {
	dbtest.Each(t, func(t *testing.T, db *database.DB) {
		w := newWorld(t, db)
		if err := w.chat.JoinChannel(ctx, w.cy, w.public.ID); err != nil {
			t.Fatal(err)
		}
		room := w.token(t, w.ana, w.public.ID).Video.Room
		join := func(s chat.Subject) {
			t.Helper()
			if err := w.svc.HandleWebhook(ctx, event("participant_joined", room, s.UserID, "S"+s.UserID[20:])); err != nil {
				t.Fatal(err)
			}
		}
		for _, s := range []chat.Subject{w.ana, w.bob, w.cy} {
			join(s)
		}
		if err := w.svc.Evict(ctx); err != nil || len(w.rooms.removed) != 0 {
			t.Fatalf("nobody lost access yet: %v %v", w.rooms.removed, err)
		}

		// removed from the channel, and deactivated
		if err := w.chat.RemoveChannelMember(ctx, w.ana, w.public.ID, w.bob.UserID); err != nil {
			t.Fatal(err)
		}
		if err := db.Gorm.Exec("UPDATE users SET status = 'disabled' WHERE id = ?", w.cy.UserID).Error; err != nil {
			t.Fatal(err)
		}
		// LiveKit unreachable: they stay listed, to be retried
		w.rooms.fail = true
		if err := w.svc.Evict(ctx); err == nil {
			t.Fatal("a failed removal must be reported")
		}
		if got := people(t, w, w.ana, w.public.ID); len(got) != 3 {
			t.Fatalf("listed before LiveKit removed them: %v", got)
		}
		w.rooms.fail = false
		if err := w.svc.Evict(ctx); err != nil {
			t.Fatal(err)
		}
		want := []string{room + "/" + w.bob.UserID, room + "/" + w.cy.UserID}
		if got := slices.Sorted(slices.Values(w.rooms.removed)); !slices.Equal(got, slices.Sorted(slices.Values(want))) {
			t.Fatalf("removed %v, want %v", got, want)
		}
		if got := people(t, w, w.ana, w.public.ID); !slices.Equal(got, []string{w.ana.UserID}) {
			t.Fatalf("after eviction: %v", got)
		}
		if ev, _ := w.rec.last(); ev.Name != "meeting_updated" || slices.Contains(ev.Recipients, w.bob.UserID) {
			t.Fatalf("eviction event %+v", ev)
		}

		// leaving the team, then the channel archived
		join(w.bob)
		if err := w.chat.AddChannelMember(ctx, w.ana, w.public.ID, w.bob.UserID); err != nil {
			t.Fatal(err)
		}
		if err := w.chat.RemoveTeamMember(ctx, w.owner, w.team.ID, w.bob.UserID); err != nil {
			t.Fatal(err)
		}
		if err := w.svc.Evict(ctx); err != nil {
			t.Fatal(err)
		}
		if got := people(t, w, w.ana, w.public.ID); !slices.Equal(got, []string{w.ana.UserID}) {
			t.Fatalf("after leaving the team: %v", got)
		}
		if err := w.chat.SetChannelArchived(ctx, w.ana, w.public.ID, true); err != nil {
			t.Fatal(err)
		}
		if err := w.svc.Evict(ctx); err != nil {
			t.Fatal(err)
		}
		if got := people(t, w, w.ana, w.public.ID); len(got) != 0 {
			t.Fatalf("archived channel still has people: %v", got)
		}
	})
}

func TestRefreshFromLiveKit(t *testing.T) {
	dbtest.Each(t, func(t *testing.T, db *database.DB) {
		w := newWorld(t, db)
		room := w.token(t, w.ana, w.public.ID).Video.Room
		if err := w.svc.HandleWebhook(ctx, event("participant_joined", room, w.ana.UserID, "PA")); err != nil {
			t.Fatal(err)
		}
		// the leave webhook was lost; LiveKit has Bob (whose join was lost) and a non-user
		w.rooms.live[room] = []meetings.LiveParticipant{{Identity: w.bob.UserID, SID: "PB", JoinedAt: 5}, {Identity: "agent", SID: "PX"}}
		_, before := w.rec.last()
		if err := w.svc.Refresh(ctx); err != nil {
			t.Fatal(err)
		}
		if got := people(t, w, w.ana, w.public.ID); !slices.Equal(got, []string{w.bob.UserID}) {
			t.Fatalf("after refresh: %v", got)
		}
		if _, after := w.rec.last(); after != before+1 {
			t.Fatalf("refresh announced %d events", after-before)
		}
		// nothing changed: no event
		if err := w.svc.Refresh(ctx); err != nil {
			t.Fatal(err)
		}
		if _, again := w.rec.last(); again != before+1 {
			t.Fatal("an unchanged refresh announced an event")
		}
		// the room ended while nobody told us
		delete(w.rooms.live, room)
		if err := w.svc.Refresh(ctx); err != nil {
			t.Fatal(err)
		}
		if got := people(t, w, w.ana, w.public.ID); len(got) != 0 {
			t.Fatalf("after the room ended: %v", got)
		}
	})
}

// TestEventDocumented: meeting_updated has its fixture and is in the event catalog of docs/api.md.
func TestEventDocumented(t *testing.T) {
	t.Parallel()
	doc, err := os.ReadFile("../../docs/api.md")
	if err != nil {
		t.Fatal(err)
	}
	b, err := os.ReadFile("testdata/meeting_updated.json")
	if err != nil {
		t.Fatal(err)
	}
	var v struct {
		Event string `json:"event"`
	}
	if json.Unmarshal(b, &v) != nil || v.Event != "meeting_updated" || !strings.Contains(string(doc), "| `meeting_updated` |") {
		t.Fatalf("fixture %q or catalog entry missing", v.Event)
	}
}
