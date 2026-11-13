package chat_test

import (
	"errors"
	"slices"
	"strings"
	"testing"

	"github.com/jniltinho/meethub/internal/audit"
	"github.com/jniltinho/meethub/internal/chat"
	"github.com/jniltinho/meethub/internal/database"
	"github.com/jniltinho/meethub/internal/database/dbtest"
)

// teamWorld adds a team "eng" with ana and bob as team users and the guest added.
func teamWorld(t *testing.T, db *database.DB) (*world, *chat.Team) {
	t.Helper()
	w := newWorld(t, db)
	team, err := w.svc.CreateTeam(ctx, w.owner, chat.NewTeam{Name: "eng", DisplayName: "Eng"})
	if err != nil {
		t.Fatal(err)
	}
	for _, s := range []chat.Subject{w.ana, w.bob, w.gu} {
		if err := w.svc.AddTeamMember(ctx, w.owner, team.ID, s.UserID); err != nil {
			t.Fatal(err)
		}
	}
	return w, team
}

func TestChannelIdentityAndCreation(t *testing.T) {
	dbtest.Each(t, func(t *testing.T, db *database.DB) {
		w, team := teamWorld(t, db)
		ch, err := w.svc.CreateChannel(ctx, w.ana, team.ID, chat.NewChannel{DisplayName: "Dev Ops", Type: chat.ChannelOpen})
		if err != nil || ch.Name != "dev-ops" {
			t.Fatalf("create: %+v %v", ch, err)
		}
		// public channel creation reaches the team (not the guest)
		if got := w.rec.to("channel_created"); slices.Contains(got, w.gu.UserID) || !slices.Contains(got, w.bob.UserID) {
			t.Fatalf("channel_created to %v", got)
		}
		if _, err := w.svc.CreateChannel(ctx, w.bob, team.ID, chat.NewChannel{Name: "dev-ops", DisplayName: "Again", Type: chat.ChannelOpen}); !errors.Is(err, chat.ErrNameTaken) {
			t.Fatalf("duplicate name in a team: %v", err)
		}
		other, _ := w.svc.CreateTeam(ctx, w.owner, chat.NewTeam{Name: "ops", DisplayName: "Ops"})
		if _, err := w.svc.CreateChannel(ctx, w.owner, other.ID, chat.NewChannel{Name: "dev-ops", DisplayName: "Dev Ops", Type: chat.ChannelOpen}); err != nil {
			t.Fatalf("same name in another team: %v", err)
		}
		if _, err := w.svc.CreateChannel(ctx, w.gu, team.ID, chat.NewChannel{DisplayName: "Mine", Type: chat.ChannelOpen}); !errors.Is(err, chat.ErrForbidden) {
			t.Fatalf("guest creating: %v", err)
		}
		long := strings.Repeat("p", 251)
		if _, err := w.svc.CreateChannel(ctx, w.ana, team.ID, chat.NewChannel{DisplayName: "X", Purpose: long, Type: chat.ChannelOpen}); !errors.Is(err, chat.ErrTooLong) {
			t.Fatalf("purpose over 250: %v", err)
		}
	})
}

