package chat_test

import (
	"errors"
	"sync"
	"testing"

	"github.com/jniltinho/meethub/internal/chat"
	"github.com/jniltinho/meethub/internal/database"
	"github.com/jniltinho/meethub/internal/database/dbtest"
	"github.com/jniltinho/meethub/internal/identity"
	"github.com/jniltinho/meethub/internal/org"
)

func TestConcurrentDirectAndGroupCreation(t *testing.T) {
	dbtest.Each(t, func(t *testing.T, db *database.DB) {
		w := newWorld(t, db)
		var wg sync.WaitGroup
		got := make([]*chat.Channel, 4)
		errs := make([]error, 4)
		wg.Go(func() { got[0], errs[0] = w.svc.OpenDirect(ctx, w.ana, w.bob.UserID) })
		wg.Go(func() { got[1], errs[1] = w.svc.OpenDirect(ctx, w.bob, w.ana.UserID) })
		wg.Go(func() { got[2], errs[2] = w.svc.OpenGroup(ctx, w.ana, []string{w.bob.UserID, w.owner.UserID}) })
		wg.Go(func() { got[3], errs[3] = w.svc.OpenGroup(ctx, w.owner, []string{w.ana.UserID, w.bob.UserID}) })
		wg.Wait()
		for i, err := range errs {
			if err != nil {
				t.Fatalf("open %d: %v", i, err)
			}
		}
		if got[0].ID != got[1].ID || got[2].ID != got[3].ID {
			t.Fatalf("two conversations for one pair or group: %s %s / %s %s", got[0].ID, got[1].ID, got[2].ID, got[3].ID)
		}
		for _, c := range []struct {
			id   string
			want int64
		}{{got[0].ID, 2}, {got[2].ID, 3}} {
			var n int64
			db.Gorm.Table("channel_members").Where("channel_id = ?", c.id).Count(&n)
			if n != c.want {
				t.Fatalf("conversation %s has %d members, want %d", c.id, n, c.want)
			}
		}
		self1, err1 := w.svc.OpenDirect(ctx, w.ana, w.ana.UserID)
		self2, err2 := w.svc.OpenDirect(ctx, w.ana, w.ana.UserID)
		if err1 != nil || err2 != nil || self1.ID != self2.ID {
			t.Fatalf("self conversation: %v %v", err1, err2)
		}
	})
}

func TestConversationRules(t *testing.T) {
	dbtest.Each(t, func(t *testing.T, db *database.DB) {
		w := newWorld(t, db)
		nine := make([]string, 0, 8)
		users := &org.Users{DB: db, Hasher: identity.NewHasher(identity.Argon2Params{MemoryKiB: 1024, Time: 1, Threads: 1}, 4, 0)}
		for i := range 8 {
			m, err := users.Create(ctx, org.SystemActor, org.NewUser{Email: string(rune('a'+i)) + "x@acme.io", DisplayName: "X", Role: org.RoleMember, Password: "a long password!"})
			if err != nil {
				t.Fatal(err)
			}
			nine = append(nine, m.ID)
		}
		if _, err := w.svc.OpenGroup(ctx, w.ana, nine); !errors.Is(err, chat.ErrInvalidGroup) {
			t.Fatalf("nine people: %v", err)
		}
		if _, err := w.svc.OpenGroup(ctx, w.ana, []string{w.bob.UserID, w.bob.UserID}); !errors.Is(err, chat.ErrInvalidGroup) {
			t.Fatalf("two distinct people: %v", err)
		}
		// a member and a guest stranger
		if _, err := w.svc.OpenDirect(ctx, w.ana, w.gu.UserID); !errors.Is(err, chat.ErrForbidden) {
			t.Fatalf("guest stranger: %v", err)
		}
		// once they share a team channel, it works
		team, _ := w.svc.CreateTeam(ctx, w.owner, chat.NewTeam{Name: "eng", DisplayName: "Eng"})
		_ = w.svc.AddTeamMember(ctx, w.owner, team.ID, w.ana.UserID)
		_ = w.svc.AddTeamMember(ctx, w.owner, team.ID, w.gu.UserID)
		dm, err := w.svc.OpenDirect(ctx, w.gu, w.ana.UserID)
		if err != nil {
			t.Fatalf("guest sharing general: %v", err)
		}
		// disabled user
		if _, err := users.Update(ctx, org.SystemActor, w.bob.UserID, org.Patch{Status: ptr(identity.StatusDisabled)}); err != nil {
			t.Fatal(err)
		}
		if _, err := w.svc.OpenDirect(ctx, w.ana, w.bob.UserID); !errors.Is(err, chat.ErrUserInactive) {
			t.Fatalf("disabled user: %v", err)
		}
		// fixed membership and privacy
		if err := w.svc.AddChannelMember(ctx, w.ana, dm.ID, w.owner.UserID); !errors.Is(err, chat.ErrDirectChannel) {
			t.Fatalf("adding to a DM: %v", err)
		}
		if err := w.svc.RemoveChannelMember(ctx, w.ana, dm.ID, w.gu.UserID); !errors.Is(err, chat.ErrDirectChannel) {
			t.Fatalf("removing from a DM: %v", err)
		}
		if _, err := w.svc.GetChannel(ctx, w.admin, dm.ID); !errors.Is(err, chat.ErrNotFound) {
			t.Fatalf("outsider reading a DM: %v", err)
		}
	})
}

func ptr(s string) *string { return &s }

func TestConversationListAndVisibility(t *testing.T) {
	dbtest.Each(t, func(t *testing.T, db *database.DB) {
		w := newWorld(t, db)
		dm, err := w.svc.OpenDirect(ctx, w.ana, w.bob.UserID)
		if err != nil {
			t.Fatal(err)
		}
		list, _, err := w.svc.MyConversations(ctx, w.ana, "", 0)
		if err != nil || len(list) != 1 || list[0].ID != dm.ID || len(list[0].Members) != 2 || list[0].Members[0].Username == "" {
			t.Fatalf("opener's list: %+v %v", list, err)
		}
		if other, _, _ := w.svc.MyConversations(ctx, w.bob, "", 0); len(other) != 0 {
			t.Fatal("an empty conversation appears for the other member")
		}
		if err := w.svc.SetConversationVisible(ctx, w.ana, dm.ID, false); err != nil {
			t.Fatal(err)
		}
		if hidden, _, _ := w.svc.MyConversations(ctx, w.ana, "", 0); len(hidden) != 0 {
			t.Fatal("hidden conversation still listed")
		}
		if err := w.svc.SetConversationVisible(ctx, w.admin, dm.ID, true); !errors.Is(err, chat.ErrNotFound) {
			t.Fatalf("outsider: %v", err)
		}
	})
}
