package chat_test

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/jniltinho/meethub/internal/audit"
	"github.com/jniltinho/meethub/internal/chat"
	"github.com/jniltinho/meethub/internal/database"
	"github.com/jniltinho/meethub/internal/database/dbtest"
)

var keySeq atomic.Int64

func key() string { return fmt.Sprintf("k-%d", keySeq.Add(1)) }

// postWorld is teamWorld plus a public channel #projects with ana and bob.
func postWorld(t *testing.T, db *database.DB) (*world, *chat.Channel) {
	t.Helper()
	w, team := teamWorld(t, db)
	ch, err := w.svc.CreateChannel(ctx, w.ana, team.ID, chat.NewChannel{Name: "projects", DisplayName: "Projects", Type: chat.ChannelOpen})
	if err != nil {
		t.Fatal(err)
	}
	if err := w.svc.JoinChannel(ctx, w.bob, ch.ID); err != nil {
		t.Fatal(err)
	}
	return w, ch
}

func post(t *testing.T, w *world, sub chat.Subject, channelID, text string) *chat.PostView {
	t.Helper()
	p, err := w.svc.CreatePost(ctx, sub, chat.NewPost{ChannelID: channelID, Message: text, PendingPostID: key()})
	if err != nil {
		t.Fatalf("post %q: %v", text, err)
	}
	return p
}

func reply(t *testing.T, w *world, sub chat.Subject, rootID, channelID, text string) *chat.PostView {
	t.Helper()
	p, err := w.svc.CreatePost(ctx, sub, chat.NewPost{ChannelID: channelID, RootID: rootID, Message: text, PendingPostID: key()})
	if err != nil {
		t.Fatalf("reply %q: %v", text, err)
	}
	return p
}

func (r *recorder) count(name string) int {
	r.mu.Lock()
	defer r.mu.Unlock()
	n := 0
	for _, e := range r.events {
		if e.Name == name {
			n++
		}
	}
	return n
}

func member(t *testing.T, db *database.DB, channelID, userID string) chat.ChannelMember {
	t.Helper()
	var m chat.ChannelMember
	if err := db.Gorm.Where("channel_id = ? AND user_id = ?", channelID, userID).Take(&m).Error; err != nil {
		t.Fatal(err)
	}
	return m
}

func total(t *testing.T, db *database.DB, channelID string) int64 {
	t.Helper()
	var n []int64
	db.Gorm.Model(&chat.Channel{}).Where("id = ?", channelID).Pluck("total_msg_count", &n)
	return n[0]
}

func TestPostStoredThenAnnounced(t *testing.T) {
	dbtest.Each(t, func(t *testing.T, db *database.DB) {
		w, ch := postWorld(t, db)
		_ = post(t, w, w.bob, ch.ID, "first")
		p := post(t, w, w.ana, ch.ID, "  Deploy at 17h  ")
		if p.Message != "Deploy at 17h" || p.ID == "" {
			t.Fatalf("stored: %+v", p)
		}
		if got := w.rec.to("posted"); !slices.Equal(got, sorted([]string{w.ana.UserID, w.bob.UserID})) {
			t.Fatalf("posted to %v", got)
		}
		// the author is read, the other member has one unread
		if m := member(t, db, ch.ID, w.ana.UserID); m.MsgCount != total(t, db, ch.ID) {
			t.Fatalf("author not marked read: %+v", m)
		}
		if m := member(t, db, ch.ID, w.bob.UserID); total(t, db, ch.ID)-m.MsgCount != 1 {
			t.Fatalf("bob unread: %+v", m)
		}
		if _, err := w.svc.CreatePost(ctx, w.owner, chat.NewPost{ChannelID: ch.ID, Message: "hi", PendingPostID: key()}); !errors.Is(err, chat.ErrForbidden) {
			t.Fatalf("not joined: %v", err)
		}
		for _, in := range []chat.NewPost{
			{ChannelID: ch.ID, Message: "   ", PendingPostID: key()},
			{ChannelID: ch.ID, Message: "x", PendingPostID: "bad key!"},
			{ChannelID: ch.ID, Message: "x", PendingPostID: ""},
		} {
			if _, err := w.svc.CreatePost(ctx, w.ana, in); !errors.Is(err, chat.ErrInvalidInput) {
				t.Fatalf("%+v: %v", in, err)
			}
		}
		if _, err := w.svc.CreatePost(ctx, w.ana, chat.NewPost{ChannelID: ch.ID, Message: strings.Repeat("é", 16384), PendingPostID: key()}); !errors.Is(err, chat.ErrTooLong) {
			t.Fatalf("too long: %v", err)
		}
		_ = w.svc.SetChannelArchived(ctx, w.ana, ch.ID, true)
		if _, err := w.svc.CreatePost(ctx, w.ana, chat.NewPost{ChannelID: ch.ID, Message: "x", PendingPostID: key()}); !errors.Is(err, chat.ErrArchived) {
			t.Fatalf("archived: %v", err)
		}
	})
}