func TestChannelAccessRules(t *testing.T) {
	dbtest.Each(t, func(t *testing.T, db *database.DB) {
		w, team := teamWorld(t, db)
		pub, _ := w.svc.CreateChannel(ctx, w.ana, team.ID, chat.NewChannel{Name: "devops", DisplayName: "DevOps", Type: chat.ChannelOpen})
		priv, _ := w.svc.CreateChannel(ctx, w.ana, team.ID, chat.NewChannel{Name: "finance", DisplayName: "Finance", Type: chat.ChannelPrivate})

		if _, err := w.svc.GetChannel(ctx, w.bob, priv.ID); !errors.Is(err, chat.ErrNotFound) {
			t.Fatalf("private, not a member: %v", err)
		}
		if _, _, err := w.svc.Members(ctx, w.bob, pub.ID, "", 0); !errors.Is(err, chat.ErrForbidden) {
			t.Fatalf("public, not joined, reading members: %v", err)
		}
		if err := w.svc.JoinChannel(ctx, w.bob, pub.ID); err != nil {
			t.Fatal(err)
		}
		if got := w.rec.to("user_added"); !slices.Contains(got, w.ana.UserID) {
			t.Fatalf("user_added to %v", got)
		}
		members, _, err := w.svc.Members(ctx, w.bob, pub.ID, "", 0)
		if err != nil || len(members) != 2 || members[0].Username == "" {
			t.Fatalf("members: %+v %v", members, err)
		}
		if err := w.svc.JoinChannel(ctx, w.gu, pub.ID); err == nil {
			t.Fatal("guest joined a public channel")
		}
		if list, _, _ := w.svc.JoinableChannels(ctx, w.gu, team.ID, "", 0); len(list) != 0 {
			t.Fatal("guests browse public channels")
		}
		if list, _, _ := w.svc.JoinableChannels(ctx, w.owner, team.ID, "", 0); len(list) != 1 || list[0].ID != pub.ID || list[0].MemberCount != 2 {
			t.Fatalf("joinable for owner: %+v", list)
		}
		if err := w.svc.JoinChannel(ctx, w.bob, priv.ID); !errors.Is(err, chat.ErrNotFound) {
			t.Fatalf("joining a private channel: %v", err)
		}
	})
}

func TestChannelMembershipRules(t *testing.T) {
	dbtest.Each(t, func(t *testing.T, db *database.DB) {
		w, team := teamWorld(t, db)
		priv, _ := w.svc.CreateChannel(ctx, w.ana, team.ID, chat.NewChannel{Name: "finance", DisplayName: "Finance", Type: chat.ChannelPrivate})
		if err := w.svc.AddChannelMember(ctx, w.ana, priv.ID, w.admin.UserID); !errors.Is(err, chat.ErrNotTeamMember) {
			t.Fatalf("adding someone outside the team: %v", err)
		}
		if err := w.svc.AddChannelMember(ctx, w.ana, priv.ID, w.bob.UserID); err != nil {
			t.Fatal(err)
		}
		if err := w.svc.SetChannelRole(ctx, w.bob, priv.ID, w.bob.UserID, chat.ChannelAdmin); !errors.Is(err, chat.ErrForbidden) {
			t.Fatalf("self-promotion: %v", err)
		}
		if err := w.svc.RemoveChannelMember(ctx, w.ana, priv.ID, w.bob.UserID); err != nil {
			t.Fatal(err)
		}
		if got := w.rec.to("user_removed"); !slices.Contains(got, w.bob.UserID) {
			t.Fatalf("the removed user must be told: %v", got)
		}
		general := defaultChannel(t, w, team.ID)
		if err := w.svc.RemoveChannelMember(ctx, w.bob, general, w.bob.UserID); !errors.Is(err, chat.ErrDefaultChannel) {
			t.Fatalf("leaving general: %v", err)
		}
		ev, _ := audit.List(ctx, db.Gorm, audit.Filter{Action: audit.ActionChannelMemberRem})
		if len(ev.Events) != 1 {
			t.Fatal("private member removal not audited")
		}
	})
}

func defaultChannel(t *testing.T, w *world, teamID string) string {
	t.Helper()
	list, _, err := w.svc.MyChannels(ctx, w.owner, teamID, "", 0)
	if err != nil {
		t.Fatal(err)
	}
	for _, c := range list {
		if c.IsDefault {
			return c.ID
		}
	}
	t.Fatal("no default channel")
	return ""
}

