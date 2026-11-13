package chat

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"strings"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"

	"github.com/jniltinho/meethub/internal/config"
	"github.com/jniltinho/meethub/internal/database"
	"github.com/jniltinho/meethub/internal/identity"
	"github.com/jniltinho/meethub/internal/org"
)

// ErrInvalidGroup is returned for a group outside 3 to 8 distinct people.
var ErrInvalidGroup = errors.New("a group conversation has 3 to 8 distinct people")

// OpenDirect returns the 1:1 conversation between the caller and other (the
// caller's own conversation when other is the caller), creating it once with
// both members, and makes it visible to the caller.
func (s *Service) OpenDirect(ctx context.Context, sub Subject, otherID string) (*Channel, error) {
	ids := []string{sub.UserID}
	if otherID != sub.UserID {
		ids = append(ids, otherID)
	}
	return s.openConversation(ctx, sub, ChannelDirect, GetDMNameFromIDs(sub.UserID, otherID), ids)
}

// OpenGroup returns the group conversation of exactly the caller and userIDs
// (3 to 8 distinct people in all), creating it once with all members.
func (s *Service) OpenGroup(ctx context.Context, sub Subject, userIDs []string) (*Channel, error) {
	ids := append([]string{sub.UserID}, userIDs...)
	slices.Sort(ids)
	ids = slices.Compact(ids)
	if len(ids) < GroupMinMembers || len(ids) > GroupMaxMembers {
		return nil, ErrInvalidGroup
	}
	return s.openConversation(ctx, sub, ChannelGroup, GetGroupNameFromUserIDs(ids), ids)
}

func (s *Service) openConversation(ctx context.Context, sub Subject, typ, name string, ids []string) (*Channel, error) {
	var out *Channel
	err := s.DB.Gorm.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		existing, err := findConversation(ctx, tx, name)
		if err != nil {
			return err
		}
		if existing == nil {
			users, err := conversationUsers(ctx, tx, sub, ids)
			if err != nil {
				return err
			}
			if existing, err = s.createConversation(ctx, tx, typ, name, sub.UserID, users); err != nil {
				return err
			}
		}
		if err := tx.Model(&ChannelMember{}).Where("channel_id = ? AND user_id = ?", existing.ID, sub.UserID).
			Update("visible", true).Error; err != nil {
			return err
		}
		out = existing
		return nil
	})
	if err != nil {
		return nil, err
	}
	return out, nil
}

func findConversation(ctx context.Context, tx *gorm.DB, name string) (*Channel, error) {
	var ch Channel
	err := tx.WithContext(ctx).Where("team_id = '' AND name = ?", name).Take(&ch).Error
	if database.IsNotFound(err) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("find conversation: %w", err)
	}
	ch.trimIDs()
	return &ch, nil
}

// findConversationLatest reads the conversation with a locking read (latest committed version).
func findConversationLatest(ctx context.Context, tx *gorm.DB, dialect, name string) (*Channel, error) {
	q := tx.WithContext(ctx).Where("team_id = '' AND name = ?", name)
	if dialect != config.DriverSQLite {
		q = q.Clauses(clause.Locking{Strength: "UPDATE"}) // portable: MariaDB has no FOR SHARE
	}
	var ch Channel
	if err := q.Take(&ch).Error; err != nil {
		if database.IsNotFound(err) {
			return nil, nil
		}
		return nil, fmt.Errorf("find conversation: %w", err)
	}
	ch.trimIDs()
	return &ch, nil
}

// conversationUsers loads the participants and applies the rules: every one
// active, and every guest sharing a team channel with each other participant.
func conversationUsers(ctx context.Context, tx *gorm.DB, sub Subject, ids []string) ([]identity.User, error) {
	var users []identity.User
	if err := tx.WithContext(ctx).Where("id IN ?", ids).Find(&users).Error; err != nil {
		return nil, fmt.Errorf("load participants: %w", err)
	}
	if len(users) != len(ids) {
		return nil, ErrNotFound
	}
	for i := range users {
		users[i].ID = strings.TrimSpace(users[i].ID)
		if users[i].Status != identity.StatusActive {
			return nil, ErrUserInactive
		}
	}
	var guests []string
	if err := tx.WithContext(ctx).Model(&org.Membership{}).Where("user_id IN ? AND role = ?", ids, org.RoleGuest).
		Pluck("user_id", &guests).Error; err != nil {
		return nil, err
	}
	for _, g := range trimAll(guests) {
		for _, other := range ids {
			if other == g {
				continue
			}
			ok, err := shareTeamChannel(ctx, tx, g, other)
			if err != nil {
				return nil, err
			}
			if !ok {
				return nil, ErrForbidden
			}
		}
	}
	return users, nil
}

func shareTeamChannel(ctx context.Context, tx *gorm.DB, a, b string) (bool, error) {
	var n int64
	err := tx.WithContext(ctx).Table("channel_members x").
		Joins("JOIN channel_members y ON y.channel_id = x.channel_id").
		Joins("JOIN channels c ON c.id = x.channel_id").
		Where("x.user_id = ? AND y.user_id = ? AND c.type IN ?", a, b, []string{ChannelOpen, ChannelPrivate}).
		Limit(1).Count(&n).Error
	return n > 0, err
}

