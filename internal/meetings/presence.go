package meetings

import (
	"context"
	"fmt"
	"strings"

	"github.com/livekit/protocol/livekit"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"

	"github.com/jniltinho/meethub/internal/chat"
)

// ParticipantView is a person in a room as the API and events show it.
type ParticipantView struct {
	UserID   string `json:"user_id"`
	JoinedAt int64  `json:"joined_at"` // Unix milliseconds
}

// RoomView is a channel's room with the people in it.
type RoomView struct {
	ChannelID    string            `json:"channel_id"`
	TeamID       string            `json:"team_id"`
	Participants []ParticipantView `json:"participants"`
}

// idLength is the length of a user id (ULID).
const idLength = 26

// LiveKit webhook events this package handles.
const (
	eventJoined   = "participant_joined"
	eventLeft     = "participant_left"
	eventAborted  = "participant_connection_aborted"
	eventFinished = "room_finished"
)

// HandleWebhook applies a verified LiveKit webhook event to the room's presence and tells the channel's members.
// Events about rooms that are not MeetHub's are ignored.
func (s *Service) HandleWebhook(ctx context.Context, ev *livekit.WebhookEvent) error {
	if ev.GetRoom().GetName() == "" {
		return nil
	}
	channelID, err := s.roomByName(ctx, ev.GetRoom().GetName())
	if err != nil || channelID == "" {
		return err
	}
	tx := s.DB.Gorm.WithContext(ctx)
	p := ev.GetParticipant()
	if ev.GetEvent() != eventFinished && len(p.GetIdentity()) != idLength {
		return nil // not a MeetHub user (every token names a user id)
	}
	switch ev.GetEvent() {
	case eventJoined:
		joined := p.GetJoinedAtMs()
		if joined == 0 {
			joined = s.now().UnixMilli()
		}
		err = tx.Clauses(clause.OnConflict{
			Columns:   []clause.Column{{Name: "channel_id"}, {Name: "user_id"}},
			DoUpdates: clause.AssignmentColumns([]string{"sid", "joined_at"}),
		}).Create(&Participant{ChannelID: channelID, UserID: p.GetIdentity(), SID: p.GetSid(), JoinedAt: joined}).Error
	case eventLeft, eventAborted:
		// only the session that left: a replaced session's late "left" must not remove the new one
		err = tx.Where("channel_id = ? AND user_id = ? AND sid = ?", channelID, p.GetIdentity(), p.GetSid()).
			Delete(&Participant{}).Error
	case eventFinished:
		err = tx.Where("channel_id = ?", channelID).Delete(&Participant{}).Error
	default:
		return nil
	}
	if err != nil {
		return fmt.Errorf("meeting presence: %w", err)
	}
	return s.announce(ctx, channelID)
}

// announce sends the room's participants to the channel's members (meeting_updated, a full snapshot).
func (s *Service) announce(ctx context.Context, channelID string) error {
	if s.Publisher == nil {
		return nil
	}
	people, err := s.participants(ctx, s.DB.Gorm.WithContext(ctx), channelID)
	if err != nil {
		return err
	}
	members, err := s.Chat.MemberIDs(ctx, channelID)
	if err != nil {
		return fmt.Errorf("meeting audience: %w", err)
	}
	if len(members) == 0 {
		return nil
	}
	var teamIDs []string
	if err := s.DB.Gorm.WithContext(ctx).Model(&chat.Channel{}).Where("id = ?", channelID).
		Pluck("team_id", &teamIDs).Error; err != nil {
		return fmt.Errorf("meeting channel: %w", err)
	}
	teamID := ""
	if len(teamIDs) > 0 {
		teamID = strings.TrimSpace(teamIDs[0])
	}
	s.Publisher.Publish(context.WithoutCancel(ctx), chat.Event{
		Name:       "meeting_updated",
		Data:       map[string]any{"channel_id": channelID, "participants": people},
		Recipients: members, ChannelID: channelID, TeamID: teamID,
	})
	return nil
}

func (s *Service) participants(ctx context.Context, tx *gorm.DB, channelID string) ([]ParticipantView, error) {
	var rows []Participant
	if err := tx.WithContext(ctx).Where("channel_id = ?", channelID).Order("joined_at, user_id").Find(&rows).Error; err != nil {
		return nil, fmt.Errorf("meeting participants: %w", err)
	}
	out := make([]ParticipantView, len(rows))
	for i, r := range rows {
		out[i] = ParticipantView{UserID: strings.TrimSpace(r.UserID), JoinedAt: r.JoinedAt}
	}
	return out, nil
}

// Room returns a channel's room with the people in it, under the read rule.
func (s *Service) Room(ctx context.Context, sub chat.Subject, channelID string) (*RoomView, error) {
	if !s.Enabled() {
		return nil, ErrDisabled
	}
	ch, err := s.Chat.ReadableChannel(ctx, sub, channelID)
	if err != nil {
		return nil, err
	}
	people, err := s.participants(ctx, s.DB.Gorm, ch.ID)
	if err != nil {
		return nil, err
	}
	return &RoomView{ChannelID: ch.ID, TeamID: ch.TeamID, Participants: people}, nil
}

// listSQL: the people in rooms of channels sub is a member of and, for team channels, whose team they are still
// in (the chat's read rule: members only, in the team).
const listSQL = `SELECT p.channel_id, c.team_id, p.user_id, p.joined_at
FROM meeting_participants p
JOIN channels c ON c.id = p.channel_id
JOIN channel_members me ON me.channel_id = p.channel_id AND me.user_id = ?
WHERE c.team_id = '' OR EXISTS (SELECT 1 FROM team_members tm WHERE tm.team_id = c.team_id AND tm.user_id = ?)
ORDER BY p.channel_id, p.joined_at, p.user_id`

// List returns the rooms with people in them among the channels and conversations sub may read.
func (s *Service) List(ctx context.Context, sub chat.Subject) ([]RoomView, error) {
	out := []RoomView{}
	if !s.Enabled() {
		return out, nil
	}
	var rows []struct {
		ChannelID string
		TeamID    string
		UserID    string
		JoinedAt  int64
	}
	if err := s.DB.Gorm.WithContext(ctx).Raw(listSQL, sub.UserID, sub.UserID).Scan(&rows).Error; err != nil {
		return nil, fmt.Errorf("list meetings: %w", err)
	}
	for _, r := range rows {
		id := strings.TrimSpace(r.ChannelID)
		if len(out) == 0 || out[len(out)-1].ChannelID != id {
			out = append(out, RoomView{ChannelID: id, TeamID: strings.TrimSpace(r.TeamID)})
		}
		last := &out[len(out)-1]
		last.Participants = append(last.Participants, ParticipantView{UserID: strings.TrimSpace(r.UserID), JoinedAt: r.JoinedAt})
	}
	return out, nil
}