func TestIdempotentPosting(t *testing.T) {
	dbtest.Each(t, func(t *testing.T, db *database.DB) {
		w, ch := postWorld(t, db)
		in := chat.NewPost{ChannelID: ch.ID, Message: "once", PendingPostID: key()}
		a, err := w.svc.CreatePost(ctx, w.ana, in)
		if err != nil {
			t.Fatal(err)
		}
		events := w.rec.count("posted")
		b, err := w.svc.CreatePost(ctx, w.ana, in)
		if err != nil || b.ID != a.ID || w.rec.count("posted") != events {
			t.Fatalf("retry: %+v %v (events %d→%d)", b, err, events, w.rec.count("posted"))
		}
		changed := in
		changed.Message = "twice"
		if _, err := w.svc.CreatePost(ctx, w.ana, changed); !errors.Is(err, chat.ErrPendingPostConflict) {
			t.Fatalf("changed text: %v", err)
		}
		// the same key by another author is another message
		if c, err := w.svc.CreatePost(ctx, w.bob, in); err != nil || c.ID == a.ID {
			t.Fatalf("other author: %+v %v", c, err)
		}

		// concurrent identical retries on two "replicas" (two services, one database)
		other := &chat.Service{DB: db, Publisher: w.rec}
		for round := range 10 {
			in := chat.NewPost{ChannelID: ch.ID, Message: fmt.Sprintf("race %d", round), PendingPostID: key()}
			ids := make([]string, 2)
			errs := make([]error, 2)
			var wg sync.WaitGroup
			for i, svc := range []*chat.Service{w.svc, other} {
				wg.Go(func() {
					p, err := svc.CreatePost(ctx, w.ana, in)
					errs[i] = err
					if p != nil {
						ids[i] = p.ID
					}
				})
			}
			wg.Wait()
			if errs[0] != nil || errs[1] != nil || ids[0] != ids[1] {
				t.Fatalf("round %d: %v %v %v", round, ids, errs[0], errs[1])
			}
		}
		var n int64
		db.Gorm.Model(&chat.Post{}).Where("message LIKE ?", "race %").Count(&n)
		if n != 10 {
			t.Fatalf("%d race posts stored, want 10", n)
		}

		// the same key sent concurrently to two channels: one wins, the other conflicts
		team := ch.TeamID
		ch2, _ := w.svc.CreateChannel(ctx, w.ana, team, chat.NewChannel{Name: "other", DisplayName: "Other", Type: chat.ChannelOpen})
		k := key()
		var wg sync.WaitGroup
		errs := make([]error, 2)
		for i, c := range []string{ch.ID, ch2.ID} {
			wg.Go(func() {
				_, errs[i] = w.svc.CreatePost(ctx, w.ana, chat.NewPost{ChannelID: c, Message: "where", PendingPostID: k})
			})
		}
		wg.Wait()
		if (errs[0] == nil) == (errs[1] == nil) || (!errors.Is(errs[0], chat.ErrPendingPostConflict) && !errors.Is(errs[1], chat.ErrPendingPostConflict)) {
			t.Fatalf("cross-channel key: %v / %v", errs[0], errs[1])
		}

		// a retry after losing access does not reveal the message
		k = key()
		priv, _ := w.svc.CreateChannel(ctx, w.ana, team, chat.NewChannel{Name: "secret", DisplayName: "Secret", Type: chat.ChannelPrivate})
		_ = w.svc.AddChannelMember(ctx, w.ana, priv.ID, w.bob.UserID)
		if _, err := w.svc.CreatePost(ctx, w.bob, chat.NewPost{ChannelID: priv.ID, Message: "psst", PendingPostID: k}); err != nil {
			t.Fatal(err)
		}
		_ = w.svc.RemoveChannelMember(ctx, w.ana, priv.ID, w.bob.UserID)
		if _, err := w.svc.CreatePost(ctx, w.bob, chat.NewPost{ChannelID: priv.ID, Message: "psst", PendingPostID: k}); !errors.Is(err, chat.ErrNotFound) {
			t.Fatalf("retry after removal: %v", err)
		}
	})
}