func TestChannelEditArchive(t *testing.T) {
	dbtest.Each(t, func(t *testing.T, db *database.DB) {
		w, team := teamWorld(t, db)
		ch, _ := w.svc.CreateChannel(ctx, w.ana, team.ID, chat.NewChannel{Name: "devops", DisplayName: "DevOps", Type: chat.ChannelOpen})
		_ = w.svc.JoinChannel(ctx, w.bob, ch.ID)
		header := "Deploys on Fridays: no"
		if _, err := w.svc.UpdateChannel(ctx, w.bob, ch.ID, chat.ChannelPatch{Header: &header}); !errors.Is(err, chat.ErrForbidden) {
			t.Fatalf("member editing: %v", err)
		}
		name := "ops"
		up, err := w.svc.UpdateChannel(ctx, w.ana, ch.ID, chat.ChannelPatch{Header: &header, Name: &name})
		if err != nil || up.Header != header || up.Name != "ops" {
			t.Fatalf("update: %+v %v", up, err)
		}
		if got := w.rec.to("channel_updated"); !slices.Contains(got, w.bob.UserID) {
			t.Fatalf("channel_updated to %v", got)
		}
		if err := w.svc.SetChannelArchived(ctx, w.owner, ch.ID, true); err != nil { // team admin
			t.Fatal(err)
		}
		if err := w.svc.JoinChannel(ctx, w.gu, ch.ID); err == nil {
			t.Fatal("joined an archived channel")
		}
		if _, err := w.svc.UpdateChannel(ctx, w.ana, ch.ID, chat.ChannelPatch{Header: &header}); !errors.Is(err, chat.ErrArchived) {
			t.Fatalf("editing an archived channel: %v", err)
		}
		if err := w.svc.SetChannelArchived(ctx, w.ana, ch.ID, false); err != nil {
			t.Fatalf("restore: %v", err)
		}
		if got := w.rec.to("channel_restored"); !slices.Contains(got, w.bob.UserID) {
			t.Fatalf("channel_restored to %v", got)
		}
		if err := w.svc.SetChannelArchived(ctx, w.owner, defaultChannel(t, w, team.ID), true); !errors.Is(err, chat.ErrDefaultChannel) {
			t.Fatalf("archiving general: %v", err)
		}
	})
}

// TestAuditFailureRollsBackChannelArchive: a privileged change fails when its
// audit event cannot be written, and nothing changes.
func TestAuditFailureRollsBackChannelArchive(t *testing.T) {
	dbtest.Each(t, func(t *testing.T, db *database.DB) {
		w, team := teamWorld(t, db)
		ch, _ := w.svc.CreateChannel(ctx, w.ana, team.ID, chat.NewChannel{Name: "devops", DisplayName: "DevOps", Type: chat.ChannelOpen})
		if err := db.Gorm.Exec(`DROP TABLE audit_events`).Error; err != nil {
			t.Fatal(err)
		}
		if err := w.svc.SetChannelArchived(ctx, w.ana, ch.ID, true); err == nil {
			t.Fatal("archive succeeded without its audit event")
		}
		got, err := w.svc.GetChannel(ctx, w.ana, ch.ID)
		if err != nil || got.ArchivedAt != 0 {
			t.Fatalf("archive not rolled back: %+v %v", got, err)
		}
	})
}

func TestMyChannelsPaging(t *testing.T) {
	dbtest.Each(t, func(t *testing.T, db *database.DB) {
		w, team := teamWorld(t, db)
		for _, n := range []string{"alpha", "beta", "gamma"} {
			if _, err := w.svc.CreateChannel(ctx, w.ana, team.ID, chat.NewChannel{Name: n, DisplayName: "Same", Type: chat.ChannelPrivate}); err != nil {
				t.Fatal(err)
			}
		}
		seen, cursor := map[string]bool{}, ""
		for range 10 {
			page, next, err := w.svc.MyChannels(ctx, w.ana, team.ID, cursor, 2)
			if err != nil {
				t.Fatal(err)
			}
			for _, c := range page {
				if seen[c.ID] {
					t.Fatalf("channel %s twice", c.ID)
				}
				seen[c.ID] = true
			}
			if cursor = next; cursor == "" {
				break
			}
		}
		if len(seen) != 4 { // general + 3
			t.Fatalf("listed %d channels, want 4", len(seen))
		}
		if _, _, err := w.svc.MyChannels(ctx, w.admin, team.ID, "", 0); !errors.Is(err, chat.ErrNotFound) {
			t.Fatalf("outside the team: %v", err)
		}
	})
}
