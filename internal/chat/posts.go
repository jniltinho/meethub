package chat

import (
	"context"
	"errors"
	"fmt"
	"regexp"
	"strings"
	"time"
	"unicode/utf8"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"

	"github.com/jniltinho/meethub/internal/audit"
	"github.com/jniltinho/meethub/internal/config"
	"github.com/jniltinho/meethub/internal/database"
)

// Post errors.
var (
	ErrInvalidInput        = errors.New("invalid input")
	ErrPendingPostConflict = errors.New("pending_post_id already used for a different message")
)

// RateLimitedError is returned when a user posts (or searches) too fast.
type RateLimitedError struct {
	RetryAfter time.Duration
	Action     string // what was limited, plural: "messages" (the default) or "searches"
}

// What names the limited action.
func (e *RateLimitedError) What() string {
	if e.Action == "" {
		return "messages"
	}
	return e.Action
}

func (e *RateLimitedError) Error() string {
	return fmt.Sprintf("too many %s, retry after %s", e.What(), e.RetryAfter.Round(time.Second))
}

// Limiter is the message rate limit (a sliding window shared through Redis).
type Limiter interface {
	Allow(ctx context.Context, key string) (bool, time.Duration)
}

var pendingPostIDRe = regexp.MustCompile(`^[A-Za-z0-9:_-]{1,64}$`)

// Post is a stored message (Mattermost post). Message is never exposed for a
// deleted post: user-facing reads go through View.
type Post struct {
	ID            string  `gorm:"primaryKey;size:26"`
	ChannelID     string  `gorm:"size:26"`
	UserID        string  `gorm:"size:26"`
	RootID        string  `gorm:"size:26"`
	Message       string  // kept in storage after deletion; users read it only through View
	Type          string  `gorm:"size:32"`
	PendingPostID *string `gorm:"size:64"`
	CreateAt      int64   `gorm:"autoCreateTime:false"`
	UpdateAt      int64   `gorm:"autoUpdateTime:false"`
	EditAt        int64
	DeleteAt      int64
	ReplyCount    int64
	LastReplyAt   int64
	Revision      int64
	PinnedAt      int64   // 0: not pinned
	PinnedBy      *string `gorm:"size:26"`
}

func (p *Post) trimIDs() {
	p.ID, p.ChannelID, p.UserID = strings.TrimSpace(p.ID), strings.TrimSpace(p.ChannelID), strings.TrimSpace(p.UserID)
	p.RootID = strings.TrimSpace(p.RootID)
}

// PostView is the API and event projection of a post: a deleted post is a
// tombstone without text or reactions.
type PostView struct {
	ID            string          `json:"id"`
	ChannelID     string          `json:"channel_id"`
	UserID        string          `json:"user_id"`
	RootID        string          `json:"root_id"`
	Message       string          `json:"message,omitempty"`
	Type          string          `json:"type"`
	PendingPostID string          `json:"pending_post_id,omitempty"`
	CreateAt      int64           `json:"create_at"`
	UpdateAt      int64           `json:"update_at"`
	EditAt        int64           `json:"edit_at"`
	DeleteAt      int64           `json:"delete_at"`
	ReplyCount    int64           `json:"reply_count"`
	LastReplyAt   int64           `json:"last_reply_at"`
	Revision      int64           `json:"revision"`
	IsPinned      bool            `json:"is_pinned"`
	PinnedBy      string          `json:"pinned_by,omitempty"` // MeetHub extension: Mattermost does not say who pinned
	PinnedAt      int64           `json:"pinned_at,omitempty"`
	Reactions     []ReactionGroup `json:"reactions,omitempty"`
}

