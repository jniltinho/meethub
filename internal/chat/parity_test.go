package chat_test

import (
	"errors"
	"fmt"
	"slices"
	"testing"
	"time"

	"github.com/jniltinho/meethub/internal/chat"
	"github.com/jniltinho/meethub/internal/database"
	"github.com/jniltinho/meethub/internal/database/dbtest"
)

// TestFavoriteChannels: a personal flag carried by the lists and announced
// only to its owner (channels spec).
func TestFavoriteChannels(t *testing.T) {
	dbtest.Each(t, func(t *testing.T, db *database.DB) {
		w, ch := postWorld(t, db)
		favorite := func(sub chat.Subject) bool {
			t.Helper()
			list, _, err := w.svc.MyChannels(ctx, sub, ch.TeamID, "", 0)
			if err != nil {
				t.Fatal(err)
			}
			i := slices.IndexFunc(list, func(c chat.ChannelWithState) bool { return c.ID == ch.ID })
			return list[i].Favorite
		}
		if favorite(w.ana) {
			t.Fatal("favorite before marking")
		}
		if err := w.svc.SetFavorite(ctx, w.ana, ch.ID, true); err != nil {
			t.Fatal(err)
		}
		if err := w.svc.SetFavorite(ctx, w.ana, ch.ID, true); err != nil { // unchanged value is not "not found"
			t.Fatal(err)
		}
		if !favorite(w.ana) || favorite(w.bob) {
			t.Fatal("the favorite is not personal")
		}
		if got := w.rec.to("preferences_changed"); !slices.Equal(got, []string{w.ana.UserID}) {
			t.Fatalf("preferences_changed to %v", got)
		}
		if err := w.svc.SetFavorite(ctx, w.ana, ch.ID, false); err != nil || favorite(w.ana) {
			t.Fatalf("unmark: %v", err)
		}
		// a conversation can be a favorite too
		dm, err := w.svc.OpenDirect(ctx, w.ana, w.bob.UserID)
		if err != nil {
			t.Fatal(err)
		}
		_ = post(t, w, w.ana, dm.ID, "hi")
		if err := w.svc.SetFavorite(ctx, w.bob, dm.ID, true); err != nil {
			t.Fatal(err)
		}
		convs, _, _ := w.svc.MyConversations(ctx, w.bob, "", 0)
		if len(convs) != 1 || !convs[0].Favorite {
			t.Fatalf("conversation favorite: %+v", convs)
		}
		// not a member: the channel does not exist for the guest
		if err := w.svc.SetFavorite(ctx, w.gu, dm.ID, true); !errors.Is(err, chat.ErrNotFound) {
			t.Fatalf("favorite by a non-member: %v", err)
		}
	})
}