// createConversation inserts the channel and all its members in the caller's
// transaction; a concurrent creator of the same conversation makes the insert
// a no-op (ON CONFLICT DO NOTHING, portable) and the winner's complete
// conversation is read back.
func (s *Service) createConversation(ctx context.Context, tx *gorm.DB, typ, name, creatorID string, users []identity.User) (*Channel, error) {
	now := s.now()
	display := ""
	if typ == ChannelGroup {
		names := make([]string, 0, len(users))
		for _, u := range users {
			names = append(names, u.Public().Username)
		}
		slices.Sort(names)
		display = strings.Join(names, ", ")
		if len([]rune(display)) > ChannelDisplayNameMaxRunes {
			display = string([]rune(display)[:ChannelDisplayNameMaxRunes])
		}
	}
	ch := &Channel{ID: newID(), Type: typ, Name: name, DisplayName: display, CreatorID: creatorID, CreatedAt: now, UpdatedAt: now}
	res := tx.WithContext(ctx).Clauses(clause.OnConflict{DoNothing: true}).Create(ch)
	if res.Error != nil {
		return nil, fmt.Errorf("create conversation: %w", res.Error)
	}
	if res.RowsAffected == 0 { // created concurrently: the winner committed it with all its members
		// a locking read sees the latest committed row; a plain SELECT under MariaDB's
		// REPEATABLE READ would still read this transaction's older snapshot
		winner, err := findConversationLatest(ctx, tx, s.DB.Dialect, name)
		if err != nil {
			return nil, err
		}
		if winner == nil {
			return nil, fmt.Errorf("create conversation: conflict without a row")
		}
		return winner, nil
	}
	members := make([]ChannelMember, 0, len(users))
	for _, u := range users {
		members = append(members, ChannelMember{
			ChannelID: ch.ID, UserID: u.ID, Role: ChannelUser, LastViewedAt: now, Visible: u.ID == creatorID, CreatedAt: now,
		})
	}
	if err := tx.WithContext(ctx).Create(&members).Error; err != nil {
		return nil, fmt.Errorf("add conversation members: %w", err)
	}
	return ch, nil
}

// SetConversationVisible shows or hides a conversation in the caller's list
// (it shows again on its next message).
func (s *Service) SetConversationVisible(ctx context.Context, sub Subject, channelID string, visible bool) error {
	res := s.DB.Gorm.WithContext(ctx).Exec(
		`UPDATE channel_members SET visible = ? WHERE channel_id = ? AND user_id = ?
		 AND channel_id IN (SELECT id FROM channels WHERE type IN ('D', 'G'))`, visible, channelID, sub.UserID)
	if res.Error != nil {
		return res.Error
	}
	if res.RowsAffected == 0 {
		return ErrNotFound
	}
	return nil
}

// revealConversationTx makes a conversation visible to members who were not
// seeing it, when a message arrives; it returns who just started seeing it
// (they receive direct_added / group_added). The caller holds the channel lock.
func revealConversationTx(ctx context.Context, tx *gorm.DB, ch *Channel) ([]string, error) {
	if ch.Type != ChannelDirect && ch.Type != ChannelGroup {
		return nil, nil
	}
	var hidden []string
	if err := tx.WithContext(ctx).Model(&ChannelMember{}).Where("channel_id = ? AND visible = ?", ch.ID, false).
		Pluck("user_id", &hidden).Error; err != nil {
		return nil, err
	}
	if len(hidden) == 0 {
		return nil, nil
	}
	if err := tx.WithContext(ctx).Model(&ChannelMember{}).Where("channel_id = ? AND visible = ?", ch.ID, false).
		Update("visible", true).Error; err != nil {
		return nil, err
	}
	return trimAll(hidden), nil
}

// Conversation is a direct or group conversation in the caller's list.
type Conversation struct {
	Channel
	Members      []identity.PublicUser `gorm:"-" json:"members"`
	MentionCount int64                 `json:"mention_count"`
	Unread       int64                 `json:"unread"`
}

// MyConversations lists the caller's visible conversations, most recent first,
// cursor-paginated.
func (s *Service) MyConversations(ctx context.Context, sub Subject, cursor string, limit int) ([]Conversation, string, error) {
	limit = clampLimit(limit)
	tx := s.DB.Gorm.WithContext(ctx)
	q := tx.Table("channels c").
		Select("c.*, m.mention_count AS mention_count, c.total_msg_count - m.msg_count AS unread").
		Joins("JOIN channel_members m ON m.channel_id = c.id AND m.user_id = ? AND m.visible = ?", sub.UserID, true).
		Where("c.type IN ?", []string{ChannelDirect, ChannelGroup})
	if cursor != "" {
		at, id, ok := strings.Cut(cursor, ":")
		if !ok {
			return nil, "", ErrNotFound
		}
		q = q.Where("(c.last_post_at < ? OR (c.last_post_at = ? AND c.id < ?))", at, at, id)
	}
	var rows []Conversation
	if err := q.Order("c.last_post_at DESC, c.id DESC").Limit(limit + 1).Scan(&rows).Error; err != nil {
		return nil, "", fmt.Errorf("list conversations: %w", err)
	}
	next := ""
	if len(rows) > limit {
		rows = rows[:limit]
		last := rows[limit-1]
		next = fmt.Sprintf("%d:%s", last.LastPostAt, strings.TrimSpace(last.ID))
	}
	for i := range rows {
		rows[i].trimIDs()
		var users []identity.User
		if err := tx.Table("users u").Joins("JOIN channel_members m ON m.user_id = u.id").
			Where("m.channel_id = ?", rows[i].ID).Order("u.username_normalized").Find(&users).Error; err != nil {
			return nil, "", err
		}
		rows[i].Members = make([]identity.PublicUser, len(users))
		for j := range users {
			rows[i].Members[j] = users[j].Public()
		}
	}
	return rows, next, nil
}