// View projects the post for users.
func (p *Post) View() PostView {
	v := PostView{
		ID: p.ID, ChannelID: p.ChannelID, UserID: p.UserID, RootID: p.RootID, Type: p.Type,
		CreateAt: p.CreateAt, UpdateAt: p.UpdateAt, EditAt: p.EditAt, DeleteAt: p.DeleteAt,
		ReplyCount: p.ReplyCount, LastReplyAt: p.LastReplyAt, Revision: p.Revision,
	}
	if p.DeleteAt == 0 {
		v.Message = p.Message
		if p.PendingPostID != nil {
			v.PendingPostID = *p.PendingPostID
		}
		if p.PinnedAt != 0 {
			v.IsPinned, v.PinnedAt = true, p.PinnedAt
			if p.PinnedBy != nil {
				v.PinnedBy = strings.TrimSpace(*p.PinnedBy)
			}
		}
	}
	return v
}

// NewPost is the input of CreatePost.
type NewPost struct {
	ChannelID     string
	RootID        string
	Message       string
	PendingPostID string
}

func checkMessage(s string) (string, error) {
	s = strings.TrimSpace(s)
	if s == "" {
		return "", ErrInvalidInput
	}
	if utf8.RuneCountInString(s) > PostMessageMaxRunes {
		return "", ErrTooLong
	}
	return s, nil
}

// CreatePost stores a message and then announces it (design D3). A repeat of
// the same pending_post_id by the same author returns the stored message
// without a new event (409 when channel, root or text differ); repeats are
// checked before the rate limit, so they never count.
func (s *Service) CreatePost(ctx context.Context, sub Subject, in NewPost) (*PostView, error) {
	msg, err := checkMessage(in.Message)
	if err != nil {
		return nil, err
	}
	if !pendingPostIDRe.MatchString(in.PendingPostID) {
		return nil, ErrInvalidInput
	}
	in.Message = msg
	if v, err := s.repeatedPost(ctx, s.DB.Gorm.WithContext(ctx), sub, in, false); v != nil || err != nil {
		return v, err
	}
	if s.Limiter != nil {
		if ok, wait := s.Limiter.Allow(ctx, sub.UserID); !ok {
			return nil, &RateLimitedError{RetryAfter: wait}
		}
	}
	var (
		em  emitter
		out *PostView
	)
	err = withChannelLock(ctx, s.DB, in.ChannelID, func(tx *gorm.DB, ch *Channel) error {
		a, _, err := accessTx(ctx, tx, sub, ch)
		if err != nil {
			return err
		}
		if !CanPost(sub, a) {
			return ReadError(sub, a)
		}
		if err := writable(ctx, tx, ch); err != nil {
			return err
		}
		// a concurrent identical post on this channel committed while we waited for the lock
		if v, err := s.repeatedPost(ctx, tx, sub, in, false); v != nil || err != nil {
			out = v
			return err
		}
		v, err := s.insertPost(ctx, tx, sub, ch, in, &em)
		out = v
		return err
	})
	if err != nil {
		return nil, err
	}
	s.publish(ctx, em.events)
	return out, nil
}

// repeatedPost returns the stored post of (author, pending_post_id) when the
// request repeats it, after re-checking read access; 409 when it differs.
// latest uses a locking read (the row a concurrent transaction just committed).
func (s *Service) repeatedPost(ctx context.Context, tx *gorm.DB, sub Subject, in NewPost, latest bool) (*PostView, error) {
	q := tx.WithContext(ctx).Where("user_id = ? AND pending_post_id = ?", sub.UserID, in.PendingPostID)
	if latest && s.DB.Dialect != config.DriverSQLite {
		q = q.Clauses(clause.Locking{Strength: "UPDATE"})
	}
	var p Post
	if err := q.Take(&p).Error; err != nil {
		if database.IsNotFound(err) {
			return nil, nil
		}
		return nil, fmt.Errorf("read pending post: %w", err)
	}
	p.trimIDs()
	var ch Channel
	if err := tx.WithContext(ctx).Where("id = ?", p.ChannelID).Take(&ch).Error; err != nil {
		return nil, fmt.Errorf("read channel: %w", err)
	}
	ch.trimIDs()
	a, _, err := accessTx(ctx, tx, sub, &ch)
	if err != nil {
		return nil, err
	}
	if !CanRead(sub, a) {
		return nil, ReadError(sub, a)
	}
	if p.ChannelID != in.ChannelID || p.Message != in.Message || p.DeleteAt != 0 || !s.sameRoot(ctx, tx, p.RootID, in.RootID) {
		return nil, ErrPendingPostConflict
	}
	v := p.View()
	return &v, nil
}

