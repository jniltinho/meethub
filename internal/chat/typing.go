package chat

import (
	"context"
	"fmt"
	"slices"
)

// Typing announces that the caller is typing in a channel or thread to the
// channel's other members, at most once every 3 seconds per user and channel
// (TypingLimiter); throttled announcements are dropped silently.
func (s *Service) Typing(ctx context.Context, sub Subject, channelID, parentID string) error {
	tx := s.DB.Gorm.WithContext(ctx)
	var ch Channel
	if err := tx.Where("id = ?", channelID).Take(&ch).Error; err != nil {
		return ErrNotFound
	}
	ch.trimIDs()
	a, _, err := accessTx(ctx, tx, sub, &ch)
	if err != nil {
		return err
	}
	if !CanPost(sub, a) {
		return ReadError(sub, a)
	}
	if ch.ArchivedAt != 0 {
		return ErrArchived
	}
	if s.TypingLimiter != nil {
		if ok, _ := s.TypingLimiter.Allow(ctx, sub.UserID+":"+channelID); !ok {
			return nil
		}
	}
	members, err := channelMemberIDs(ctx, tx, ch.ID)
	if err != nil {
		return fmt.Errorf("typing: %w", err)
	}
	members = slices.DeleteFunc(members, func(id string) bool { return id == sub.UserID })
	var em emitter
	if ev := em.add("typing", map[string]any{"user_id": sub.UserID, "parent_id": parentID}, members); ev != nil {
		ev.ChannelID, ev.TeamID = ch.ID, ch.TeamID
	}
	s.publish(ctx, em.events)
	return nil
}
