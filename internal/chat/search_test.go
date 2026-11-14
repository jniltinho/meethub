package chat_test

import (
	"errors"
	"fmt"
	"slices"
	"strings"
	"testing"

	"github.com/jniltinho/meethub/internal/chat"
	"github.com/jniltinho/meethub/internal/database"
	"github.com/jniltinho/meethub/internal/database/dbtest"
)

func TestParseSearch(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		in    string
		terms []string
		from  []string
		ch    []string
		bad   string // part of the error message; "" = valid
	}{
		{in: "Deploy 17h", terms: []string{"deploy", "17h"}},
		{in: `"release notes" from:carla.pereira`, terms: []string{"release notes"}, from: []string{"carla.pereira"}},
		{in: `  REDIS   in:#devops in:infra from:@ana `, terms: []string{"redis"}, from: []string{"ana"}, ch: []string{"devops", "infra"}},
		{in: `"open quote takes the rest`, terms: []string{"open quote takes the rest"}},
		{in: "100% a_b !x", terms: []string{"100%", "a_b", "!x"}},
		{in: "http://example.com", terms: []string{"http://example.com"}},
		{in: "a", bad: "at least 2"},
		{in: "", bad: "at least 2"},
		{in: "from:ana", bad: "at least 2"},
		{in: `"" x`, bad: "at least 2"},
		{in: "deploy from:", bad: "username"},
		{in: "deploy in:#", bad: "channel name"},
		{in: "w1 w2 w3 w4 w5 w6 w7 w8 w9 w10 w11", bad: "more than 10"},
		{in: strings.Repeat("x", 257), bad: "longer than 256"},
	} {
		q, err := chat.ParseSearch(tc.in)
		if tc.bad != "" {
			var se *chat.SearchError
			if !errors.As(err, &se) || !strings.Contains(se.Msg, tc.bad) || !errors.Is(err, chat.ErrInvalidInput) {
				t.Errorf("%q: error %v, want one about %q", tc.in, err, tc.bad)
			}
			continue
		}
		if err != nil || !slices.Equal(q.Terms, tc.terms) || !slices.Equal(q.From, tc.from) || !slices.Equal(q.In, tc.ch) {
			t.Errorf("%q: %+v %v", tc.in, q, err)
		}
	}
}