// sameRoot reports whether a requested root (possibly a reply, which attaches
// to its root) is the stored root.
func (s *Service) sameRoot(ctx context.Context, tx *gorm.DB, stored, requested string) bool {
	if stored == requested {
		return true
	}
	if requested == "" {
		return false
	}
	var roots []string
	tx.WithContext(ctx).Model(&Post{}).Where("id = ?", requested).Pluck("root_id", &roots)
	return len(roots) == 1 && strings.TrimSpace(roots[0]) == stored
}

// insertPost writes the post and every counter under the channel lock.
func (s *Service) insertPost(ctx context.Context, tx *gorm.DB, sub Subject, ch *Channel, in NewPost, em *emitter) (*PostView, error) {
	now := s.now()
	var root *Post
	if in.RootID != "" {
		r, err := threadRoot(ctx, tx, ch.ID, in.RootID)
		if err != nil {
			return nil, err
		}
		root = r
	}
	key := in.PendingPostID
	p := &Post{
		ID: newID(), ChannelID: ch.ID, UserID: sub.UserID, Message: in.Message, PendingPostID: &key,
		CreateAt: now, UpdateAt: now, Revision: ch.Revision,
	}
	if root != nil {
		p.RootID = root.ID
	}
	res := tx.WithContext(ctx).Clauses(clause.OnConflict{DoNothing: true}).Create(p)
	if res.Error != nil {
		return nil, fmt.Errorf("create post: %w", res.Error)
	}
	if res.RowsAffected == 0 { // the same key committed concurrently (through another channel's lock)
		v, err := s.repeatedPost(ctx, tx, sub, in, true)
		if err == nil && v == nil {
			err = fmt.Errorf("create post: conflict without a row")
		}
		return v, err
	}
	if err := tx.WithContext(ctx).Exec(`UPDATE channels SET total_msg_count = total_msg_count + 1, last_post_at = ? WHERE id = ?`,
		now, ch.ID).Error; err != nil {
		return nil, fmt.Errorf("count post: %w", err)
	}
	if root != nil {
		if err := tx.WithContext(ctx).Exec(`UPDATE posts SET reply_count = reply_count + 1, last_reply_at = ?, update_at = ?, revision = ? WHERE id = ?`,
			now, now, ch.Revision, root.ID).Error; err != nil {
			return nil, fmt.Errorf("count reply: %w", err)
		}
	}
	// posting marks the channel read for its author (Mattermost behavior)
	if err := tx.WithContext(ctx).Exec(`UPDATE channel_members SET msg_count = ?, mention_count = 0, last_viewed_at = ? WHERE channel_id = ? AND user_id = ?`,
		ch.TotalMsgCount+1, now, ch.ID, sub.UserID).Error; err != nil {
		return nil, fmt.Errorf("mark read: %w", err)
	}
	mentioned, err := s.applyMentions(ctx, tx, sub, ch, p.Message)
	if err != nil {
		return nil, err
	}
	revealed, err := revealConversationTx(ctx, tx, ch)
	if err != nil {
		return nil, err
	}
	members, err := channelMemberIDs(ctx, tx, ch.ID)
	if err != nil {
		return nil, err
	}
	for _, u := range revealed {
		name := "direct_added"
		if ch.Type == ChannelGroup {
			name = "group_added"
		}
		em.add(name, map[string]any{"channel_id": ch.ID, "teammate_id": sub.UserID}, []string{u})
	}
	v := p.View()
	if ev := em.add("posted", map[string]any{
		"post": v, "channel_type": ch.Type, "channel_name": ch.Name, "team_id": ch.TeamID, "mentions": mentioned,
	}, members); ev != nil {
		ev.ChannelID, ev.TeamID = ch.ID, ch.TeamID
	}
	return &v, nil
}