type fakeLimiter struct {
	mu    sync.Mutex
	left  int
	calls int
}

func (f *fakeLimiter) Allow(context.Context, string) (bool, time.Duration) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.calls++
	if f.left == 0 {
		return false, 3 * time.Second
	}
	f.left--
	return true, 0
}

func TestPostRateLimit(t *testing.T) {
	dbtest.Each(t, func(t *testing.T, db *database.DB) {
		w, ch := postWorld(t, db)
		lim := &fakeLimiter{left: 1}
		w.svc.Limiter = lim
		in := chat.NewPost{ChannelID: ch.ID, Message: "one", PendingPostID: key()}
		if _, err := w.svc.CreatePost(ctx, w.ana, in); err != nil {
			t.Fatal(err)
		}
		if _, err := w.svc.CreatePost(ctx, w.ana, in); err != nil || lim.calls != 1 {
			t.Fatalf("a repeat was rate limited or counted: %v, %d calls", err, lim.calls)
		}
		var rl *chat.RateLimitedError
		if _, err := w.svc.CreatePost(ctx, w.ana, chat.NewPost{ChannelID: ch.ID, Message: "two", PendingPostID: key()}); !errors.As(err, &rl) || rl.RetryAfter != 3*time.Second {
			t.Fatalf("over the limit: %v", err)
		}
	})
}

func TestThreads(t *testing.T) {
	dbtest.Each(t, func(t *testing.T, db *database.DB) {
		w, ch := postWorld(t, db)
		root := post(t, w, w.ana, ch.ID, "root")
		r1 := reply(t, w, w.bob, root.ID, ch.ID, "r1")
		_ = reply(t, w, w.ana, r1.ID, ch.ID, "reply to a reply") // attaches to the root
		r3 := reply(t, w, w.bob, root.ID, ch.ID, "r3")
		thread, next, err := w.svc.Thread(ctx, w.bob, r1.ID, "", 0)
		if err != nil || next != "" || len(thread) != 4 || thread[0].ID != root.ID || thread[2].RootID != root.ID {
			t.Fatalf("thread: %d posts, %v", len(thread), err)
		}
		if thread[0].ReplyCount != 3 || thread[0].LastReplyAt != r3.CreateAt || thread[0].Revision <= root.Revision {
			t.Fatalf("root counters: %+v", thread[0])
		}
		page, next, _ := w.svc.Thread(ctx, w.bob, root.ID, "", 2)
		rest, last, _ := w.svc.Thread(ctx, w.bob, root.ID, next, 2)
		if len(page) != 3 || len(rest) != 1 || last != "" || rest[0].ID != r3.ID {
			t.Fatalf("thread pages: %d + %d", len(page), len(rest))
		}
		if err := w.svc.DeletePost(ctx, w.bob, r3.ID); err != nil {
			t.Fatal(err)
		}
		thread, _, _ = w.svc.Thread(ctx, w.bob, root.ID, "", 0)
		if thread[0].ReplyCount != 2 || thread[0].LastReplyAt == r3.CreateAt {
			t.Fatalf("after deleting a reply: %+v", thread[0])
		}
		if _, err := w.svc.CreatePost(ctx, w.ana, chat.NewPost{ChannelID: ch.ID, RootID: "01NOPE00000000000000000000", Message: "x", PendingPostID: key()}); !errors.Is(err, chat.ErrInvalidInput) {
			t.Fatalf("unknown root: %v", err)
		}
	})
}

func TestReplyRacingRootDeletion(t *testing.T) {
	dbtest.Each(t, func(t *testing.T, db *database.DB) {
		w, ch := postWorld(t, db)
		for range 10 {
			root := post(t, w, w.ana, ch.ID, "root")
			var wg sync.WaitGroup
			var replyErr error
			var r *chat.PostView
			wg.Go(func() {
				r, replyErr = w.svc.CreatePost(ctx, w.bob, chat.NewPost{ChannelID: ch.ID, RootID: root.ID, Message: "late", PendingPostID: key()})
			})
			wg.Go(func() { _ = w.svc.DeletePost(ctx, w.ana, root.ID) })
			wg.Wait()
			if replyErr != nil {
				if !errors.Is(replyErr, chat.ErrInvalidInput) {
					t.Fatal(replyErr)
				}
				continue
			}
			// stored before the deletion: deleted with its thread
			var p chat.Post
			db.Gorm.Where("id = ?", r.ID).Take(&p)
			if p.DeleteAt == 0 {
				t.Fatal("a reply outlived its deleted root")
			}
		}
	})
}

