package chat_test

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"slices"
	"sync/atomic"
	"testing"
	"time"

	"github.com/hibiken/asynq"

	"github.com/jniltinho/meethub/internal/chat"
	"github.com/jniltinho/meethub/internal/database"
	"github.com/jniltinho/meethub/internal/database/dbtest"
	"github.com/jniltinho/meethub/internal/jobs"
	"github.com/jniltinho/meethub/internal/testenv"
)

// activityWorld is postWorld plus the admin ("carla" of the spec) in #projects.
func activityWorld(t *testing.T, db *database.DB) (*world, *chat.Channel) {
	t.Helper()
	w, ch := postWorld(t, db)
	if err := w.svc.AddTeamMember(ctx, w.owner, ch.TeamID, w.admin.UserID); err != nil {
		t.Fatal(err)
	}
	if err := w.svc.JoinChannel(ctx, w.admin, ch.ID); err != nil {
		t.Fatal(err)
	}
	return w, ch
}

func activity(t *testing.T, w *world, sub chat.Subject) ([]chat.ActivityView, int64) {
	t.Helper()
	items, _, unseen, err := w.svc.Activity(ctx, sub, "", 0)
	if err != nil {
		t.Fatal(err)
	}
	return items, unseen
}

// kinds lists the kinds of a person's items for one message.
func kinds(t *testing.T, w *world, sub chat.Subject, postID string) []string {
	t.Helper()
	items, _ := activity(t, w, sub)
	var out []string
	for _, it := range items {
		if it.Post.ID == postID {
			out = append(out, it.Kind)
		}
	}
	return out
}

// unseenEvents returns the unseen counts sent in activity_updated to each recipient, in order.
func unseenEvents(w *world) map[string][]int64 {
	w.rec.mu.Lock()
	defer w.rec.mu.Unlock()
	out := map[string][]int64{}
	for _, e := range w.rec.events {
		if e.Name != "activity_updated" {
			continue
		}
		for _, r := range e.Recipients {
			out[r] = append(out[r], e.Data["unseen"].(int64))
		}
	}
	return out
}

func TestActivityCreation(t *testing.T) {
	dbtest.Each(t, func(t *testing.T, db *database.DB) {
		w, ch := activityWorld(t, db)
		ana, bob, carla := w.ana, w.bob, w.admin

		// Mention: Ana gets a mention, Bruno (the author) nothing
		m := post(t, w, bob, ch.ID, "@"+username(t, db, ana.UserID)+" can you review?")
		if got := kinds(t, w, ana, m.ID); !slices.Equal(got, []string{chat.ActivityMention}) {
			t.Fatalf("ana: %v", got)
		}
		if got := kinds(t, w, bob, m.ID); got != nil {
			t.Fatalf("author: %v", got)
		}
		// a self-mention is no item
		self := post(t, w, bob, ch.ID, "note to @"+username(t, db, bob.UserID))
		if got := kinds(t, w, bob, self.ID); got != nil {
			t.Fatalf("self mention: %v", got)
		}

		// Reply in my thread: Ana started it, Carla replied; Bruno's reply reaches both
		root := post(t, w, ana, ch.ID, "plan for friday")
		reply(t, w, carla, root.ID, ch.ID, "sounds good")
		r := reply(t, w, bob, root.ID, ch.ID, "I can help")
		for _, who := range []chat.Subject{ana, carla} {
			if got := kinds(t, w, who, r.ID); !slices.Equal(got, []string{chat.ActivityReply}) {
				t.Fatalf("%s: %v", who.UserID, got)
			}
		}
		if got := kinds(t, w, bob, r.ID); got != nil {
			t.Fatalf("reply author: %v", got)
		}

		// mentioned and in the thread: one item, a mention
		both := reply(t, w, bob, root.ID, ch.ID, "@"+username(t, db, ana.UserID)+" @"+username(t, db, ana.UserID)+" ok?")
		if got := kinds(t, w, ana, both.ID); !slices.Equal(got, []string{chat.ActivityMention}) {
			t.Fatalf("both: %v", got)
		}
		if got := kinds(t, w, carla, both.ID); !slices.Equal(got, []string{chat.ActivityReply}) {
			t.Fatalf("carla on both: %v", got)
		}

		// Channel-wide mention: nobody
		wide := post(t, w, bob, ch.ID, "@channel deploy at 17h, @here too")
		for _, who := range []chat.Subject{ana, carla, bob} {
			if got := kinds(t, w, who, wide.ID); got != nil {
				t.Fatalf("@channel gave %s %v", who.UserID, got)
			}
		}

		// editing creates nothing
		if _, err := w.svc.EditPost(ctx, bob, wide.ID, "@"+username(t, db, carla.UserID)+" edited"); err != nil {
			t.Fatal(err)
		}
		if got := kinds(t, w, carla, wide.ID); got != nil {
			t.Fatalf("edit created %v", got)
		}

		// deleting the root removes its items and its replies' (Ana: Carla's and Bruno's two replies); the counts
		// follow, live
		_, before := activity(t, w, ana)
		if err := w.svc.DeletePost(ctx, ana, root.ID); err != nil {
			t.Fatal(err)
		}
		items, after := activity(t, w, ana)
		if after != before-3 || len(items) != 1 || items[0].Post.ID != m.ID {
			t.Fatalf("after delete: %d → %d, %+v", before, after, items)
		}
		if ev := unseenEvents(w)[ana.UserID]; ev[len(ev)-1] != after {
			t.Fatalf("events to ana %v, unseen %d", ev, after)
		}
		var rows int64
		db.Gorm.Model(&chat.ActivityItem{}).Where("post_id IN ?", []string{root.ID, r.ID, both.ID}).Count(&rows)
		if rows != 0 {
			t.Fatalf("%d items of deleted messages remain", rows)
		}
	})
}