// threadRoot resolves the root of a reply: a reply to a reply attaches to its
// root; a missing, deleted or foreign root is invalid input.
func threadRoot(ctx context.Context, tx *gorm.DB, channelID, id string) (*Post, error) {
	var p Post
	if err := tx.WithContext(ctx).Where("id = ?", id).Take(&p).Error; err != nil {
		if database.IsNotFound(err) {
			return nil, ErrInvalidInput
		}
		return nil, fmt.Errorf("read root: %w", err)
	}
	p.trimIDs()
	if p.RootID != "" {
		return threadRoot(ctx, tx, channelID, p.RootID)
	}
	if p.ChannelID != channelID || p.DeleteAt != 0 {
		return nil, ErrInvalidInput
	}
	return &p, nil
}

// lockedPost reads a post of the locked channel.
func lockedPost(ctx context.Context, tx *gorm.DB, channelID, postID string) (*Post, error) {
	var p Post
	if err := tx.WithContext(ctx).Where("id = ? AND channel_id = ?", postID, channelID).Take(&p).Error; err != nil {
		if database.IsNotFound(err) {
			return nil, ErrNotFound
		}
		return nil, fmt.Errorf("read post: %w", err)
	}
	p.trimIDs()
	return &p, nil
}

// postChannel returns the channel of a post (to take its lock).
func (s *Service) postChannel(ctx context.Context, postID string) (string, error) {
	var ids []string
	if err := s.DB.Gorm.WithContext(ctx).Model(&Post{}).Where("id = ?", postID).Pluck("channel_id", &ids).Error; err != nil {
		return "", fmt.Errorf("read post: %w", err)
	}
	if len(ids) == 0 {
		return "", ErrNotFound
	}
	return strings.TrimSpace(ids[0]), nil
}

// withPostLock locks the post's channel and checks the caller may read it.
func (s *Service) withPostLock(ctx context.Context, sub Subject, postID string, fn func(tx *gorm.DB, ch *Channel, a Access, p *Post) error) error {
	channelID, err := s.postChannel(ctx, postID)
	if err != nil {
		return err
	}
	return withChannelLock(ctx, s.DB, channelID, func(tx *gorm.DB, ch *Channel) error {
		a, _, err := accessTx(ctx, tx, sub, ch)
		if err != nil {
			return err
		}
		if !CanRead(sub, a) {
			return ReadError(sub, a)
		}
		p, err := lockedPost(ctx, tx, ch.ID, postID)
		if err != nil {
			return err
		}
		if p.DeleteAt != 0 {
			return ErrNotFound
		}
		return fn(tx, ch, a, p)
	})
}

// EditPost changes the text of the caller's own message.
func (s *Service) EditPost(ctx context.Context, sub Subject, postID, message string) (*PostView, error) {
	msg, err := checkMessage(message)
	if err != nil {
		return nil, err
	}
	var (
		em  emitter
		out *PostView
	)
	err = s.withPostLock(ctx, sub, postID, func(tx *gorm.DB, ch *Channel, _ Access, p *Post) error {
		if p.UserID != sub.UserID {
			return ErrForbidden
		}
		if err := writable(ctx, tx, ch); err != nil {
			return err
		}
		now := s.now()
		p.Message, p.EditAt, p.UpdateAt, p.Revision = msg, now, now, ch.Revision
		if err := tx.WithContext(ctx).Model(&Post{}).Where("id = ?", p.ID).Updates(map[string]any{
			"message": msg, "edit_at": now, "update_at": now, "revision": ch.Revision,
		}).Error; err != nil {
			return fmt.Errorf("edit post: %w", err)
		}
		members, err := channelMemberIDs(ctx, tx, ch.ID)
		if err != nil {
			return err
		}
		v := p.View()
		if v.Reactions, err = reactionsFor(ctx, tx, p.ID); err != nil {
			return err
		}
		if ev := em.add("post_edited", map[string]any{"post": v}, members); ev != nil {
			ev.ChannelID, ev.TeamID = ch.ID, ch.TeamID
		}
		out = &v
		return nil
	})
	if err != nil {
		return nil, err
	}
	s.publish(ctx, em.events)
	return out, nil
}

