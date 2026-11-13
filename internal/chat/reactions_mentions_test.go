package chat_test

import (
	"context"
	"errors"
	"slices"
	"sync"
	"testing"

	"github.com/jniltinho/meethub/internal/chat"
	"github.com/jniltinho/meethub/internal/database"
	"github.com/jniltinho/meethub/internal/database/dbtest"
)

func TestReactions(t *testing.T) {
	dbtest.Each(t, func(t *testing.T, db *database.DB) {
		w, ch := postWorld(t, db)
		p := post(t, w, w.ana, ch.ID, "ship it")
		if err := w.svc.AddReaction(ctx, w.ana, p.ID, "+1"); err != nil {
			t.Fatal(err)
		}
		n := w.rec.count("reaction_added")
		if err := w.svc.AddReaction(ctx, w.ana, p.ID, "+1"); err != nil || w.rec.count("reaction_added") != n {
			t.Fatalf("same reaction twice: %v, events %d→%d", err, n, w.rec.count("reaction_added"))
		}
		if err := w.svc.AddReaction(ctx, w.bob, p.ID, "white_check_mark"); err != nil {
			t.Fatal(err)
		}
		if got := w.rec.to("reaction_added"); !slices.Contains(got, w.ana.UserID) {
			t.Fatalf("reaction_added to %v", got)
		}
		if err := w.svc.AddReaction(ctx, w.bob, p.ID, "not_an_emoji"); !errors.Is(err, chat.ErrInvalidInput) {
			t.Fatalf("unknown emoji: %v", err)
		}
		if err := w.svc.AddReaction(ctx, w.owner, p.ID, "rocket"); !errors.Is(err, chat.ErrForbidden) {
			t.Fatalf("non-member reacting: %v", err)
		}
		for _, s := range []chat.Subject{w.ana, w.bob} {
			_ = w.svc.AddReaction(ctx, s, p.ID, "rocket")
		}
		_ = w.svc.JoinChannel(ctx, w.owner, ch.ID)
		_ = w.svc.AddReaction(ctx, w.owner, p.ID, "rocket")
		hist, _, _ := w.svc.History(ctx, w.bob, ch.ID, "", 0)
		var groups []chat.ReactionGroup
		for _, h := range hist {
			if h.ID == p.ID {
				groups = h.Reactions
			}
		}
		i := slices.IndexFunc(groups, func(g chat.ReactionGroup) bool { return g.EmojiName == "rocket" })
		if len(groups) != 3 || i < 0 || groups[i].Count != 3 || len(groups[i].UserIDs) != 3 {
			t.Fatalf("grouped: %+v", groups)
		}
		if err := w.svc.RemoveReaction(ctx, w.ana, p.ID, "rocket"); err != nil || w.rec.to("reaction_removed") == nil {
			t.Fatalf("remove: %v", err)
		}
		_ = w.svc.SetChannelArchived(ctx, w.ana, ch.ID, true)
		if err := w.svc.AddReaction(ctx, w.bob, p.ID, "tada"); !errors.Is(err, chat.ErrArchived) {
			t.Fatalf("archived: %v", err)
		}
		_ = w.svc.SetChannelArchived(ctx, w.ana, ch.ID, false)
		_ = w.svc.DeletePost(ctx, w.ana, p.ID)
		var left int64
		db.Gorm.Model(&chat.Reaction{}).Where("post_id = ?", p.ID).Count(&left)
		if left != 0 {
			t.Fatal("a deleted message kept its reactions")
		}
	})
}

func TestParseMentions(t *testing.T) {
	tests := []struct {
		text  string
		names []string
		ch    bool
		here  bool
	}{
		{"hi @ana.silva.", []string{"ana.silva"}, false, false},
		{"@Bruno, @channel and @here", []string{"bruno"}, true, true},
		{"`@ana` and ```\n@bob\n```", nil, false, false},
		{"mail me at x@ana.io", nil, false, false},
		{"@all-", nil, true, false},
		{"(@carla_)", []string{"carla"}, false, false},
	}
	for _, tt := range tests {
		m := chat.ParseMentions(tt.text)
		if !slices.Equal(m.Usernames, tt.names) || m.Channel != tt.ch || m.Here != tt.here {
			t.Errorf("%q: %+v", tt.text, m)
		}
	}
}

