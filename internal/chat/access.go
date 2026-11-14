package chat

import "context"

// ReadableChannel returns the channel when sub may read it (CanRead); otherwise ReadError's 404 or 403. Other
// packages (meetings) authorize through it so the read rule stays in one place.
func (s *Service) ReadableChannel(ctx context.Context, sub Subject, channelID string) (*Channel, error) {
	return s.readableChannel(ctx, s.DB.Gorm.WithContext(ctx), sub, channelID)
}

// MemberIDs lists the user ids of a channel's members (the audience of channel-scoped events).
func (s *Service) MemberIDs(ctx context.Context, channelID string) ([]string, error) {
	return channelMemberIDs(ctx, s.DB.Gorm, channelID)
}
