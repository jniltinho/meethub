package chat

import (
	"context"
	"fmt"
	"strings"
	"unicode/utf8"

	"github.com/jniltinho/meethub/internal/identity"
)

// Search limits (message-search spec).
const (
	SearchMaxRunes      = 256
	SearchMaxTerms      = 10
	SearchDefaultPage   = 20
	SearchMaxPage       = 100
	searchMinTermLength = 2
)

// SearchError is an invalid search; its message says which part is wrong.
type SearchError struct{ Msg string }

func (e *SearchError) Error() string { return e.Msg }

// Is makes a SearchError an ErrInvalidInput for errors.Is.
func (e *SearchError) Is(target error) bool { return target == ErrInvalidInput }

// SearchQuery is a parsed search: every term must be in the message; From
// and In are alternatives among themselves (Mattermost's modifiers).
type SearchQuery struct {
	Terms []string // lower-cased words and quoted phrases
	From  []string // usernames, without @
	In    []string // channel names, without # or ~
}

// ParseSearch splits a search into words, "quoted phrases", from:<username>
// and in:<channel name>.
func ParseSearch(s string) (SearchQuery, error) {
	var q SearchQuery
	if utf8.RuneCountInString(s) > SearchMaxRunes {
		return q, &SearchError{fmt.Sprintf("the search is longer than %d characters", SearchMaxRunes)}
	}
	long := false
	add := func(term string) {
		if term = strings.ToLower(strings.TrimSpace(term)); term == "" {
			return
		}
		q.Terms = append(q.Terms, term)
		long = long || utf8.RuneCountInString(term) >= searchMinTermLength
	}
	for s = strings.TrimSpace(s); s != ""; s = strings.TrimSpace(s) {
		if s[0] == '"' {
			phrase, rest, closed := strings.Cut(s[1:], `"`)
			if !closed {
				rest = "" // an open quote takes the rest of the search
			}
			add(phrase)
			s = rest
			continue
		}
		word, rest, _ := strings.Cut(s, " ")
		s = rest
		switch name, value, ok := strings.Cut(word, ":"); {
		case ok && strings.EqualFold(name, "from"):
			if value = strings.TrimPrefix(value, "@"); value == "" {
				return q, &SearchError{"from: needs a username"}
			}
			q.From = append(q.From, value)
		case ok && strings.EqualFold(name, "in"):
			if value = strings.TrimLeft(value, "#~"); value == "" {
				return q, &SearchError{"in: needs a channel name"}
			}
			q.In = append(q.In, strings.ToLower(value))
		default:
			add(word)
		}
	}
	switch {
	case !long:
		return q, &SearchError{fmt.Sprintf("the search needs a word of at least %d characters", searchMinTermLength)}
	case len(q.Terms) > SearchMaxTerms:
		return q, &SearchError{fmt.Sprintf("the search has more than %d words", SearchMaxTerms)}
	}
	return q, nil
}

// likePattern is a LIKE pattern for "contains term", with ! as the escape
// character (the same on the three databases).
func likePattern(term string) string {
	return "%" + strings.NewReplacer("!", "!!", "%", "!%", "_", "!_").Replace(term) + "%"
}

// SearchChannel names a channel of a result page.
type SearchChannel struct {
	ID          string `json:"id"`
	Type        string `json:"type"`
	Name        string `json:"name"`
	DisplayName string `json:"display_name"`
	TeamID      string `json:"team_id"`
}

// SearchResult is a page of messages, newest first.
type SearchResult struct {
	Posts    []PostView               `json:"posts"`
	HasMore  bool                     `json:"has_more"`
	Channels map[string]SearchChannel `json:"channels"`
}

