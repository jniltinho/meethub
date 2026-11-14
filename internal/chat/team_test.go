package chat_test

import (
	"context"
	"errors"
	"slices"
	"sync"
	"testing"
	"time"

	"github.com/jniltinho/meethub/internal/audit"
	"github.com/jniltinho/meethub/internal/chat"
	"github.com/jniltinho/meethub/internal/database"
	"github.com/jniltinho/meethub/internal/database/dbtest"
	"github.com/jniltinho/meethub/internal/identity"
	"github.com/jniltinho/meethub/internal/org"
)

var ctx = context.Background()

// recorder is a Publisher that keeps every event.
type recorder struct {
	mu     sync.Mutex
	events []chat.Event
}

func (r *recorder) Publish(_ context.Context, events ...chat.Event) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.events = append(r.events, events...)
}

// to returns the recipients of the last event with that name.
func (r *recorder) to(name string) []string {
	r.mu.Lock()
	defer r.mu.Unlock()
	for i := len(r.events) - 1; i >= 0; i-- {
		if r.events[i].Name == name {
			out := slices.Clone(r.events[i].Recipients)
			slices.Sort(out)
			return out
		}
	}
	return nil
}

type world struct {
	db                         *database.DB
	svc                        *chat.Service
	rec                        *recorder
	owner, admin, ana, bob, gu chat.Subject
}