func TestActivityDirectMessagesHaveNone(t *testing.T) {
	dbtest.Each(t, func(t *testing.T, db *database.DB) {
		w, _ := activityWorld(t, db)
		dm, err := w.svc.OpenDirect(ctx, w.bob, w.ana.UserID)
		if err != nil {
			t.Fatal(err)
		}
		p := post(t, w, w.bob, dm.ID, "@"+username(t, db, w.ana.UserID)+" hi")
		if got := kinds(t, w, w.ana, p.ID); got != nil {
			t.Fatalf("direct message: %v", got)
		}
	})
}

func TestActivityListAccessPagingAndSeen(t *testing.T) {
	dbtest.Each(t, func(t *testing.T, db *database.DB) {
		w, ch := activityWorld(t, db)
		at := username(t, db, w.ana.UserID)
		devops, err := w.svc.CreateChannel(ctx, w.bob, ch.TeamID, chat.NewChannel{Name: "devops", DisplayName: "DevOps", Type: chat.ChannelOpen})
		if err != nil {
			t.Fatal(err)
		}
		if err := w.svc.JoinChannel(ctx, w.ana, devops.ID); err != nil {
			t.Fatal(err)
		}
		gone := post(t, w, w.bob, devops.ID, "@"+at+" in devops")
		var want []string
		for i := range 5 {
			want = append(want, post(t, w, w.bob, ch.ID, "@"+at+" number "+string(rune('a'+i))).ID)
		}
		slices.Reverse(want) // newest first

		// only Ana is told, with her count
		if got := w.rec.to("activity_updated"); !slices.Equal(got, []string{w.ana.UserID}) {
			t.Fatalf("activity_updated to %v", got)
		}
		if ev := unseenEvents(w)[w.ana.UserID]; ev[len(ev)-1] != 6 {
			t.Fatalf("live counts %v", ev)
		}
		if ev := unseenEvents(w); len(ev[w.admin.UserID])+len(ev[w.bob.UserID]) != 0 {
			t.Fatalf("others told: %v", ev)
		}

		// After leaving the channel: that mention is not listed, the count drops, live
		if err := w.svc.RemoveChannelMember(ctx, w.ana, devops.ID, w.ana.UserID); err != nil {
			t.Fatal(err)
		}
		if ev := unseenEvents(w)[w.ana.UserID]; ev[len(ev)-1] != 5 {
			t.Fatalf("after leaving %v", ev)
		}
		var got []string
		cursor := ""
		for {
			items, next, unseen, err := w.svc.Activity(ctx, w.ana, cursor, 2)
			if err != nil {
				t.Fatal(err)
			}
			if unseen != 5 {
				t.Fatalf("unseen %d", unseen)
			}
			for _, it := range items {
				if it.Post.ID == gone.ID {
					t.Fatal("an item of a channel she left is listed")
				}
				if it.Channel.ID != ch.ID || it.Channel.Name != "projects" || it.Post.Message == "" {
					t.Fatalf("item %+v", it)
				}
				got = append(got, it.Post.ID)
			}
			if cursor = next; cursor == "" {
				break
			}
		}
		if !slices.Equal(got, want) {
			t.Fatalf("pages %v, want %v", got, want)
		}
		if _, _, _, err := w.svc.Activity(ctx, w.ana, "bad", 2); err == nil {
			t.Fatal("a bad cursor was accepted")
		}

		// Seen: the count resets and her connections are told 0
		time.Sleep(2 * time.Millisecond) // after the last item's millisecond
		if err := w.svc.SeeActivity(ctx, w.ana); err != nil {
			t.Fatal(err)
		}
		if _, unseen := activity(t, w, w.ana); unseen != 0 {
			t.Fatalf("unseen after seen: %d", unseen)
		}
		if ev := unseenEvents(w)[w.ana.UserID]; ev[len(ev)-1] != 0 {
			t.Fatalf("seen events %v", ev)
		}
		time.Sleep(2 * time.Millisecond)
		post(t, w, w.bob, ch.ID, "@"+at+" one more")
		if _, unseen := activity(t, w, w.ana); unseen != 1 {
			t.Fatalf("new item after seen: %d", unseen)
		}
		// the same millisecond as the item seen: still unseen (the mark is the item, not the clock)
		fixed := time.Now().Add(time.Hour)
		w.svc.Now = func() time.Time { return fixed }
		post(t, w, w.bob, ch.ID, "@"+at+" at the same time")
		if err := w.svc.SeeActivity(ctx, w.ana); err != nil {
			t.Fatal(err)
		}
		post(t, w, w.bob, ch.ID, "@"+at+" in the same millisecond")
		w.svc.Now = nil
		if _, unseen := activity(t, w, w.ana); unseen != 1 {
			t.Fatalf("an item of the same millisecond as the seen mark: unseen %d", unseen)
		}
		// rejoining a channel whose items were kept counts them again, live
		before := len(unseenEvents(w)[w.ana.UserID])
		if err := w.svc.JoinChannel(ctx, w.ana, devops.ID); err != nil {
			t.Fatal(err)
		}
		if ev := unseenEvents(w)[w.ana.UserID]; len(ev) != before+1 || ev[len(ev)-1] != 1 {
			t.Fatalf("after rejoining %v", ev) // the devops mention was seen (before the mark)
		}
	})
}