type onlineSet map[string]bool

func (o onlineSet) Online(_ context.Context, ids []string) (map[string]bool, error) {
	out := map[string]bool{}
	for _, id := range ids {
		if o[id] {
			out[id] = true
		}
	}
	return out, nil
}

func username(t *testing.T, db *database.DB, id string) string {
	t.Helper()
	var u []string
	db.Gorm.Table("users").Where("id = ?", id).Pluck("username", &u)
	return u[0]
}

func TestMentionCounts(t *testing.T) {
	dbtest.Each(t, func(t *testing.T, db *database.DB) {
		w, ch := postWorld(t, db)
		_ = w.svc.JoinChannel(ctx, w.owner, ch.ID)
		mentions := func(id string) int64 { return member(t, db, ch.ID, id).MentionCount }
		bob := username(t, db, w.bob.UserID)

		post(t, w, w.ana, ch.ID, "@channel and @"+bob) // double mention counts once
		if mentions(w.bob.UserID) != 1 || mentions(w.owner.UserID) != 1 || mentions(w.ana.UserID) != 0 {
			t.Fatalf("after @channel: bob %d owner %d ana %d", mentions(w.bob.UserID), mentions(w.owner.UserID), mentions(w.ana.UserID))
		}
		post(t, w, w.ana, ch.ID, "`@"+bob+"`")
		post(t, w, w.ana, ch.ID, "@"+username(t, db, w.admin.UserID)) // not a member
		if mentions(w.bob.UserID) != 1 {
			t.Fatal("mention in code counted")
		}
		w.svc.Presence = onlineSet{w.owner.UserID: true}
		post(t, w, w.ana, ch.ID, "@here")
		if mentions(w.owner.UserID) != 2 || mentions(w.bob.UserID) != 1 {
			t.Fatal("@here reached someone offline or missed someone online")
		}
		// view: read moves forward and mentions reset, other connections told
		if err := w.svc.ViewChannel(ctx, w.bob, ch.ID); err != nil {
			t.Fatal(err)
		}
		if m := member(t, db, ch.ID, w.bob.UserID); m.MentionCount != 0 || m.MsgCount != total(t, db, ch.ID) {
			t.Fatalf("after view: %+v", m)
		}
		if got := w.rec.to("channel_viewed"); !slices.Equal(got, []string{w.bob.UserID}) {
			t.Fatalf("channel_viewed to %v", got)
		}
		// direct messages: every message from the other person is a mention
		dm, _ := w.svc.OpenDirect(ctx, w.ana, w.bob.UserID)
		post(t, w, w.ana, dm.ID, "hello")
		if member(t, db, dm.ID, w.bob.UserID).MentionCount != 1 {
			t.Fatal("DM not counted as a mention")
		}
		if got := w.rec.to("direct_added"); !slices.Equal(got, []string{w.bob.UserID}) {
			t.Fatalf("direct_added to %v", got)
		}
	})
}

// TestConcurrentViewAndMention: the mention is either read or one unread mention.
func TestConcurrentViewAndMention(t *testing.T) {
	dbtest.Each(t, func(t *testing.T, db *database.DB) {
		w, ch := postWorld(t, db)
		bob := username(t, db, w.bob.UserID)
		for range 15 {
			var wg sync.WaitGroup
			wg.Go(func() { post(t, w, w.ana, ch.ID, "@"+bob+" look") })
			wg.Go(func() { _ = w.svc.ViewChannel(ctx, w.bob, ch.ID) })
			wg.Wait()
			m := member(t, db, ch.ID, w.bob.UserID)
			unread := total(t, db, ch.ID) - m.MsgCount
			if (unread == 0 && m.MentionCount != 0) || (unread == 1 && m.MentionCount != 1) || unread > 1 {
				t.Fatalf("unread %d with %d mentions", unread, m.MentionCount)
			}
			_ = w.svc.ViewChannel(ctx, w.bob, ch.ID)
		}
	})
}
