package chat_test

import (
	"errors"
	"testing"
	"time"

	"github.com/jniltinho/meethub/internal/chat"
	"github.com/jniltinho/meethub/internal/database"
	"github.com/jniltinho/meethub/internal/database/dbtest"
)

// TestPinning covers the pinned-messages spec: pin, repeat, who may pin, the
// list's order, live event and catch-up, and deletes clearing pins.
func TestPinning(t *testing.T) {
	dbtest.Each(t, func(t *testing.T, db *database.DB) {
		w, ch := postWorld(t, db)
		clock := time.UnixMilli(1_800_000_000_000)
		w.svc.Now = func() time.Time { clock = clock.Add(time.Second); return clock }
		a := post(t, w, w.bob, ch.ID, "checklist")
		b := post(t, w, w.ana, ch.ID, "rollback plan")

		edits := w.rec.count("post_edited")
		pinned, err := w.svc.PinPost(ctx, w.ana, a.ID)
		if err != nil || !pinned.IsPinned || pinned.PinnedBy != w.ana.UserID || pinned.PinnedAt == 0 || pinned.EditAt != 0 {
			t.Fatalf("pin: %+v %v", pinned, err)
		}
		if pinned.Revision <= a.Revision || w.rec.count("post_edited") != edits+1 {
			t.Fatalf("pin revision %d (was %d), %d events", pinned.Revision, a.Revision, w.rec.count("post_edited")-edits)
		}
		// a repeat changes nothing: first author and time kept, no event
		again, err := w.svc.PinPost(ctx, w.bob, a.ID)
		if err != nil || again.PinnedBy != w.ana.UserID || again.PinnedAt != pinned.PinnedAt || w.rec.count("post_edited") != edits+1 {
			t.Fatalf("pin twice: %+v %v", again, err)
		}
		if _, err := w.svc.PinPost(ctx, w.bob, b.ID); err != nil {
			t.Fatal(err)
		}
		list, next, err := w.svc.PinnedPosts(ctx, w.bob, ch.ID, "", 0)
		if err != nil || next != "" || len(list) != 2 || list[0].ID != b.ID || list[1].ID != a.ID {
			t.Fatalf("pinned list (newest pin first): %+v %q %v", list, next, err)
		}
		page, next, _ := w.svc.PinnedPosts(ctx, w.bob, ch.ID, "", 1)
		rest, last, _ := w.svc.PinnedPosts(ctx, w.bob, ch.ID, next, 1)
		if len(page) != 1 || next == "" || len(rest) != 1 || rest[0].ID != a.ID || last != "" {
			t.Fatalf("paged pins: %+v %q %+v %q", page, next, rest, last)
		}

		// catch-up returns the unpin
		since := again.Revision
		un, err := w.svc.UnpinPost(ctx, w.bob, a.ID)
		if err != nil || un.IsPinned || un.PinnedBy != "" || un.PinnedAt != 0 {
			t.Fatalf("unpin: %+v %v", un, err)
		}
		changes, err := w.svc.ChangesSince(ctx, w.ana, ch.ID, since, 0)
		found := false
		for _, p := range changes.Posts {
			found = found || (p.ID == a.ID && !p.IsPinned)
		}
		if err != nil || !found {
			t.Fatalf("catch-up without the unpin: %+v %v", changes, err)
		}

		// deleting clears the pin: gone from the list, tombstone not pinned
		if err := w.svc.DeletePost(ctx, w.ana, b.ID); err != nil {
			t.Fatal(err)
		}
		if list, _, _ := w.svc.PinnedPosts(ctx, w.ana, ch.ID, "", 0); len(list) != 0 {
			t.Fatalf("pinned after delete: %+v", list)
		}
		if _, err := w.svc.PinPost(ctx, w.ana, b.ID); !errors.Is(err, chat.ErrNotFound) {
			t.Fatalf("pin a deleted message: %v", err)
		}
		// a thread's replies lose their pins with the root
		root := post(t, w, w.ana, ch.ID, "root")
		rep := reply(t, w, w.bob, root.ID, ch.ID, "pinned reply")
		if _, err := w.svc.PinPost(ctx, w.bob, rep.ID); err != nil {
			t.Fatal(err)
		}
		if err := w.svc.DeletePost(ctx, w.ana, root.ID); err != nil {
			t.Fatal(err)
		}
		if list, _, _ := w.svc.PinnedPosts(ctx, w.ana, ch.ID, "", 0); len(list) != 0 {
			t.Fatalf("reply still pinned after its root was deleted: %+v", list)
		}
	})
}

func TestPinningAccess(t *testing.T) {
	dbtest.Each(t, func(t *testing.T, db *database.DB) {
		w, team := teamWorld(t, db)
		priv, err := w.svc.CreateChannel(ctx, w.ana, team.ID, chat.NewChannel{Name: "finance", DisplayName: "Finance", Type: chat.ChannelPrivate})
		if err != nil {
			t.Fatal(err)
		}
		p := post(t, w, w.ana, priv.ID, "budget approved")
		// not a member of the private channel: the message does not exist for bob
		if _, err := w.svc.PinPost(ctx, w.bob, p.ID); !errors.Is(err, chat.ErrNotFound) {
			t.Fatalf("pin by a non-member: %v", err)
		}
		if _, _, err := w.svc.PinnedPosts(ctx, w.bob, priv.ID, "", 0); !errors.Is(err, chat.ErrNotFound) {
			t.Fatalf("pinned list for a non-member: %v", err)
		}
		if _, err := w.svc.PinPost(ctx, w.ana, "01J0000000000000000000NONE"); !errors.Is(err, chat.ErrNotFound) {
			t.Fatalf("unknown message: %v", err)
		}
		if err := w.svc.SetChannelArchived(ctx, w.ana, priv.ID, true); err != nil {
			t.Fatal(err)
		}
		if _, err := w.svc.PinPost(ctx, w.ana, p.ID); !errors.Is(err, chat.ErrArchived) {
			t.Fatalf("pin in an archived channel: %v", err)
		}
	})
}