func TestActivityRetention(t *testing.T) {
	dbtest.Each(t, func(t *testing.T, db *database.DB) {
		w, ch := activityWorld(t, db)
		at := username(t, db, w.ana.UserID)
		w.svc.Now = func() time.Time { return time.Now().Add(-91 * 24 * time.Hour) }
		old := post(t, w, w.bob, ch.ID, "@"+at+" long ago")
		w.svc.Now = func() time.Time { return time.Now().Add(-89 * 24 * time.Hour) }
		recent := post(t, w, w.bob, ch.ID, "@"+at+" not so long ago")
		w.svc.Now = nil
		n, users, err := chat.PurgeActivity(ctx, db.Gorm, time.Now())
		if err != nil || n != 1 || !slices.Equal(users, []string{w.ana.UserID}) {
			t.Fatalf("purged %d: %v", n, err)
		}
		items, _ := activity(t, w, w.ana)
		if len(items) != 1 || items[0].Post.ID != recent.ID || items[0].Post.ID == old.ID {
			t.Fatalf("left %+v", items)
		}
	})
}

// TestActivityCleanupRunsOnceAcrossReplicas: two replicas schedule the same day; the cleanup runs once.
func TestActivityCleanupRunsOnceAcrossReplicas(t *testing.T) {
	dbtest.Each(t, func(t *testing.T, db *database.DB) {
		addr := testenv.RedisAddr(t)
		w, ch := activityWorld(t, db)
		w.svc.Now = func() time.Time { return time.Now().Add(-91 * 24 * time.Hour) }
		post(t, w, w.bob, ch.ID, "@"+username(t, db, w.ana.UserID)+" long ago")
		w.svc.Now = nil
		b := make([]byte, 4)
		_, _ = rand.Read(b)
		queue := "t_" + hex.EncodeToString(b)
		t.Cleanup(func() {
			insp := asynq.NewInspector(asynq.RedisClientOpt{Addr: addr})
			_ = insp.DeleteQueue(queue, true)
			_ = insp.Close()
		})
		var runs atomic.Int32
		replica := func() *jobs.Runner {
			tt := w.svc.ActivityCleanupTask()
			handler := tt.Handler
			tt.Handler = func(ctx context.Context, task *asynq.Task) error {
				runs.Add(1)
				return handler(ctx, task)
			}
			r, err := jobs.New(context.Background(), jobs.Options{
				Redis: asynq.RedisClientOpt{Addr: addr}, ConnectTimeout: 5 * time.Second, Queue: queue,
				Concurrency: 2, Shutdown: 5 * time.Second, PollInterval: 50 * time.Millisecond, DB: db,
			})
			if err != nil {
				t.Fatal(err)
			}
			r.Register(tt)
			t.Cleanup(func() { _ = r.Close() })
			c, stop := context.WithCancel(context.Background())
			done := make(chan struct{})
			go func() { _ = r.Run(c); close(done) }()
			t.Cleanup(func() { stop(); <-done })
			return r
		}
		a, b2 := replica(), replica()
		day := jobs.PeriodStart(time.Now(), 24*time.Hour)
		for _, r := range []*jobs.Runner{a, b2} {
			_ = r.EnqueueOccurrence(context.Background(), chat.TaskActivityCleanup, day)
		}
		deadline := time.Now().Add(10 * time.Second)
		for {
			if items, _ := activity(t, w, w.ana); len(items) == 0 {
				break
			}
			if time.Now().After(deadline) {
				t.Fatal("the old item was not removed")
			}
			time.Sleep(50 * time.Millisecond)
		}
		time.Sleep(time.Second) // a second run would have started by now
		if n := runs.Load(); n != 1 {
			t.Fatalf("the cleanup ran %d times for one day", n)
		}
	})
}