// DeletePost soft-deletes a message: its author or a channel administrator
// (audited as moderation). Deleting a root deletes its replies with one event;
// deleting a reply updates the root's counters. Unread counts do not change.
func (s *Service) DeletePost(ctx context.Context, sub Subject, postID string) error {
	var em emitter
	err := s.withPostLock(ctx, sub, postID, func(tx *gorm.DB, ch *Channel, a Access, p *Post) error {
		moderator := p.UserID != sub.UserID
		if moderator && !CanAdminister(sub, a) {
			return ErrForbidden
		}
		now := s.now()
		ids := []string{p.ID}
		if p.RootID == "" {
			var replies []string
			if err := tx.WithContext(ctx).Model(&Post{}).Where("root_id = ? AND delete_at = 0", p.ID).Pluck("id", &replies).Error; err != nil {
				return err
			}
			ids = append(ids, trimAll(replies)...)
		}
		if err := tx.WithContext(ctx).Model(&Post{}).Where("id IN ?", ids).
			Updates(map[string]any{"delete_at": now, "update_at": now, "revision": ch.Revision, "pinned_at": 0, "pinned_by": nil}).Error; err != nil {
			return fmt.Errorf("delete post: %w", err)
		}
		if err := tx.WithContext(ctx).Where("post_id IN ?", ids).Delete(&Reaction{}).Error; err != nil {
			return fmt.Errorf("delete reactions: %w", err)
		}
		if p.RootID != "" {
			if err := recountReplies(ctx, tx, p.RootID, now, ch.Revision); err != nil {
				return err
			}
		}
		if moderator {
			if err := audit.Record(ctx, tx, auditEvent(sub, audit.ActionPostModerated, "post", p.ID,
				map[string]any{"channel_id": ch.ID, "author_id": p.UserID})); err != nil {
				return err
			}
		}
		p.DeleteAt, p.UpdateAt, p.Revision, p.PinnedAt, p.PinnedBy = now, now, ch.Revision, 0, nil
		members, err := channelMemberIDs(ctx, tx, ch.ID)
		if err != nil {
			return err
		}
		data := map[string]any{"post": p.View()}
		if p.RootID == "" && len(ids) > 1 {
			data["delete_thread"] = true
		}
		if ev := em.add("post_deleted", data, members); ev != nil {
			ev.ChannelID, ev.TeamID = ch.ID, ch.TeamID
		}
		return nil
	})
	if err != nil {
		return err
	}
	s.publish(ctx, em.events)
	return nil
}

// recountReplies recomputes a root's reply count and last reply time from its
// remaining replies and stamps the new revision.
func recountReplies(ctx context.Context, tx *gorm.DB, rootID string, now, revision int64) error {
	var agg struct {
		N    int64
		Last int64
	}
	if err := tx.WithContext(ctx).Model(&Post{}).Select("COUNT(*) AS n, COALESCE(MAX(create_at), 0) AS last").
		Where("root_id = ? AND delete_at = 0", rootID).Scan(&agg).Error; err != nil {
		return fmt.Errorf("recount replies: %w", err)
	}
	return tx.WithContext(ctx).Model(&Post{}).Where("id = ?", rootID).Updates(map[string]any{
		"reply_count": agg.N, "last_reply_at": agg.Last, "update_at": now, "revision": revision,
	}).Error
}
