package notify_test

import (
	"context"
	"errors"
	"fmt"
	"io"
	"mime"
	"mime/quotedprintable"
	"net/mail"
	"os"
	"regexp"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/jniltinho/meethub/internal/chat"
	"github.com/jniltinho/meethub/internal/database"
	"github.com/jniltinho/meethub/internal/database/dbtest"
	"github.com/jniltinho/meethub/internal/identity"
	"github.com/jniltinho/meethub/internal/notify"
	"github.com/jniltinho/meethub/internal/org"
	"github.com/jniltinho/meethub/internal/presence"
)

var ctx = context.Background()

// outbox records the e-mails; fail makes the next sends fail (a mail server down).
type outbox struct {
	mu   sync.Mutex
	sent map[string][]string
	fail bool
}

func (o *outbox) Send(_ context.Context, to string, msg []byte) error {
	o.mu.Lock()
	defer o.mu.Unlock()
	if o.fail {
		return errors.New("connection refused")
	}
	if o.sent == nil {
		o.sent = map[string][]string{}
	}
	o.sent[to] = append(o.sent[to], string(msg))
	return nil
}

func (o *outbox) to(addr string) []string {
	o.mu.Lock()
	defer o.mu.Unlock()
	return append([]string(nil), o.sent[addr]...)
}

// statuses is a fake presence: online people, or everything stale.
type statuses struct {
	online map[string]bool
	stale  bool
}

func (s *statuses) Get(_ context.Context, ids []string) ([]presence.Status, error) {
	out := make([]presence.Status, len(ids))
	for i, id := range ids {
		out[i] = presence.Status{UserID: id, Status: presence.Offline, Stale: s.stale}
		if s.online[id] {
			out[i].Status = presence.Online
		}
	}
	return out, nil
}

type world struct {
	db               *database.DB
	chat             *chat.Service
	n                *notify.Service
	out              *outbox
	st               *statuses
	clock            time.Time
	ana, bruno       chat.Subject
	anaMail          string
	projects, direct string
}

func newWorld(t *testing.T, db *database.DB) *world {
	t.Helper()
	if _, err := org.Create(ctx, db.Gorm, "Acme"); err != nil {
		t.Fatal(err)
	}
	users := &org.Users{DB: db, Hasher: identity.NewHasher(identity.Argon2Params{MemoryKiB: 1024, Time: 1, Threads: 1}, 8, time.Second)}
	mk := func(name string) chat.Subject {
		m, err := users.Create(ctx, org.SystemActor, org.NewUser{Email: name + "@acme.io", DisplayName: strings.ToUpper(name[:1]) + name[1:],
			Role: org.RoleMember, Password: "a long password!"})
		if err != nil {
			t.Fatal(err)
		}
		return chat.Subject{UserID: m.ID, OrgRole: org.RoleMember}
	}
	w := &world{db: db, out: &outbox{}, st: &statuses{online: map[string]bool{}}, clock: time.Now()}
	w.chat = &chat.Service{DB: db, EmailNotifications: true}
	w.ana, w.bruno, w.anaMail = mk("ana"), mk("bruno"), "ana@acme.io"
	owner := chat.Subject{UserID: w.bruno.UserID, OrgRole: org.RoleOwner}
	team, err := w.chat.CreateTeam(ctx, owner, chat.NewTeam{Name: "eng", DisplayName: "Eng"})
	if err != nil {
		t.Fatal(err)
	}
	if err := w.chat.AddTeamMember(ctx, owner, team.ID, w.ana.UserID); err != nil {
		t.Fatal(err)
	}
	ch, err := w.chat.CreateChannel(ctx, w.bruno, team.ID, chat.NewChannel{Name: "projects", DisplayName: "Projects", Type: chat.ChannelOpen})
	if err != nil {
		t.Fatal(err)
	}
	if err := w.chat.JoinChannel(ctx, w.ana, ch.ID); err != nil {
		t.Fatal(err)
	}
	dm, err := w.chat.OpenDirect(ctx, w.bruno, w.ana.UserID)
	if err != nil {
		t.Fatal(err)
	}
	w.projects, w.direct = ch.ID, dm.ID
	w.n = &notify.Service{DB: db.Gorm, Sender: w.out, From: "MeetHub <meethub@acme.io>", Presence: w.st,
		PublicURL: "https://chat.acme.io", Excerpt: true, Now: func() time.Time { return w.clock }}
	return w
}

var seq int

func (w *world) post(t *testing.T, sub chat.Subject, channel, text string) *chat.PostView {
	t.Helper()
	seq++
	p, err := w.chat.CreatePost(ctx, sub, chat.NewPost{ChannelID: channel, Message: text, PendingPostID: fmt.Sprintf("n-%d", seq)})
	if err != nil {
		t.Fatal(err)
	}
	return p
}

// run runs the task at the world's clock moved by d.
func (w *world) run(t *testing.T, d time.Duration) error {
	t.Helper()
	w.clock = w.clock.Add(d)
	return w.n.Run(ctx)
}

