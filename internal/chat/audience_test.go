package chat_test

import (
	"slices"
	"testing"

	"github.com/jniltinho/meethub/internal/chat"
	"github.com/jniltinho/meethub/internal/database"
	"github.com/jniltinho/meethub/internal/database/dbtest"
)

func sorted(ids []string) []string {
	out := slices.Clone(ids)
	slices.Sort(out)
	return out
}

func TestStatusVisibility(t *testing.T) {
	dbtest.Each(t, func(t *testing.T, db *database.DB) {
		w, team := teamWorld(t, db) // eng: owner, ana, bob, guest (in #general); admin outside
		got, err := w.svc.VisibleStatuses(ctx, w.ana.UserID, []string{w.bob.UserID, w.admin.UserID, w.gu.UserID, w.ana.UserID, "01UNKNOWN0000000000000000X"})
		if err != nil {
			t.Fatal(err)
		}
		if want := sorted([]string{w.ana.UserID, w.bob.UserID, w.gu.UserID}); !slices.Equal(sorted(got), want) {
			t.Fatalf("visible to ana: %v, want %v", got, want)
		}

		// the guest shares the team but no channel: invisible both ways
		db.Gorm.Exec(`DELETE FROM channel_members WHERE user_id = ?`, w.gu.UserID)
		if got, _ := w.svc.VisibleStatuses(ctx, w.gu.UserID, []string{w.ana.UserID}); len(got) != 0 {
			t.Fatalf("guest sees %v through the team alone", got)
		}
		if got, _ := w.svc.VisibleStatuses(ctx, w.ana.UserID, []string{w.gu.UserID}); len(got) != 0 {
			t.Fatalf("ana sees the guest through the team alone: %v", got)
		}
		priv, err := w.svc.CreateChannel(ctx, w.ana, team.ID, chat.NewChannel{Name: "project", DisplayName: "Project", Type: chat.ChannelPrivate})
		if err != nil {
			t.Fatal(err)
		}
		if err := w.svc.AddChannelMember(ctx, w.ana, priv.ID, w.gu.UserID); err != nil {
			t.Fatal(err)
		}
		if got, _ := w.svc.VisibleStatuses(ctx, w.gu.UserID, []string{w.ana.UserID, w.bob.UserID}); !slices.Equal(got, []string{w.ana.UserID}) {
			t.Fatalf("guest with a shared channel sees %v", got)
		}

		watchers, err := w.svc.StatusWatchers(ctx, []string{w.ana.UserID, w.admin.UserID})
		if err != nil {
			t.Fatal(err)
		}
		if a := watchers[w.ana.UserID]; !slices.Contains(a, w.bob.UserID) || !slices.Contains(a, w.gu.UserID) || slices.Contains(a, w.admin.UserID) || !slices.Contains(a, w.ana.UserID) {
			t.Fatalf("ana's watchers: %v", a)
		}
		if a := watchers[w.admin.UserID]; !slices.Equal(a, []string{w.admin.UserID}) {
			t.Fatalf("admin's watchers: %v", a)
		}
	})
}
