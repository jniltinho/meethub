package meetings

import (
	"context"
	"errors"
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

// RoomView is a channel's room with the people in it. Revision grows with every change of the room's presence:
// clients keep the highest they have seen and ignore older snapshots.
type RoomView struct {
	ChannelID    string            `json:"channel_id"`
	TeamID       string            `json:"team_id"`
	Revision     int64             `json:"revision"`
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
// Events about rooms that are not MeetHub's, or participants that are not MeetHub users, are ignored.
func (s *Service) HandleWebhook(ctx context.Context, ev *livekit.WebhookEvent) error {
	if ev.GetRoom().GetName() == "" {
		return nil
	}
	channelID, err := s.roomByName(ctx, ev.GetRoom().GetName())
	if err != nil || channelID == "" {
		return err
	}
	p := ev.GetParticipant()
	if ev.GetEvent() != eventFinished && len(p.GetIdentity()) != idLength {
		return nil // every token names a user id
	}
	var fn func(tx *gorm.DB) (bool, error)
	switch ev.GetEvent() {
	case eventJoined:
		joined := p.GetJoinedAtMs()
		if joined == 0 {
			joined = s.now().UnixMilli()
		}
		fn = func(tx *gorm.DB) (bool, error) {
			err := tx.Clauses(clause.OnConflict{
				Columns:   []clause.Column{{Name: "channel_id"}, {Name: "user_id"}},
				DoUpdates: clause.AssignmentColumns([]string{"sid", "joined_at"}),
			}).Create(&Participant{ChannelID: channelID, UserID: p.GetIdentity(), SID: p.GetSid(), JoinedAt: joined}).Error
			return true, err
		}
	case eventLeft, eventAborted:
		// only the session that left: a replaced session's late "left" must not remove the new one
		fn = func(tx *gorm.DB) (bool, error) {
			res := tx.Where("channel_id = ? AND user_id = ? AND sid = ?", channelID, p.GetIdentity(), p.GetSid()).
				Delete(&Participant{})
			return res.RowsAffected > 0, res.Error
		}
	case eventFinished:
		fn = func(tx *gorm.DB) (bool, error) {
			res := tx.Where("channel_id = ?", channelID).Delete(&Participant{})
			return res.RowsAffected > 0, res.Error
		}
	default:
		return nil
	}
	return s.change(ctx, channelID, fn)
}

// errUnchanged rolls back a change that changed nothing (no revision bump, no event).
var errUnchanged = errors.New("unchanged")

// change applies fn to a room's presence under the room's row lock, bumping its revision, and announces the
// resulting snapshot after the commit. Snapshots of one room are thereby ordered by revision even when replicas
// publish them out of order. fn reports whether it changed anything.
func (s *Service) change(ctx context.Context, channelID string, fn func(tx *gorm.DB) (bool, error)) error {
	var view RoomView
	err := s.DB.Gorm.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		res := tx.Exec(`UPDATE meeting_rooms SET revision = revision + 1 WHERE channel_id = ?`, channelID)
		if res.Error != nil {
			return fmt.Errorf("lock meeting room: %w", res.Error)
		}
		if res.RowsAffected == 0 {
			return errUnchanged
		}
		changed, err := fn(tx)
		if err != nil {
			return fmt.Errorf("meeting presence: %w", err)
		}
		if !changed {
			return errUnchanged
		}
		v, err := s.view(ctx, tx, channelID)
		if err != nil {
			return err
		}
		view = *v
		return nil
	})
	if errors.Is(err, errUnchanged) {
		return nil
	}
	if err != nil {
		return err
	}
	return s.announce(ctx, &view)
}

// view reads a room's revision and participants (revision 0 and nobody when the room was never used).
func (s *Service) view(ctx context.Context, tx *gorm.DB, channelID string) (*RoomView, error) {
	var revs []int64
	if err := tx.WithContext(ctx).Model(&Room{}).Where("channel_id = ?", channelID).Limit(1).
		Pluck("revision", &revs).Error; err != nil {
		return nil, fmt.Errorf("meeting room: %w", err)
	}
	var rows []Participant
	if err := tx.WithContext(ctx).Where("channel_id = ?", channelID).Order("joined_at, user_id").Find(&rows).Error; err != nil {
		return nil, fmt.Errorf("meeting participants: %w", err)
	}
	v := &RoomView{ChannelID: channelID, Participants: make([]ParticipantView, len(rows))}
	if len(revs) > 0 {
		v.Revision = revs[0]
	}
	for i, r := range rows {
		v.Participants[i] = ParticipantView{UserID: strings.TrimSpace(r.UserID), JoinedAt: r.JoinedAt}
	}
	return v, nil
}

// announce sends a room's snapshot to the channel's members (meeting_updated).
func (s *Service) announce(ctx context.Context, v *RoomView) error {
	if s.Publisher == nil {
		return nil
	}
	members, err := s.Chat.MemberIDs(ctx, v.ChannelID)
	if err != nil {
		return fmt.Errorf("meeting audience: %w", err)
	}
	if len(members) == 0 {
		return nil
	}
	var teamIDs []string
	if err := s.DB.Gorm.WithContext(ctx).Model(&chat.Channel{}).Where("id = ?", v.ChannelID).
		Pluck("team_id", &teamIDs).Error; err != nil {
		return fmt.Errorf("meeting channel: %w", err)
	}
	teamID := ""
	if len(teamIDs) > 0 {
		teamID = strings.TrimSpace(teamIDs[0])
	}
	s.Publisher.Publish(context.WithoutCancel(ctx), chat.Event{
		Name:       "meeting_updated",
		Data:       map[string]any{"channel_id": v.ChannelID, "revision": v.Revision, "participants": v.Participants},
		Recipients: members, ChannelID: v.ChannelID, TeamID: teamID,
	})
	return nil
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
	v, err := s.view(ctx, s.DB.Gorm, ch.ID)
	if err != nil {
		return nil, err
	}
	v.TeamID = ch.TeamID
	return v, nil
}

// listSQL: the people in rooms of channels sub is a member of and, for team channels, whose team they are still
// in (the chat's read rule: members only, in the team).
const listSQL = `SELECT p.channel_id, c.team_id, r.revision, p.user_id, p.joined_at
FROM meeting_participants p
JOIN meeting_rooms r ON r.channel_id = p.channel_id
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
		Revision  int64
		UserID    string
		JoinedAt  int64
	}
	if err := s.DB.Gorm.WithContext(ctx).Raw(listSQL, sub.UserID, sub.UserID).Scan(&rows).Error; err != nil {
		return nil, fmt.Errorf("list meetings: %w", err)
	}
	for _, r := range rows {
		id := strings.TrimSpace(r.ChannelID)
		if len(out) == 0 || out[len(out)-1].ChannelID != id {
			out = append(out, RoomView{ChannelID: id, TeamID: strings.TrimSpace(r.TeamID), Revision: r.Revision})
		}
		last := &out[len(out)-1]
		last.Participants = append(last.Participants, ParticipantView{UserID: strings.TrimSpace(r.UserID), JoinedAt: r.JoinedAt})
	}
	return out, nil
}