func newWorld(t *testing.T, db *database.DB) *world {
	t.Helper()
	o, err := org.Create(ctx, db.Gorm, "Acme")
	if err != nil {
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
	_ = o
	rec := &recorder{}
	return &world{
		db: db, rec: rec, svc: &chat.Service{DB: db, Publisher: rec},
		owner: mk("owner", org.RoleOwner), admin: mk("admin", org.RoleAdmin),
		ana: mk("ana", org.RoleMember), bob: mk("bob", org.RoleMember), gu: mk("guest", org.RoleGuest),
	}
}

func TestTeamsCreateAndNames(t *testing.T) {
	dbtest.Each(t, func(t *testing.T, db *database.DB) {
		w := newWorld(t, db)
		if _, err := w.svc.CreateTeam(ctx, w.ana, chat.NewTeam{Name: "eng", DisplayName: "Eng"}); !errors.Is(err, chat.ErrForbidden) {
			t.Fatalf("member creating a team: %v", err)
		}
		team, err := w.svc.CreateTeam(ctx, w.admin, chat.NewTeam{Name: "engineering", DisplayName: "Engineering"})
		if err != nil {
			t.Fatal(err)
		}
		if got := w.rec.to("added_to_team"); !slices.Equal(got, []string{w.admin.UserID}) {
			t.Fatalf("added_to_team to %v", got)
		}
		if _, err := w.svc.CreateTeam(ctx, w.owner, chat.NewTeam{Name: "Engineering", DisplayName: "Other"}); !errors.Is(err, chat.ErrNameTaken) {
			t.Fatalf("duplicate name: %v", err)
		}
		mine, err := w.svc.MyTeams(ctx, w.admin)
		if err != nil || len(mine) != 1 || mine[0].ID != team.ID || mine[0].Role != chat.TeamAdmin {
			t.Fatalf("my teams: %+v %v", mine, err)
		}
		ev, _ := audit.List(ctx, db.Gorm, audit.Filter{Action: audit.ActionTeamCreated})
		if len(ev.Events) != 1 {
			t.Fatal("team creation not audited")
		}
		// without a name, it comes from the display name
		if derived, err := w.svc.CreateTeam(ctx, w.owner, chat.NewTeam{DisplayName: "Customer_Success Team"}); err != nil || derived.Name != "customer-success-team" {
			t.Fatalf("derived name: %+v %v", derived, err)
		}
	})
}

func TestTeamsJoinRules(t *testing.T) {
	dbtest.Each(t, func(t *testing.T, db *database.DB) {
		w := newWorld(t, db)
		open, _ := w.svc.CreateTeam(ctx, w.owner, chat.NewTeam{Name: "open-team", DisplayName: "Open"})
		invite, _ := w.svc.CreateTeam(ctx, w.owner, chat.NewTeam{Name: "invite-team", DisplayName: "Invite", Type: chat.TeamInvite})

		joinable, _, err := w.svc.JoinableTeams(ctx, w.ana, "", 0)
		if err != nil || len(joinable) != 1 || joinable[0].ID != open.ID {
			t.Fatalf("joinable: %+v %v", joinable, err)
		}
		if g, _, _ := w.svc.JoinableTeams(ctx, w.gu, "", 0); len(g) != 0 {
			t.Fatal("guests see joinable teams")
		}
		if err := w.svc.JoinTeam(ctx, w.ana, open.ID); err != nil {
			t.Fatal(err)
		}
		mine, _ := w.svc.MyTeams(ctx, w.ana)
		if len(mine) != 1 || mine[0].Role != chat.TeamUser {
			t.Fatalf("after join: %+v", mine)
		}
		var inGeneral int64
		db.Gorm.Table("channel_members m").Joins("JOIN channels c ON c.id = m.channel_id").
			Where("c.team_id = ? AND c.is_default = ? AND m.user_id = ?", open.ID, true, w.ana.UserID).Count(&inGeneral)
		if inGeneral != 1 {
			t.Fatal("joining the team did not join its default channel")
		}
		if err := w.svc.JoinTeam(ctx, w.bob, invite.ID); !errors.Is(err, chat.ErrForbidden) {
			t.Fatalf("invite-only: %v", err)
		}
		if err := w.svc.JoinTeam(ctx, w.gu, open.ID); !errors.Is(err, chat.ErrForbidden) {
			t.Fatalf("guest joining: %v", err)
		}
		if err := w.svc.AddTeamMember(ctx, w.owner, invite.ID, w.gu.UserID); err != nil {
			t.Fatalf("admin adds a guest: %v", err)
		}
	})
}

func TestLeavingATeamRemovesItsChannels(t *testing.T) {
	dbtest.Each(t, func(t *testing.T, db *database.DB) {
		w := newWorld(t, db)
		ops, _ := w.svc.CreateTeam(ctx, w.owner, chat.NewTeam{Name: "ops", DisplayName: "Ops"})
		if err := w.svc.AddTeamMember(ctx, w.owner, ops.ID, w.bob.UserID); err != nil {
			t.Fatal(err)
		}
		// a private channel #finance with Bob (channels service comes in task 2.4)
		finance := "01JFINANCE0000000000000000"
		db.Gorm.Exec(`INSERT INTO channels (id, team_id, type, name, display_name, created_at, updated_at) VALUES (?, ?, 'P', 'finance', 'Finance', 1, 1)`, finance, ops.ID)
		for _, u := range []string{w.owner.UserID, w.bob.UserID} {
			db.Gorm.Exec(`INSERT INTO channel_members (channel_id, user_id, role, created_at) VALUES (?, ?, 'channel_user', 1)`, finance, u)
		}
		if err := w.svc.RemoveTeamMember(ctx, w.owner, ops.ID, w.bob.UserID); err != nil {
			t.Fatal(err)
		}
		var left int64
		db.Gorm.Table("channel_members m").Joins("JOIN channels c ON c.id = m.channel_id").
			Where("c.team_id = ? AND m.user_id = ?", ops.ID, w.bob.UserID).Count(&left)
		if left != 0 {
			t.Fatalf("Bob kept %d channel memberships of the team", left)
		}
		if got := w.rec.to("leave_team"); !slices.Equal(got, []string{w.bob.UserID}) {
			t.Fatalf("leave_team to %v", got)
		}
		// user_removed of the last channel processed: remaining members plus Bob himself
		if got := w.rec.to("user_removed"); !slices.Contains(got, w.bob.UserID) || !slices.Contains(got, w.owner.UserID) {
			t.Fatalf("user_removed to %v", got)
		}
	})
}

func TestTeamAdministration(t *testing.T) {
	dbtest.Each(t, func(t *testing.T, db *database.DB) {
		w := newWorld(t, db)
		team, _ := w.svc.CreateTeam(ctx, w.admin, chat.NewTeam{Name: "eng", DisplayName: "Eng"})
		_ = w.svc.AddTeamMember(ctx, w.admin, team.ID, w.ana.UserID)
		if err := w.svc.SetTeamRole(ctx, w.ana, team.ID, w.ana.UserID, chat.TeamAdmin); !errors.Is(err, chat.ErrForbidden) {
			t.Fatalf("self-promotion: %v", err)
		}
		if err := w.svc.SetTeamRole(ctx, w.admin, team.ID, w.admin.UserID, chat.TeamUser); !errors.Is(err, chat.ErrForbidden) {
			t.Fatalf("changing one's own role: %v", err)
		}
		if err := w.svc.SetTeamRole(ctx, w.admin, team.ID, w.ana.UserID, chat.TeamAdmin); err != nil {
			t.Fatal(err)
		}
		// recipients: the team admins (now including Ana) and Ana herself
		if got := w.rec.to("user_role_updated"); !slices.Contains(got, w.admin.UserID) || !slices.Contains(got, w.ana.UserID) {
			t.Fatalf("user_role_updated to %v", got)
		}
		name := "Engineering"
		if _, err := w.svc.UpdateTeam(ctx, w.bob, team.ID, chat.TeamPatch{DisplayName: &name}); !errors.Is(err, chat.ErrNotFound) {
			t.Fatalf("non-member updating: %v", err)
		}
		if _, err := w.svc.UpdateTeam(ctx, w.ana, team.ID, chat.TeamPatch{DisplayName: &name}); err != nil {
			t.Fatalf("new team admin updating: %v", err)
		}
		if err := w.svc.SetTeamArchived(ctx, w.ana, team.ID, true); err != nil {
			t.Fatal(err)
		}
		if mine, _ := w.svc.MyTeams(ctx, w.ana); len(mine) != 0 {
			t.Fatal("archived team still in the switcher")
		}
		if got := w.rec.to("delete_team"); !slices.Contains(got, w.ana.UserID) {
			t.Fatalf("delete_team to %v", got)
		}
		db.Gorm.Exec(`UPDATE teams SET is_default = ?, archived_at = 0 WHERE id = ?`, true, team.ID)
		if err := w.svc.SetTeamArchived(ctx, w.owner, team.ID, true); !errors.Is(err, chat.ErrDefaultTeam) {
			t.Fatalf("archiving the default team: %v", err)
		}
	})
}

func TestMyTeamsUnreadTotals(t *testing.T) {
	dbtest.Each(t, func(t *testing.T, db *database.DB) {
		w := newWorld(t, db)
		team, _ := w.svc.CreateTeam(ctx, w.owner, chat.NewTeam{Name: "eng", DisplayName: "Eng"})
		_ = w.svc.AddTeamMember(ctx, w.owner, team.ID, w.ana.UserID)
		// two unread messages in general, one mentioning Ana (posting arrives in task 4.1)
		db.Gorm.Exec(`UPDATE channels SET total_msg_count = total_msg_count + 2 WHERE team_id = ?`, team.ID)
		db.Gorm.Exec(`UPDATE channel_members SET mention_count = 1 WHERE user_id = ?`, w.ana.UserID)
		mine, err := w.svc.MyTeams(ctx, w.ana)
		if err != nil || len(mine) != 1 || !mine[0].HasUnread || mine[0].MentionCount != 1 {
			t.Fatalf("totals: %+v %v", mine, err)
		}
	})
}
