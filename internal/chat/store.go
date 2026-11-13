package chat

import (
	"context"
	"fmt"
	"strings"
	"time"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"

	"github.com/jniltinho/meethub/internal/config"
	"github.com/jniltinho/meethub/internal/database"
)

// Team is a Mattermost team.
type Team struct {
	ID          string `gorm:"primaryKey;size:26" json:"id"`
	Name        string `json:"name"`
	DisplayName string `json:"display_name"`
	Description string `json:"description"`
	Type        string `json:"type"`
	IsDefault   bool   `json:"is_default"`
	ArchivedAt  int64  `json:"archived_at"`
	CreatedAt   int64  `gorm:"autoCreateTime:false" json:"created_at"`
	UpdatedAt   int64  `gorm:"autoUpdateTime:false" json:"updated_at"`
}

// TeamMember is a user's membership of a team.
type TeamMember struct {
	TeamID    string `gorm:"primaryKey;size:26" json:"team_id"`
	UserID    string `gorm:"primaryKey;size:26" json:"user_id"`
	Role      string `json:"role"`
	CreatedAt int64  `gorm:"autoCreateTime:false" json:"created_at"`
}

// Channel is a Mattermost channel (O, P, D or G). Direct and group channels
// have TeamID "".
type Channel struct {
	ID            string `gorm:"primaryKey;size:26" json:"id"`
	TeamID        string `json:"team_id"`
	Type          string `json:"type"`
	Name          string `json:"name"`
	DisplayName   string `json:"display_name"`
	Purpose       string `json:"purpose"`
	Header        string `json:"header"`
	CreatorID     string `json:"creator_id"`
	IsDefault     bool   `json:"is_default"`
	Revision      int64  `json:"revision"`
	TotalMsgCount int64  `json:"total_msg_count"`
	LastPostAt    int64  `json:"last_post_at"`
	ArchivedAt    int64  `json:"archived_at"`
	CreatedAt     int64  `gorm:"autoCreateTime:false" json:"created_at"`
	UpdatedAt     int64  `gorm:"autoUpdateTime:false" json:"updated_at"`
}

// ChannelMember is a user's membership of a channel with their read state.
type ChannelMember struct {
	ChannelID    string `gorm:"primaryKey;size:26" json:"channel_id"`
	UserID       string `gorm:"primaryKey;size:26" json:"user_id"`
	Role         string `json:"role"`
	MsgCount     int64  `json:"msg_count"`
	MentionCount int64  `json:"mention_count"`
	LastViewedAt int64  `json:"last_viewed_at"`
	Visible      bool   `json:"visible"`
	CreatedAt    int64  `gorm:"autoCreateTime:false" json:"created_at"`
}

// trimIDs removes the padding PostgreSQL adds to CHAR(26) columns.
func (c *Channel) trimIDs() {
	c.ID, c.TeamID, c.CreatorID = strings.TrimSpace(c.ID), strings.TrimSpace(c.TeamID), strings.TrimSpace(c.CreatorID)
}

func (t *Team) trimIDs() { t.ID = strings.TrimSpace(t.ID) }

// nowMillis is the current time in Unix milliseconds (Mattermost's time shape).
func nowMillis(now time.Time) int64 { return now.UTC().UnixMilli() }

// withChannelLock runs fn in a transaction that first takes the channel's row
// lock by bumping its revision (design D3). Every write to a channel's content or
// membership goes through it, so those writes are serialized per channel and
// commit order equals revision order. fn receives the channel as it is under
// the lock, with the new revision to stamp on what it changes.
func withChannelLock(ctx context.Context, db *database.DB, channelID string, fn func(tx *gorm.DB, ch *Channel) error) error {
	return db.Gorm.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		ch, err := lockChannel(ctx, tx, channelID)
		if err != nil {
			return err
		}
		return fn(tx, ch)
	})
}

// lockChannel bumps the channel's revision (taking its row lock until the
// transaction ends) and reads it back.
func lockChannel(ctx context.Context, tx *gorm.DB, channelID string) (*Channel, error) {
	res := tx.WithContext(ctx).Exec(`UPDATE channels SET revision = revision + 1 WHERE id = ?`, channelID)
	if res.Error != nil {
		return nil, fmt.Errorf("lock channel: %w", res.Error)
	}
	if res.RowsAffected == 0 {
		return nil, ErrNotFound
	}
	var ch Channel
	if err := tx.WithContext(ctx).Where("id = ?", channelID).Take(&ch).Error; err != nil {
		return nil, fmt.Errorf("read locked channel: %w", err)
	}
	ch.trimIDs()
	return &ch, nil
}

// lockTeam takes the team's row lock (lock order: team before its channels) and
// reads it.
func lockTeam(ctx context.Context, tx *gorm.DB, dialect, teamID string) (*Team, error) {
	q := tx.WithContext(ctx).Where("id = ?", teamID)
	if dialect != config.DriverSQLite {
		q = q.Clauses(clause.Locking{Strength: "UPDATE"})
	}
	var t Team
	if err := q.Take(&t).Error; err != nil {
		if database.IsNotFound(err) {
			return nil, ErrNotFound
		}
		return nil, fmt.Errorf("lock team: %w", err)
	}
	t.trimIDs()
	return &t, nil
}

// channelMember reads a user's membership of a channel ("" role when absent).
func channelMember(ctx context.Context, tx *gorm.DB, channelID, userID string) (*ChannelMember, error) {
	var m ChannelMember
	err := tx.WithContext(ctx).Where("channel_id = ? AND user_id = ?", channelID, userID).Take(&m).Error
	if database.IsNotFound(err) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("read channel member: %w", err)
	}
	return &m, nil
}

// teamRole is a user's role in a team, "" when not a member.
func teamRole(ctx context.Context, tx *gorm.DB, teamID, userID string) (string, error) {
	if teamID == "" {
		return "", nil
	}
	var roles []string
	if err := tx.WithContext(ctx).Model(&TeamMember{}).Where("team_id = ? AND user_id = ?", teamID, userID).
		Limit(1).Pluck("role", &roles).Error; err != nil {
		return "", fmt.Errorf("read team role: %w", err)
	}
	if len(roles) == 0 {
		return "", nil
	}
	return roles[0], nil
}