// Search finds messages by text in the channels and conversations the caller
// belongs to now (never deleted messages or archived channels).
func (s *Service) Search(ctx context.Context, sub Subject, terms string, page, perPage int) (*SearchResult, error) {
	q, err := ParseSearch(terms)
	if err != nil {
		return nil, err
	}
	if page < 0 {
		return nil, &SearchError{"the page cannot be negative"}
	}
	if perPage <= 0 {
		perPage = SearchDefaultPage
	}
	perPage = min(perPage, SearchMaxPage)
	if s.SearchLimiter != nil {
		if ok, wait := s.SearchLimiter.Allow(ctx, sub.UserID); !ok {
			return nil, &RateLimitedError{RetryAfter: wait, Action: "searches"}
		}
	}
	tx := s.DB.Gorm.WithContext(ctx)
	// shortcut: substring matching with LIKE scans the posts of the caller's channels (and SQLite's LOWER only
	// folds ASCII); replace the WHERE with native full-text (tsvector, FULLTEXT, FTS5) when histories outgrow it.
	// The join is the authorization: only rows of channels the caller is a member of exist for this query.
	sel := tx.Table("posts p").Select("p.*").
		Joins("JOIN channel_members m ON m.channel_id = p.channel_id AND m.user_id = ?", sub.UserID).
		Joins("JOIN channels c ON c.id = p.channel_id").
		Joins("LEFT JOIN teams t ON t.id = c.team_id").
		Where("p.delete_at = 0 AND c.archived_at = 0 AND (t.id IS NULL OR t.archived_at = 0)")
	for _, term := range q.Terms {
		sel = sel.Where("LOWER(p.message) LIKE ? ESCAPE '!'", likePattern(term))
	}
	if len(q.From) > 0 {
		authors := make([]string, 0, len(q.From))
		for _, name := range q.From {
			var ids []string
			if norm, err := identity.NormalizeUsername(name); err == nil {
				if err := tx.Model(&identity.User{}).Where("username_normalized = ?", norm).Pluck("id", &ids).Error; err != nil {
					return nil, fmt.Errorf("search author: %w", err)
				}
			}
			// a user the caller may not see (a guest's strangers) answers like one that does not exist
			if visible, err := s.UsersByIDs(ctx, sub, trimAll(ids)); err != nil {
				return nil, err
			} else if len(visible) == 0 {
				return nil, &SearchError{"from: there is no user " + name}
			}
			authors = append(authors, trimAll(ids)...)
		}
		sel = sel.Where("p.user_id IN ?", authors)
	}
	if len(q.In) > 0 {
		// the same answer for a channel that does not exist and one the caller is not a member of
		var mine []Channel
		if err := tx.Table("channels c").Select("c.id, c.name").
			Joins("JOIN channel_members m ON m.channel_id = c.id AND m.user_id = ?", sub.UserID).
			Where("c.name IN ?", q.In).Scan(&mine).Error; err != nil {
			return nil, fmt.Errorf("search channel: %w", err)
		}
		found, in := map[string]bool{}, make([]string, len(mine))
		for i, c := range mine {
			found[c.Name], in[i] = true, c.ID
		}
		for _, n := range q.In {
			if !found[n] {
				return nil, &SearchError{"in: there is no channel " + n}
			}
		}
		sel = sel.Where("p.channel_id IN ?", trimAll(in))
	}
	var posts []Post
	if err := sel.Order("p.create_at DESC, p.id DESC").Limit(perPage + 1).Offset(page * perPage).Scan(&posts).Error; err != nil {
		return nil, fmt.Errorf("search: %w", err)
	}
	out := &SearchResult{Channels: map[string]SearchChannel{}}
	if out.HasMore = len(posts) > perPage; out.HasMore {
		posts = posts[:perPage]
	}
	if out.Posts, err = views(ctx, tx, posts); err != nil {
		return nil, err
	}
	ids := make([]string, 0, len(posts))
	for _, p := range out.Posts {
		ids = append(ids, p.ChannelID)
	}
	if len(ids) > 0 {
		var chs []Channel
		if err := tx.Where("id IN ?", ids).Find(&chs).Error; err != nil {
			return nil, fmt.Errorf("search channels: %w", err)
		}
		for i := range chs {
			chs[i].trimIDs()
			c := chs[i]
			out.Channels[c.ID] = SearchChannel{ID: c.ID, Type: c.Type, Name: c.Name, DisplayName: c.DisplayName, TeamID: c.TeamID}
		}
	}
	if out.Posts == nil {
		out.Posts = []PostView{}
	}
	return out, nil
}