func (w *world) queued(t *testing.T) int64 {
	t.Helper()
	var n int64
	w.db.Gorm.Table("notification_queue").Count(&n)
	return n
}

// body is an e-mail's decoded subject and parts, for substring checks.
func body(t *testing.T, raw string) string {
	t.Helper()
	m, err := mail.ReadMessage(strings.NewReader(raw))
	if err != nil {
		t.Fatal(err)
	}
	subject, _ := new(mime.WordDecoder).DecodeHeader(m.Header.Get("Subject"))
	b, _ := io.ReadAll(quotedprintable.NewReader(m.Body))
	return subject + "\n" + string(b)
}

func TestEmailNotifications(t *testing.T) {
	dbtest.Each(t, func(t *testing.T, db *database.DB) {
		w := newWorld(t, db)

		// Away: one direct message, unread for 10 minutes, Ana offline → one e-mail
		p := w.post(t, w.bruno, w.direct, "Can you review the release plan?")
		if err := w.run(t, 9*time.Minute); err != nil || len(w.out.to(w.anaMail)) != 0 {
			t.Fatalf("e-mailed before 10 minutes: %v", err)
		}
		if err := w.run(t, 2*time.Minute); err != nil {
			t.Fatal(err)
		}
		got := w.out.to(w.anaMail)
		if len(got) != 1 {
			t.Fatalf("%d e-mails", len(got))
		}
		text := body(t, got[0])
		for _, want := range []string{"Bruno", "Can you review the release plan?", "https://chat.acme.io/pl/" + p.ID, "unsubscribe?token="} {
			if !strings.Contains(text, want) {
				t.Errorf("the e-mail lacks %q:\n%s", want, text)
			}
		}
		if !regexp.MustCompile(`List-Unsubscribe: <https://chat.acme.io/api/v1/notifications/unsubscribe\?token=[0-9a-f]{64}>\r\n`).MatchString(got[0]) ||
			!strings.Contains(got[0], "List-Unsubscribe-Post: List-Unsubscribe=One-Click") {
			t.Error("no one-click List-Unsubscribe header with one HTTPS URI")
		}
		if w.queued(t) != 0 {
			t.Fatal("the sent message stays queued")
		}

		// at most one e-mail every 15 minutes, gathering what happened since: twelve messages, one e-mail
		for i := range 12 {
			w.post(t, w.bruno, w.direct, fmt.Sprintf("message %d", i))
		}
		if err := w.run(t, 11*time.Minute); err != nil || len(w.out.to(w.anaMail)) != 1 {
			t.Fatalf("a second e-mail within 15 minutes: %v", err)
		}
		if err := w.run(t, 5*time.Minute); err != nil {
			t.Fatal(err)
		}
		got = w.out.to(w.anaMail)
		if len(got) != 2 || !strings.Contains(body(t, got[1]), "message 0") || !strings.Contains(body(t, got[1]), "message 11") ||
			!strings.Contains(body(t, got[1]), "12 new messages") {
			t.Fatalf("the twelve messages: %d e-mails", len(got))
		}

		// read in time: nothing is sent and nothing stays queued
		w.clock = w.clock.Add(time.Hour)
		w.post(t, w.bruno, w.direct, "read this soon")
		if err := w.chat.ViewChannel(ctx, w.ana, w.direct); err != nil {
			t.Fatal(err)
		}
		if err := w.run(t, 11*time.Minute); err != nil || len(w.out.to(w.anaMail)) != 2 || w.queued(t) != 0 {
			t.Fatalf("a message read in time: %v, queued %d", err, w.queued(t))
		}

		// a mention by name in a channel; @channel is not a direct mention
		w.clock = w.clock.Add(time.Hour)
		w.post(t, w.bruno, w.projects, "@channel lunch")
		if w.queued(t) != 0 {
			t.Fatal("@channel was queued")
		}
		w.post(t, w.bruno, w.projects, "@"+username(t, db, w.ana.UserID)+" the build is red")

		// online: nothing yet; presence unavailable: nothing at all
		w.st.online[w.ana.UserID] = true
		if err := w.run(t, 11*time.Minute); err != nil || len(w.out.to(w.anaMail)) != 2 || w.queued(t) != 1 {
			t.Fatalf("e-mailed while online: %v", err)
		}
		w.st.online, w.st.stale = map[string]bool{}, true
		if err := w.run(t, time.Minute); err != nil || len(w.out.to(w.anaMail)) != 2 || w.queued(t) != 1 {
			t.Fatalf("e-mailed with presence unavailable: %v", err)
		}
		w.st.stale = false

		// the mail server down: nothing lost, sent on a later run
		w.out.fail = true
		if err := w.run(t, time.Minute); err == nil || w.queued(t) != 1 {
			t.Fatalf("a failed send: %v, queued %d", err, w.queued(t))
		}
		w.out.fail = false
		if err := w.run(t, time.Minute); err != nil || len(w.out.to(w.anaMail)) != 3 || !strings.Contains(body(t, w.out.to(w.anaMail)[2]), "the build is red") {
			t.Fatalf("not sent after the server came back: %v", err)
		}

		// unsubscribe from the link: a wrong token is refused, the right one turns e-mails off
		token := regexp.MustCompile(`unsubscribe\?token=([0-9a-f]{64})`).FindStringSubmatch(w.out.to(w.anaMail)[2])[1]
		if err := w.n.Unsubscribe(ctx, strings.Repeat("0", 64)); !errors.Is(err, notify.ErrInvalidToken) {
			t.Fatalf("a wrong token: %v", err)
		}
		if err := w.n.Unsubscribe(ctx, token); err != nil {
			t.Fatal(err)
		}
		prefs, _ := identity.Store{}.Preferences(ctx, db.Gorm, w.ana.UserID)
		if prefs.Email != identity.NotifyNone {
			t.Fatalf("e-mail preference %q", prefs.Email)
		}
		w.clock = w.clock.Add(time.Hour)
		w.post(t, w.bruno, w.direct, "after unsubscribing")
		if err := w.run(t, 11*time.Minute); err != nil || len(w.out.to(w.anaMail)) != 3 || w.queued(t) != 0 {
			t.Fatalf("e-mailed after unsubscribing: %v, queued %d", err, w.queued(t))
		}
		// turned on again: the old link stops working
		// a token older than 90 days is refused even if no e-mail removed it
		other := w.out.to(w.anaMail)[1]
		old := regexp.MustCompile(`unsubscribe\?token=([0-9a-f]{64})`).FindStringSubmatch(other)[1]
		saved := w.clock
		w.clock = w.clock.Add(91 * 24 * time.Hour)
		if err := w.n.Unsubscribe(ctx, old); !errors.Is(err, notify.ErrInvalidToken) {
			t.Fatalf("a 91-day-old link: %v", err)
		}
		w.clock = saved
		if err := w.n.ResetUnsubscribe(ctx, w.ana.UserID); err != nil {
			t.Fatal(err)
		}
		if err := w.n.Unsubscribe(ctx, token); !errors.Is(err, notify.ErrInvalidToken) {
			t.Fatalf("an old link after turning e-mails on again: %v", err)
		}
	})
}

