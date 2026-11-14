package meetings

import (
	"context"
	"fmt"
	"slices"
	"strings"
	"time"

	"gorm.io/gorm"

	"github.com/jniltinho/meethub/internal/identity"
)

// Reconciliation cadence (design D5): eviction well inside the spec's 10 seconds, the LiveKit refresh every 30.
const (
	EvictEvery   = 5 * time.Second
	RefreshEvery = 30 * time.Second
)

// LiveParticipant is a participant as LiveKit reports it.
type LiveParticipant struct {
	Identity string
	SID      string
	JoinedAt int64 // Unix milliseconds
}

// RoomService is LiveKit's room API as meetings need it. A room LiveKit does not know lists no participants, and
// removing someone who is not there is not an error.
type RoomService interface {
	ListRooms(ctx context.Context) ([]string, error)
	ListParticipants(ctx context.Context, room string) ([]LiveParticipant, error)
	RemoveParticipant(ctx context.Context, room, identity string) error
}

// Run evicts and refreshes until ctx ends; a failed pass is logged and retried on the next tick.
func (s *Service) Run(ctx context.Context) error {
	if !s.Enabled() {
		return nil
	}
	evict, refresh := time.NewTicker(EvictEvery), time.NewTicker(RefreshEvery)
	defer evict.Stop()
	defer refresh.Stop()
	for {
		select {
		case <-ctx.Done():
			return nil
		case <-evict.C:
			if err := s.Evict(ctx); err != nil && ctx.Err() == nil {
				s.logger().WarnContext(ctx, "meetings: eviction", "error", err)
			}
		case <-refresh.C:
			if err := s.Refresh(ctx); err != nil && ctx.Err() == nil {
				s.logger().WarnContext(ctx, "meetings: refresh", "error", err)
			}
		}
	}
}

// evictSQL finds the people in rooms who may no longer read the channel: not a member, not in the channel's team,
// not an active user, or the channel archived.
const evictSQL = `SELECT p.channel_id, p.user_id, r.name
FROM meeting_participants p
JOIN meeting_rooms r ON r.channel_id = p.channel_id
JOIN channels c ON c.id = p.channel_id
LEFT JOIN users u ON u.id = p.user_id
WHERE u.id IS NULL OR u.status <> ? OR c.archived_at <> 0
OR NOT EXISTS (SELECT 1 FROM channel_members cm WHERE cm.channel_id = p.channel_id AND cm.user_id = p.user_id)
OR (c.team_id <> '' AND NOT EXISTS (SELECT 1 FROM team_members tm WHERE tm.team_id = c.team_id AND tm.user_id = p.user_id))`

// Evict removes from LiveKit and from the presence everyone who lost access to their room's channel. A person
// LiveKit could not remove stays listed and is retried on the next pass.
func (s *Service) Evict(ctx context.Context) error {
	var rows []struct {
		ChannelID string
		UserID    string
		Name      string
	}
	if err := s.DB.Gorm.WithContext(ctx).Raw(evictSQL, identity.StatusActive).Scan(&rows).Error; err != nil {
		return fmt.Errorf("find evictions: %w", err)
	}
	changed := map[string]bool{}
	var errs []error
	for _, r := range rows {
		ch, user := strings.TrimSpace(r.ChannelID), strings.TrimSpace(r.UserID)
		if s.Rooms != nil {
			if err := s.Rooms.RemoveParticipant(ctx, r.Name, user); err != nil {
				errs = append(errs, err)
				continue
			}
		}
		if err := s.DB.Gorm.WithContext(ctx).Where("channel_id = ? AND user_id = ?", ch, user).
			Delete(&Participant{}).Error; err != nil {
			return fmt.Errorf("evict: %w", err)
		}
		s.logger().InfoContext(ctx, "meetings: removed a participant who lost access", "channel_id", ch, "user_id", user)
		changed[ch] = true
	}
	for ch := range changed {
		if err := s.announce(ctx, ch); err != nil {
			return err
		}
	}
	if len(errs) > 0 {
		return fmt.Errorf("remove participants: %w", errs[0])
	}
	return nil
}

// Refresh replaces the presence of every room LiveKit reports active or that we list as occupied with LiveKit's
// own participant list (webhooks can be lost), announcing the rooms that changed.
func (s *Service) Refresh(ctx context.Context) error {
	if s.Rooms == nil {
		return nil
	}
	active, err := s.Rooms.ListRooms(ctx)
	if err != nil {
		return fmt.Errorf("list rooms: %w", err)
	}
	var rooms []Room
	tx := s.DB.Gorm.WithContext(ctx)
	if err := tx.Where("name IN ? OR channel_id IN (SELECT channel_id FROM meeting_participants)",
		append(active, "")).Find(&rooms).Error; err != nil {
		return fmt.Errorf("occupied rooms: %w", err)
	}
	for _, r := range rooms {
		ch := strings.TrimSpace(r.ChannelID)
		live, err := s.Rooms.ListParticipants(ctx, r.Name)
		if err != nil {
			return fmt.Errorf("list participants: %w", err)
		}
		changed, err := s.replace(ctx, ch, live)
		if err != nil {
			return err
		}
		if changed {
			if err := s.announce(ctx, ch); err != nil {
				return err
			}
		}
	}
	return nil
}

// replace sets a room's presence to live, reporting whether anything changed.
func (s *Service) replace(ctx context.Context, channelID string, live []LiveParticipant) (bool, error) {
	var rows []Participant
	tx := s.DB.Gorm.WithContext(ctx)
	if err := tx.Where("channel_id = ?", channelID).Find(&rows).Error; err != nil {
		return false, fmt.Errorf("meeting participants: %w", err)
	}
	key := func(user, sid string) string { return strings.TrimSpace(user) + "/" + strings.TrimSpace(sid) }
	have := make([]string, 0, len(rows))
	for _, r := range rows {
		have = append(have, key(r.UserID, r.SID))
	}
	want := make([]Participant, 0, len(live))
	wantKeys := make([]string, 0, len(live))
	for _, p := range live {
		if len(p.Identity) != idLength || slices.ContainsFunc(want, func(w Participant) bool { return w.UserID == p.Identity }) {
			continue
		}
		want = append(want, Participant{ChannelID: channelID, UserID: p.Identity, SID: p.SID, JoinedAt: p.JoinedAt})
		wantKeys = append(wantKeys, key(p.Identity, p.SID))
	}
	slices.Sort(have)
	slices.Sort(wantKeys)
	if slices.Equal(have, wantKeys) {
		return false, nil
	}
	err := tx.Transaction(func(tx *gorm.DB) error {
		if err := tx.Where("channel_id = ?", channelID).Delete(&Participant{}).Error; err != nil {
			return err
		}
		if len(want) == 0 {
			return nil
		}
		return tx.Create(&want).Error
	})
	if err != nil {
		return false, fmt.Errorf("replace meeting participants: %w", err)
	}
	return true, nil
}
