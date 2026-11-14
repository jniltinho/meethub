package chat

import (
	"context"
	"fmt"
	"slices"

	"gorm.io/gorm"
)

// queueEmail queues a new message for notification e-mails (notifications spec,
// design D2) inside the posting transaction: in a direct or group conversation
// for its other members, in a channel for the members mentioned by name; not
// for people whose e-mail preference is "none". The notify task decides later
// whether to send (still unread after 10 minutes, the person offline).
func (s *Service) queueEmail(ctx context.Context, tx *gorm.DB, p *Post, ch *Channel, mentioned, named []string) error {
	if !s.EmailNotifications {
		return nil
	}
	to := named
	if ch.Type == ChannelDirect || ch.Type == ChannelGroup {
		to = mentioned // every other member
	}
	to = slices.DeleteFunc(slices.Clone(to), func(id string) bool { return id == p.UserID })
	for chunk := range slices.Chunk(to, 500) {
		if err := tx.WithContext(ctx).Exec(`INSERT INTO notification_queue (user_id, post_id, create_at)
			SELECT u.id, ?, ? FROM users u LEFT JOIN user_preferences up ON up.user_id = u.id
			WHERE u.id IN ? AND COALESCE(up.email_notifications, '') <> 'none'`, p.ID, p.CreateAt, chunk).Error; err != nil {
			return fmt.Errorf("queue notification: %w", err)
		}
	}
	return nil
}