func TestEditAndDelete(t *testing.T) {
	dbtest.Each(t, func(t *testing.T, db *database.DB) {
		w, ch := postWorld(t, db)
		fixed := time.UnixMilli(1_800_000_000_000)
		w.svc.Now = func() time.Time { return fixed }
		p := post(t, w, w.ana, ch.ID, "v1")
		if _, err := w.svc.EditPost(ctx, w.bob, p.ID, "hacked"); !errors.Is(err, chat.ErrForbidden) {
			t.Fatalf("editing another's message: %v", err)
		}
		e1, err := w.svc.EditPost(ctx, w.ana, p.ID, "v2")
		if err != nil {
			t.Fatal(err)
		}
		e2, _ := w.svc.EditPost(ctx, w.ana, p.ID, "v3") // same millisecond
		if e1.Revision == e2.Revision || e2.Revision < e1.Revision || e2.EditAt == 0 {
			t.Fatalf("edits: %d then %d", e1.Revision, e2.Revision)
		}
		root := post(t, w, w.bob, ch.ID, "secret root")
		_ = reply(t, w, w.ana, root.ID, ch.ID, "secret reply")
		_ = reply(t, w, w.bob, root.ID, ch.ID, "secret reply 2")
		unread := total(t, db, ch.ID) - member(t, db, ch.ID, w.bob.UserID).MsgCount
		deletes := w.rec.count("post_deleted")
		if err := w.svc.DeletePost(ctx, w.bob, root.ID); err != nil {
			t.Fatal(err)
		}
		if w.rec.count("post_deleted") != deletes+1 {
			t.Fatal("a thread deletion sent more than one event")
		}
		w.rec.mu.Lock()
		last := w.rec.events[len(w.rec.events)-1]
		w.rec.mu.Unlock()
		if last.Data["delete_thread"] != true || last.Data["post"].(chat.PostView).Message != "" {
			t.Fatalf("post_deleted: %+v", last.Data)
		}
		if got := total(t, db, ch.ID) - member(t, db, ch.ID, w.bob.UserID).MsgCount; got != unread {
			t.Fatalf("deletion changed unread %d → %d", unread, got)
		}
		// text never leaks: history, catch-up and thread
		hist, _, _ := w.svc.History(ctx, w.ana, ch.ID, "", 0)
		changes, _ := w.svc.ChangesSince(ctx, w.ana, ch.ID, 0, 0)
		thread, _, _ := w.svc.Thread(ctx, w.ana, root.ID, "", 0)
		for _, list := range [][]chat.PostView{hist, changes.Posts, thread} {
			for _, v := range list {
				if strings.Contains(v.Message, "secret") || (v.DeleteAt != 0 && v.Message != "") {
					t.Fatalf("deleted text leaked: %+v", v)
				}
			}
		}
		if len(thread) != 3 {
			t.Fatalf("thread tombstones: %d", len(thread))
		}
		// a channel admin (ana created #projects) deletes bob's message: audited
		bp := post(t, w, w.bob, ch.ID, "spam")
		if err := w.svc.DeletePost(ctx, w.bob, p.ID); !errors.Is(err, chat.ErrForbidden) {
			t.Fatalf("member deleting another's message: %v", err)
		}
		if err := w.svc.DeletePost(ctx, w.ana, bp.ID); err != nil {
			t.Fatal(err)
		}
		ev, _ := audit.List(ctx, db.Gorm, audit.Filter{Action: audit.ActionPostModerated})
		if len(ev.Events) != 1 {
			t.Fatal("moderation not audited")
		}
	})
}

