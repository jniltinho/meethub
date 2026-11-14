package chat

import (
	"context"
	"fmt"
	"strings"

	"gorm.io/gorm"
)

// PinPost pins a message for every member of its channel (Mattermost pinned
// posts). Pinning a pinned message changes nothing and keeps the first pin's
// author and time.
func (s *Service) PinPost(ctx context.Context, sub Subject, postID string) (*PostView, error) {
	return s.setPinned(ctx, sub, postID, true)
}

// UnpinPost clears a message's pin.
func (s *Service) UnpinPost(ctx context.Context, sub Subject, postID string) (*PostView, error) {
	return s.setPinned(ctx, sub, postID, false)
}

// setPinned runs under the channel lock: a change stamps the channel's new
// revision on the message and is announced as post_edited, as Mattermost
// does, so the clients' revision rule and the catch-up cover it. edit_at is
// not touched: a pinned message is not an edited one.
func (s *Service) setPinned(ctx context.Context, sub Subject, postID string, pin bool) (*PostView, error) {
	var (
		em  emitter
		out *PostView
	)
	err := s.withPostLock(ctx, sub, postID, func(tx *gorm.DB, ch *Channel, a Access, p *Post) error {
		if !CanPost(sub, a) {
			return ErrForbidden
		}
		if err := writable(ctx, tx, ch); err != nil {
			return err
		}
		changed := (p.PinnedAt != 0) != pin
		if changed {
			now := s.now()
			p.PinnedAt, p.PinnedBy, p.UpdateAt, p.Revision = 0, nil, now, ch.Revision
			if pin {
				by := sub.UserID
				p.PinnedAt, p.PinnedBy = now, &by
			}
			if err := tx.WithContext(ctx).Model(&Post{}).Where("id = ?", p.ID).Updates(map[string]any{
				"pinned_at": p.PinnedAt, "pinned_by": p.PinnedBy, "update_at": now, "revision": ch.Revision,
			}).Error; err != nil {
				return fmt.Errorf("pin post: %w", err)
			}
		}
		v := p.View()
		var err error
		if v.Reactions, err = reactionsFor(ctx, tx, p.ID); err != nil {
			return err
		}
		out = &v
		if !changed {
			return nil
		}
		members, err := channelMemberIDs(ctx, tx, ch.ID)
		if err != nil {
			return err
		}
		if ev := em.add("post_edited", map[string]any{"post": v}, members); ev != nil {
			ev.ChannelID, ev.TeamID = ch.ID, ch.TeamID
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	s.publish(ctx, em.events)
	return out, nil
}

// PinnedPosts pages a channel's pinned messages, most recently pinned first,
// by an opaque cursor over (pinned_at, id); next is "" on the last page.
func (s *Service) PinnedPosts(ctx context.Context, sub Subject, channelID, cursor string, limit int) ([]PostView, string, error) {
	limit = perPage(limit)
	tx := s.DB.Gorm.WithContext(ctx)
	if _, err := s.readableChannel(ctx, tx, sub, channelID); err != nil {
		return nil, "", err
	}
	q := tx.Where("channel_id = ? AND pinned_at > 0 AND delete_at = 0", channelID)
	if cursor != "" {
		at, id, err := parseCursor(cursor)
		if err != nil {
			return nil, "", err
		}
		q = q.Where("(pinned_at < ? OR (pinned_at = ? AND id < ?))", at, at, id)
	}
	var posts []Post
	if err := q.Order("pinned_at DESC, id DESC").Limit(limit + 1).Find(&posts).Error; err != nil {
		return nil, "", fmt.Errorf("pinned posts: %w", err)
	}
	next := ""
	if len(posts) > limit {
		posts = posts[:limit]
		last := posts[limit-1]
		next = fmt.Sprintf("%d:%s", last.PinnedAt, strings.TrimSpace(last.ID))
	}
	out, err := views(ctx, tx, posts)
	return out, next, err
}
