package chat

import (
	"context"
	"fmt"
	"slices"
	"strings"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

// MaxReactionUsers caps the users listed per emoji in a grouped reaction.
const MaxReactionUsers = 25

// Reaction is one user's emoji on a post.
type Reaction struct {
	PostID    string `gorm:"primaryKey;size:26" json:"post_id"`
	UserID    string `gorm:"primaryKey;size:26" json:"user_id"`
	EmojiName string `gorm:"primaryKey;size:64" json:"emoji_name"`
	CreateAt  int64  `gorm:"autoCreateTime:false" json:"create_at"`
}

// ReactionGroup is the reactions of one emoji on a post: the count and up to
// MaxReactionUsers users, oldest first.
type ReactionGroup struct {
	EmojiName string   `json:"emoji_name"`
	Count     int      `json:"count"`
	UserIDs   []string `json:"user_ids"`
}

// ValidEmoji reports whether name is a built-in emoji short name.
func ValidEmoji(name string) bool {
	_, ok := emojiNames[name]
	return ok
}

// AddReaction adds the caller's emoji to a post; adding an existing one changes nothing.
func (s *Service) AddReaction(ctx context.Context, sub Subject, postID, emoji string) error {
	return s.react(ctx, sub, postID, emoji, true)
}

// RemoveReaction removes the caller's emoji from a post.
func (s *Service) RemoveReaction(ctx context.Context, sub Subject, postID, emoji string) error {
	return s.react(ctx, sub, postID, emoji, false)
}

func (s *Service) react(ctx context.Context, sub Subject, postID, emoji string, add bool) error {
	if !ValidEmoji(emoji) {
		return ErrInvalidInput
	}
	var em emitter
	err := s.withPostLock(ctx, sub, postID, func(tx *gorm.DB, ch *Channel, a Access, p *Post) error {
		if !CanPost(sub, a) {
			return ErrForbidden
		}
		if err := writable(ctx, tx, ch); err != nil {
			return err
		}
		now := s.now()
		r := Reaction{PostID: p.ID, UserID: sub.UserID, EmojiName: emoji, CreateAt: now}
		var res *gorm.DB
		if add {
			res = tx.WithContext(ctx).Clauses(clause.OnConflict{DoNothing: true}).Create(&r)
		} else {
			res = tx.WithContext(ctx).Where("post_id = ? AND user_id = ? AND emoji_name = ?", p.ID, sub.UserID, emoji).Delete(&Reaction{})
		}
		if res.Error != nil {
			return fmt.Errorf("reaction: %w", res.Error)
		}
		if res.RowsAffected == 0 {
			return nil // nothing changed: no revision on the post, no event
		}
		if err := tx.WithContext(ctx).Model(&Post{}).Where("id = ?", p.ID).
			Updates(map[string]any{"update_at": now, "revision": ch.Revision}).Error; err != nil {
			return fmt.Errorf("stamp post: %w", err)
		}
		members, err := channelMemberIDs(ctx, tx, ch.ID)
		if err != nil {
			return err
		}
		name := "reaction_added"
		if !add {
			name = "reaction_removed"
		}
		if ev := em.add(name, map[string]any{"reaction": r, "channel_id": ch.ID, "revision": ch.Revision}, members); ev != nil {
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

// reactionsForPosts groups the reactions of several posts by emoji, in the
// order each emoji was first used.
func reactionsForPosts(ctx context.Context, tx *gorm.DB, postIDs []string) (map[string][]ReactionGroup, error) {
	out := map[string][]ReactionGroup{}
	for chunk := range slices.Chunk(postIDs, 500) {
		var rows []Reaction
		if err := tx.WithContext(ctx).Where("post_id IN ?", chunk).Order("create_at, user_id").Find(&rows).Error; err != nil {
			return nil, fmt.Errorf("read reactions: %w", err)
		}
		for _, r := range rows {
			pid := strings.TrimSpace(r.PostID)
			groups := out[pid]
			i := slices.IndexFunc(groups, func(g ReactionGroup) bool { return g.EmojiName == r.EmojiName })
			if i < 0 {
				groups = append(groups, ReactionGroup{EmojiName: r.EmojiName})
				i = len(groups) - 1
			}
			groups[i].Count++
			if len(groups[i].UserIDs) < MaxReactionUsers {
				groups[i].UserIDs = append(groups[i].UserIDs, strings.TrimSpace(r.UserID))
			}
			out[pid] = groups
		}
	}
	return out, nil
}