func TestHistoryWithEqualTimestamps(t *testing.T) {
	dbtest.Each(t, func(t *testing.T, db *database.DB) {
		w, ch := postWorld(t, db)
		fixed := time.UnixMilli(1_800_000_000_000)
		w.svc.Now = func() time.Time { return fixed }
		for i := range 7 {
			post(t, w, w.ana, ch.ID, fmt.Sprintf("m%d", i))
		}
		seen, cursor := map[string]bool{}, ""
		for {
			page, next, err := w.svc.History(ctx, w.bob, ch.ID, cursor, 3)
			if err != nil {
				t.Fatal(err)
			}
			for _, p := range page {
				if seen[p.ID] {
					t.Fatalf("%s twice", p.ID)
				}
				seen[p.ID] = true
			}
			if cursor = next; cursor == "" {
				break
			}
		}
		if len(seen) != 7 {
			t.Fatalf("saw %d of 7", len(seen))
		}
		if _, _, err := w.svc.History(ctx, w.owner, ch.ID, "", 0); !errors.Is(err, chat.ErrForbidden) {
			t.Fatalf("non-member history: %v", err)
		}
	})
}

// TestCatchUpPagedDuringWrites: 450 changes since a revision, paged while
// other clients keep posting, are each received exactly once.
func TestCatchUpPagedDuringWrites(t *testing.T) {
	dbtest.Each(t, func(t *testing.T, db *database.DB) {
		w, ch := postWorld(t, db)
		start, _ := w.svc.ChangesSince(ctx, w.bob, ch.ID, 0, 0)
		since := start.NextRevision
		var ids []string
		for i := range 400 {
			ids = append(ids, post(t, w, w.ana, ch.ID, fmt.Sprintf("m%d", i)).ID)
		}
		for _, id := range ids[:50] { // 50 edits: those posts come once, at their new revision
			if _, err := w.svc.EditPost(ctx, w.ana, id, "edited"); err != nil {
				t.Fatal(err)
			}
		}
		root := post(t, w, w.ana, ch.ID, "thread")
		for range 49 {
			reply(t, w, w.bob, root.ID, ch.ID, "r")
		}
		_ = w.svc.DeletePost(ctx, w.ana, root.ID) // 50 posts stamped with one revision
		want := map[string]bool{}
		var all []chat.Post
		db.Gorm.Where("channel_id = ? AND revision > ?", ch.ID, since).Find(&all)
		for _, p := range all {
			want[strings.TrimSpace(p.ID)] = true
		}

		stop := make(chan struct{})
		var wg sync.WaitGroup
		wg.Go(func() {
			for {
				select {
				case <-stop:
					return
				default:
					_, _ = w.svc.CreatePost(ctx, w.bob, chat.NewPost{ChannelID: ch.ID, Message: "live", PendingPostID: key()})
				}
			}
		})
		seen := map[string]int{}
		rev := since
		for {
			page, err := w.svc.ChangesSince(ctx, w.bob, ch.ID, rev, 0)
			if err != nil {
				t.Fatal(err)
			}
			for _, p := range page.Posts {
				if p.Revision <= rev {
					t.Fatalf("revision %d not after %d", p.Revision, rev)
				}
				seen[p.ID]++
			}
			rev = page.NextRevision
			if !page.HasMore {
				break
			}
		}
		close(stop)
		wg.Wait()
		for id := range want {
			if seen[id] != 1 {
				t.Fatalf("post %s received %d times", id, seen[id])
			}
		}
	})
}

func TestThousandMessagesInOrder(t *testing.T) {
	dbtest.Each(t, func(t *testing.T, db *database.DB) {
		w, ch := postWorld(t, db)
		var wg sync.WaitGroup
		for _, sub := range []chat.Subject{w.ana, w.bob, w.ana, w.bob} {
			wg.Go(func() {
				for range 250 {
					if _, err := w.svc.CreatePost(ctx, sub, chat.NewPost{ChannelID: ch.ID, Message: "m", PendingPostID: key()}); err != nil {
						t.Error(err)
						return
					}
				}
			})
		}
		wg.Wait()
		var got []chat.PostView
		cursor := ""
		for {
			page, next, err := w.svc.History(ctx, w.ana, ch.ID, cursor, 200)
			if err != nil {
				t.Fatal(err)
			}
			got = append(got, page...)
			if cursor = next; cursor == "" {
				break
			}
		}
		if len(got) != 1000 {
			t.Fatalf("history has %d messages", len(got))
		}
		ids := map[string]bool{}
		for i, p := range got {
			if ids[p.ID] {
				t.Fatal("duplicate in history")
			}
			ids[p.ID] = true
			if i > 0 && (p.CreateAt > got[i-1].CreateAt || (p.CreateAt == got[i-1].CreateAt && p.ID > got[i-1].ID)) {
				t.Fatalf("out of order at %d", i)
			}
		}
		if total(t, db, ch.ID) != 1000 {
			t.Fatal("total_msg_count lost an increment")
		}
	})
}