// TestHistoryAround: a window around a message and forward paging to the
// newest one, with equal timestamps at the window's edges (messages spec).
func TestHistoryAround(t *testing.T) {
	dbtest.Each(t, func(t *testing.T, db *database.DB) {
		w, ch := postWorld(t, db)
		// pairs of messages share a millisecond: the cursor must order by (create_at, id)
		n, tick := 0, time.UnixMilli(1_800_000_000_000)
		w.svc.Now = func() time.Time { return tick.Add(time.Duration(n/2) * time.Millisecond) }
		for ; n < 41; n++ {
			_ = post(t, w, w.ana, ch.ID, fmt.Sprintf("m%02d", n))
		}
		// creation order: same-millisecond pairs are ordered by id
		hist, _, _ := w.svc.History(ctx, w.bob, ch.ID, "", 200)
		order := make([]string, len(hist))
		for i, p := range hist {
			order[len(hist)-1-i] = p.ID
		}
		center := order[20]
		win, err := w.svc.HistoryAround(ctx, w.bob, ch.ID, center, 10)
		if err != nil || len(win.Posts) != 11 || win.Posts[5].ID != center || win.PrevCursor == "" || win.NextCursor == "" {
			t.Fatalf("around: %d posts, %+v %v", len(win.Posts), win, err)
		}
		for i, p := range win.Posts { // newest first: order[25] … order[15]
			if p.ID != order[25-i] {
				t.Fatalf("window position %d: %s, want %s", i, p.ID, order[25-i])
			}
		}
		// forwards to the newest message, without gaps or repeats
		got, cursor := []string{}, win.NextCursor
		for cursor != "" {
			var page []chat.PostView
			if page, cursor, err = w.svc.HistoryAfter(ctx, w.bob, ch.ID, cursor, 4); err != nil {
				t.Fatal(err)
			}
			for _, p := range page {
				got = append(got, p.ID)
			}
		}
		if !slices.Equal(got, order[26:]) {
			t.Fatalf("after: %d posts, want %d in order", len(got), len(order[26:]))
		}
		// backwards with the history's own cursor
		older, _, err := w.svc.History(ctx, w.bob, ch.ID, win.PrevCursor, 200)
		if err != nil || len(older) != 15 || older[0].ID != order[14] {
			t.Fatalf("before the window: %d posts %v", len(older), err)
		}
		// the edges: no cursor where the history ends
		first, _ := w.svc.HistoryAround(ctx, w.bob, ch.ID, order[0], 10)
		last, _ := w.svc.HistoryAround(ctx, w.bob, ch.ID, order[40], 10)
		if first.PrevCursor != "" || len(first.Posts) != 6 || last.NextCursor != "" || len(last.Posts) != 6 {
			t.Fatalf("edges: %+v / %+v", first, last)
		}
		// a message of another channel, or a channel the caller cannot read
		priv, _ := w.svc.CreateChannel(ctx, w.ana, ch.TeamID, chat.NewChannel{Name: "finance", DisplayName: "Finance", Type: chat.ChannelPrivate})
		secret := post(t, w, w.ana, priv.ID, "budget")
		if _, err := w.svc.HistoryAround(ctx, w.bob, ch.ID, secret.ID, 10); !errors.Is(err, chat.ErrNotFound) {
			t.Fatalf("around a message of another channel: %v", err)
		}
		if _, err := w.svc.HistoryAround(ctx, w.bob, priv.ID, secret.ID, 10); !errors.Is(err, chat.ErrNotFound) {
			t.Fatalf("around in a private channel of others: %v", err)
		}
	})
}

// TestUserUpdatedAudience: a profile change reaches the users who may see the
// user and nobody else (profile-images spec, realtime-events catalog).
func TestUserUpdatedAudience(t *testing.T) {
	dbtest.Each(t, func(t *testing.T, db *database.DB) {
		w, _ := postWorld(t, db) // ana, bob and the guest share a team; the organization's admin is in none
		// ana: every member who is not a guest may see her (also the admin, outside her teams), and the guest
		// because they share a channel
		if err := w.svc.UserUpdated(ctx, w.ana.UserID); err != nil {
			t.Fatal(err)
		}
		got := w.rec.to("user_updated")
		for _, who := range []chat.Subject{w.ana, w.bob, w.admin, w.owner, w.gu} {
			if !slices.Contains(got, who.UserID) {
				t.Fatalf("user_updated of ana misses %s: %v", who.UserID, got)
			}
		}
		// the admin shares nothing with the guest: the guest may not see them, so hears nothing
		w.rec.mu.Lock()
		w.rec.events = nil
		w.rec.mu.Unlock()
		if err := w.svc.UserUpdated(ctx, w.admin.UserID); err != nil {
			t.Fatal(err)
		}
		got = w.rec.to("user_updated")
		if !slices.Contains(got, w.ana.UserID) || !slices.Contains(got, w.admin.UserID) || slices.Contains(got, w.gu.UserID) {
			t.Fatalf("user_updated of the admin to %v", got)
		}
	})
}