// TestSearch covers the message-search spec on the three databases.
func TestSearch(t *testing.T) {
	dbtest.Each(t, func(t *testing.T, db *database.DB) {
		w, ch := postWorld(t, db) // #projects with ana and bob
		priv, _ := w.svc.CreateChannel(ctx, w.ana, ch.TeamID, chat.NewChannel{Name: "finance", DisplayName: "Finance", Type: chat.ChannelPrivate})
		_ = post(t, w, w.ana, priv.ID, "budget approved")
		_ = post(t, w, w.ana, ch.ID, "deploy at 17h")
		_ = post(t, w, w.bob, ch.ID, "deploy tomorrow")
		_ = post(t, w, w.bob, ch.ID, "Redis restarted, 100% healthy")
		_ = post(t, w, w.ana, ch.ID, "the release notes are ready")
		_ = post(t, w, w.ana, ch.ID, "snake_case and 100 percent")
		gone := post(t, w, w.bob, ch.ID, "deploy cancelled")
		if err := w.svc.DeletePost(ctx, w.bob, gone.ID); err != nil {
			t.Fatal(err)
		}
		find := func(sub chat.Subject, q string) []string {
			t.Helper()
			res, err := w.svc.Search(ctx, sub, q, 0, 0)
			if err != nil {
				t.Fatalf("search %q: %v", q, err)
			}
			out := make([]string, len(res.Posts))
			for i, p := range res.Posts {
				out[i] = p.Message
				if c, ok := res.Channels[p.ChannelID]; !ok || c.Name == "" {
					t.Fatalf("search %q: channel of the result missing: %+v", q, res.Channels)
				}
			}
			return out
		}
		for _, tc := range []struct {
			who  chat.Subject
			q    string
			want []string
		}{
			{w.ana, "deploy 17h", []string{"deploy at 17h"}},                             // every term
			{w.ana, "deploy", []string{"deploy tomorrow", "deploy at 17h"}},              // newest first, deleted excluded
			{w.ana, "REDIS", []string{"Redis restarted, 100% healthy"}},                  // case
			{w.ana, `"release notes" from:ana`, []string{"the release notes are ready"}}, // phrase and author
			{w.ana, "deploy from:bob", []string{"deploy tomorrow"}},
			{w.ana, "100%", []string{"Redis restarted, 100% healthy"}}, // % is a character, not a wildcard
			{w.ana, "snake_case", []string{"snake_case and 100 percent"}},
			{w.ana, "snakeXcase", nil}, // _ is not a wildcard either
			{w.ana, "budget", []string{"budget approved"}},
			{w.bob, "budget", nil}, // private channel of others
			{w.ana, "approved in:finance", []string{"budget approved"}},
			{w.ana, "deploy in:finance", nil},
		} {
			if got := find(tc.who, tc.q); !slices.Equal(got, tc.want) {
				t.Errorf("%s searches %q: %v, want %v", tc.who.UserID[:4], tc.q, got, tc.want)
			}
		}
		// a channel of others and an unknown one answer alike; an unknown author too
		for _, q := range []string{"budget in:finance", "budget in:nope", "budget from:nobody"} {
			var se *chat.SearchError
			if _, err := w.svc.Search(ctx, w.bob, q, 0, 0); !errors.As(err, &se) {
				t.Errorf("%q by bob: %v, want a search error", q, err)
			}
		}
		// a guest may not learn that a user they cannot see exists: "from:" answers as for an unknown user
		var hidden *chat.SearchError
		if _, err := w.svc.Search(ctx, w.gu, "deploy from:admin", 0, 0); !errors.As(err, &hidden) || !strings.Contains(hidden.Msg, "no user") {
			t.Errorf("guest searching from: a user they cannot see: %v", err)
		}
		if _, err := w.svc.Search(ctx, w.gu, "deploy from:ana", 0, 0); err != nil {
			t.Errorf("guest searching from: someone of their channels: %v", err)
		}
		// after leaving, and in an archived channel, nothing is found
		if err := w.svc.SetChannelArchived(ctx, w.ana, priv.ID, true); err != nil {
			t.Fatal(err)
		}
		if got := find(w.ana, "budget"); len(got) != 0 {
			t.Errorf("archived channel searched: %v", got)
		}
		if err := w.svc.RemoveChannelMember(ctx, w.bob, ch.ID, w.bob.UserID); err != nil {
			t.Fatal(err)
		}
		if got := find(w.bob, "deploy"); len(got) != 0 {
			t.Errorf("bob left #projects and still finds %v", got)
		}
		// paging: 45 matches, 20 per page
		for i := range 45 {
			_ = post(t, w, w.ana, ch.ID, fmt.Sprintf("paging %02d", i))
		}
		p0, _ := w.svc.Search(ctx, w.ana, "paging", 0, 0)
		p2, _ := w.svc.Search(ctx, w.ana, "paging", 2, 0)
		if len(p0.Posts) != 20 || !p0.HasMore || p0.Posts[0].Message != "paging 44" || len(p2.Posts) != 5 || p2.HasMore || p2.Posts[4].Message != "paging 00" {
			t.Fatalf("paging: %d (more %v) and %d (more %v)", len(p0.Posts), p0.HasMore, len(p2.Posts), p2.HasMore)
		}
		// the limit: the eleventh search in the window is refused with a wait
		w.svc.SearchLimiter = &fakeLimiter{left: 10}
		for i := range 11 {
			_, err := w.svc.Search(ctx, w.ana, "deploy", 0, 0)
			var rl *chat.RateLimitedError
			if limited := errors.As(err, &rl); limited != (i == 10) || (limited && (rl.RetryAfter <= 0 || rl.What() != "searches")) {
				t.Fatalf("search %d: %v", i+1, err)
			}
		}
	})
}