func TestNothingUnreadableIsSent(t *testing.T) {
	dbtest.Each(t, func(t *testing.T, db *database.DB) {
		w := newWorld(t, db)
		w.n.Excerpt = false
		w.post(t, w.bruno, w.projects, "@"+username(t, db, w.ana.UserID)+" the salaries are attached")
		w.post(t, w.bruno, w.direct, "a secret plan")
		// removed from #projects five minutes later
		w.clock = w.clock.Add(5 * time.Minute)
		if err := w.chat.RemoveChannelMember(ctx, w.ana, w.projects, w.ana.UserID); err != nil {
			t.Fatal(err)
		}
		if err := w.run(t, 6*time.Minute); err != nil {
			t.Fatal(err)
		}
		got := w.out.to(w.anaMail)
		if len(got) != 1 {
			t.Fatalf("%d e-mails", len(got))
		}
		text := body(t, got[0])
		if strings.Contains(text, "salaries") || strings.Contains(text, "#Projects") || strings.Contains(text, "secret plan") {
			t.Fatalf("an unreadable mention or an excerpt was sent:\n%s", text)
		}
		if !strings.Contains(text, "Bruno in a direct message") {
			t.Fatalf("who and where are missing:\n%s", text)
		}
	})
}

func TestPreferenceNoneQueuesNothing(t *testing.T) {
	dbtest.Each(t, func(t *testing.T, db *database.DB) {
		w := newWorld(t, db)
		none := identity.NotifyNone
		if err := (identity.Store{}).UpdatePreferences(ctx, db.Gorm, w.ana.UserID, identity.PreferencesPatch{Email: &none}); err != nil {
			t.Fatal(err)
		}
		w.post(t, w.bruno, w.direct, "hello")
		if w.queued(t) != 0 {
			t.Fatal("queued for someone who turned e-mails off")
		}
		if err := w.run(t, 11*time.Minute); err != nil || len(w.out.to(w.anaMail)) != 0 {
			t.Fatal("e-mailed someone who turned e-mails off")
		}
	})
}

func username(t *testing.T, db *database.DB, id string) string {
	t.Helper()
	var u []string
	db.Gorm.Table("users").Where("id = ?", id).Pluck("username", &u)
	return u[0]
}

// TestEmailStringsAreInTheFrontendCatalog: every e-mail text is listed in frontend/src/i18n/email.ts, so the SPA's
// catalog test requires its translations.
func TestEmailStringsAreInTheFrontendCatalog(t *testing.T) {
	t.Parallel()
	src, err := os.ReadFile("../../frontend/src/i18n/email.ts")
	if err != nil {
		t.Fatal(err)
	}
	for _, s := range notify.Strings {
		if !strings.Contains(string(src), "'"+strings.ReplaceAll(s, "'", "\\'")+"'") {
			t.Errorf("%q is not in frontend/src/i18n/email.ts", s)
		}
	}
}
