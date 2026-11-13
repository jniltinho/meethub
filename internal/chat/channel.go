package chat

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"gorm.io/gorm"

	"github.com/jniltinho/meethub/internal/audit"
	"github.com/jniltinho/meethub/internal/database"
	"github.com/jniltinho/meethub/internal/identity"
)

// NewChannel is the input of CreateChannel. An empty Name is derived from DisplayName.
type NewChannel struct {
	Name        string
	DisplayName string
	Purpose     string
	Header      string
	Type        string // O or P
}

// ChannelPatch lists editable fields; nil means unchanged.
type ChannelPatch struct {
	Name        *string
	DisplayName *string
	Purpose     *string
	Header      *string
}

// accessTx computes the subject's access to a channel inside tx.
func accessTx(ctx context.Context, tx *gorm.DB, sub Subject, ch *Channel) (Access, *ChannelMember, error) {
	a := Access{Type: ch.Type}
	if ch.TeamID != "" {
		r, err := teamRole(ctx, tx, ch.TeamID, sub.UserID)
		if err != nil {
			return a, nil, err
		}
		a.TeamRole = r
	}
	m, err := channelMember(ctx, tx, ch.ID, sub.UserID)
	if err != nil {
		return a, nil, err
	}
	if m != nil {
		a.ChannelRole = m.Role
	}
	return a, m, nil
}

// requireAdmin checks the administer permission, hiding undiscoverable channels.
func requireAdmin(sub Subject, a Access) error {
	if CanAdminister(sub, a) {
		return nil
	}
	if a.Type == ChannelDirect || a.Type == ChannelGroup {
		if CanDiscover(sub, a) {
			return ErrDirectChannel
		}
		return ErrNotFound
	}
	return ReadError(sub, a)
}

func teamArchived(ctx context.Context, tx *gorm.DB, teamID string) (bool, error) {
	if teamID == "" {
		return false, nil
	}
	var at []int64
	if err := tx.WithContext(ctx).Model(&Team{}).Where("id = ?", teamID).Pluck("archived_at", &at).Error; err != nil {
		return false, err
	}
	return len(at) == 1 && at[0] != 0, nil
}

// CreateChannel creates a public or private channel in a team; the creator
// becomes its channel_admin.
func (s *Service) CreateChannel(ctx context.Context, sub Subject, teamID string, in NewChannel) (*Channel, error) {
	if in.Type != ChannelOpen && in.Type != ChannelPrivate {
		return nil, fmt.Errorf("%w: channel type must be O or P", ErrInvalidRole)
	}
	display, err := CheckDisplayName(in.DisplayName)
	if err != nil {
		return nil, err
	}
	name := in.Name
	if strings.TrimSpace(name) == "" {
		name = SlugFromDisplayName(display)
	}
	if name, err = NormalizeChannelName(name); err != nil {
		return nil, err
	}
	purpose, err := CheckText(in.Purpose, ChannelPurposeMaxRunes)
	if err != nil {
		return nil, err
	}
	header, err := CheckText(in.Header, ChannelHeaderMaxRunes)
	if err != nil {
		return nil, err
	}
	now := s.now()
	ch := &Channel{ID: newID(), TeamID: teamID, Type: in.Type, Name: name, DisplayName: display, Purpose: purpose,
		Header: header, CreatorID: sub.UserID, CreatedAt: now, UpdatedAt: now}
	var em emitter
	err = s.DB.Gorm.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		t, err := lockTeam(ctx, tx, s.DB.Dialect, teamID)
		if err != nil {
			return err
		}
		role, err := teamRole(ctx, tx, t.ID, sub.UserID)
		if err != nil {
			return err
		}
		if role == "" {
			return ErrNotFound
		}
		if !CanCreateChannel(sub, role) {
			return ErrForbidden
		}
		if t.ArchivedAt != 0 {
			return ErrArchived
		}
		if err := database.Translate(tx.Create(ch).Error); err != nil {
			if errors.Is(err, database.ErrDuplicate) {
				return ErrNameTaken
			}
			return fmt.Errorf("create channel: %w", err)
		}
		if err := tx.Create(&ChannelMember{ChannelID: ch.ID, UserID: sub.UserID, Role: ChannelAdmin, LastViewedAt: now,
			Visible: true, CreatedAt: now}).Error; err != nil {
			return fmt.Errorf("add channel creator: %w", err)
		}
		recipients := []string{sub.UserID}
		if ch.Type == ChannelOpen { // public: every team member except guests may discover it
			if err := tx.Table("team_members tm").Joins("JOIN memberships m ON m.user_id = tm.user_id").
				Where("tm.team_id = ? AND m.role <> 'guest' AND tm.user_id <> ?", t.ID, sub.UserID).
				Pluck("tm.user_id", &recipients).Error; err != nil {
				return err
			}
			recipients = append(trimAll(recipients), sub.UserID)
		}
		em.add("channel_created", map[string]any{"channel": ch}, recipients)
		return audit.Record(ctx, tx, auditEvent(sub, audit.ActionChannelCreated, "channel", ch.ID, map[string]any{"name": name, "type": ch.Type}))
	})
	if err != nil {
		return nil, err
	}
	s.publish(ctx, em.events)
	return ch, nil
}

// ChannelInfo is a channel as shown to someone who may discover it.
type ChannelInfo struct {
	Channel
	MemberCount int64  `json:"member_count"`
	Role        string `json:"role,omitempty"` // the caller's channel role, "" when not a member
}

// GetChannel returns a channel the caller may discover (404 otherwise).
func (s *Service) GetChannel(ctx context.Context, sub Subject, channelID string) (*ChannelInfo, error) {
	tx := s.DB.Gorm.WithContext(ctx)
	ch, err := readChannel(ctx, tx, channelID)
	if err != nil {
		return nil, err
	}
	a, _, err := accessTx(ctx, tx, sub, ch)
	if err != nil {
		return nil, err
	}
	if !CanDiscover(sub, a) {
		return nil, ErrNotFound
	}
	var n int64
	if err := tx.Model(&ChannelMember{}).Where("channel_id = ?", ch.ID).Count(&n).Error; err != nil {
		return nil, err
	}
	return &ChannelInfo{Channel: *ch, MemberCount: n, Role: a.ChannelRole}, nil
}

func readChannel(ctx context.Context, tx *gorm.DB, id string) (*Channel, error) {
	var ch Channel
	if err := tx.WithContext(ctx).Where("id = ?", id).Take(&ch).Error; err != nil {
		if database.IsNotFound(err) {
			return nil, ErrNotFound
		}
		return nil, fmt.Errorf("read channel: %w", err)
	}
	ch.trimIDs()
	return &ch, nil
}

// UpdateChannel changes a channel's name, display name, purpose or header.
func (s *Service) UpdateChannel(ctx context.Context, sub Subject, channelID string, p ChannelPatch) (*Channel, error) {
	var out *Channel
	var em emitter
	err := withChannelLock(ctx, s.DB, channelID, func(tx *gorm.DB, ch *Channel) error {
		a, _, err := accessTx(ctx, tx, sub, ch)
		if err != nil {
			return err
		}
		if err := requireAdmin(sub, a); err != nil {
			return err
		}
		if ch.ArchivedAt != 0 {
			return ErrArchived
		}
		updates := map[string]any{"updated_at": s.now()}
		if p.Name != nil {
			n, err := NormalizeChannelName(*p.Name)
			if err != nil {
				return err
			}
			if ch.IsDefault && n != ch.Name {
				return ErrDefaultChannel
			}
			updates["name"], ch.Name = n, n
		}
		if p.DisplayName != nil {
			d, err := CheckDisplayName(*p.DisplayName)
			if err != nil {
				return err
			}
			updates["display_name"], ch.DisplayName = d, d
		}
		if p.Purpose != nil {
			v, err := CheckText(*p.Purpose, ChannelPurposeMaxRunes)
			if err != nil {
				return err
			}
			updates["purpose"], ch.Purpose = v, v
		}
		if p.Header != nil {
			v, err := CheckText(*p.Header, ChannelHeaderMaxRunes)
			if err != nil {
				return err
			}
			updates["header"], ch.Header = v, v
		}
		if err := database.Translate(tx.Model(&Channel{}).Where("id = ?", ch.ID).Updates(updates).Error); err != nil {
			if errors.Is(err, database.ErrDuplicate) {
				return ErrNameTaken
			}
			return err
		}
		members, err := channelMemberIDs(ctx, tx, ch.ID)
		if err != nil {
			return err
		}
		if e := em.add("channel_updated", map[string]any{"channel": ch}, members); e != nil {
			e.ChannelID = ch.ID
		}
		out = ch
		return audit.Record(ctx, tx, auditEvent(sub, audit.ActionChannelUpdated, "channel", ch.ID, nil))
	})
	if err != nil {
		return nil, err
	}
	s.publish(ctx, em.events)
	return out, nil
}

// SetChannelArchived archives or restores a channel (never the default one).
func (s *Service) SetChannelArchived(ctx context.Context, sub Subject, channelID string, archived bool) error {
	var em emitter
	err := withChannelLock(ctx, s.DB, channelID, func(tx *gorm.DB, ch *Channel) error {
		a, _, err := accessTx(ctx, tx, sub, ch)
		if err != nil {
			return err
		}
		if err := requireAdmin(sub, a); err != nil {
			return err
		}
		if ch.IsDefault {
			return ErrDefaultChannel
		}
		at, action, event := int64(0), audit.ActionChannelRestored, "channel_restored"
		if archived {
			at, action, event = s.now(), audit.ActionChannelArchived, "channel_deleted"
		}
		if err := tx.Model(&Channel{}).Where("id = ?", ch.ID).Updates(map[string]any{"archived_at": at, "updated_at": s.now()}).Error; err != nil {
			return err
		}
		members, err := channelMemberIDs(ctx, tx, ch.ID)
		if err != nil {
			return err
		}
		if e := em.add(event, map[string]any{"channel_id": ch.ID, "team_id": ch.TeamID}, members); e != nil {
			e.ChannelID = ch.ID
		}
		return audit.Record(ctx, tx, auditEvent(sub, action, "channel", ch.ID, nil))
	})
	if err != nil {
		return err
	}
	s.publish(ctx, em.events)
	return nil
}

// JoinChannel lets a team member (not a guest) join a public channel.
func (s *Service) JoinChannel(ctx context.Context, sub Subject, channelID string) error {
	var em emitter
	err := withChannelLock(ctx, s.DB, channelID, func(tx *gorm.DB, ch *Channel) error {
		a, _, err := accessTx(ctx, tx, sub, ch)
		if err != nil {
			return err
		}
		if a.ChannelRole != "" {
			return nil
		}
		if !CanJoin(sub, a) {
			return ReadError(sub, a)
		}
		if err := writable(ctx, tx, ch); err != nil {
			return err
		}
		return joinChannelTx(ctx, tx, ch, sub.UserID, ChannelUser, s.now(), &em)
	})
	if err != nil {
		return err
	}
	s.publish(ctx, em.events)
	return nil
}

// writable refuses archived channels and channels of archived teams.
func writable(ctx context.Context, tx *gorm.DB, ch *Channel) error {
	if ch.ArchivedAt != 0 {
		return ErrArchived
	}
	if archived, err := teamArchived(ctx, tx, ch.TeamID); err != nil {
		return err
	} else if archived {
		return ErrArchived
	}
	return nil
}

// AddChannelMember lets a channel administrator add a member of the team.
func (s *Service) AddChannelMember(ctx context.Context, sub Subject, channelID, userID string) error {
	var em emitter
	err := withChannelLock(ctx, s.DB, channelID, func(tx *gorm.DB, ch *Channel) error {
		a, _, err := accessTx(ctx, tx, sub, ch)
		if err != nil {
			return err
		}
		if err := requireAdmin(sub, a); err != nil {
			return err
		}
		if err := writable(ctx, tx, ch); err != nil {
			return err
		}
		if role, err := teamRole(ctx, tx, ch.TeamID, userID); err != nil {
			return err
		} else if role == "" {
			return ErrNotTeamMember
		}
		u, err := identity.Store{}.ByID(ctx, tx, userID)
		if err != nil {
			return err
		}
		if u.Status != identity.StatusActive {
			return ErrUserInactive
		}
		if err := joinChannelTx(ctx, tx, ch, userID, ChannelUser, s.now(), &em); err != nil {
			return err
		}
		if ch.Type == ChannelPrivate {
			return audit.Record(ctx, tx, auditEvent(sub, audit.ActionChannelMemberAdd, "channel", ch.ID, map[string]any{"user_id": userID}))
		}
		return nil
	})
	if err != nil {
		return err
	}
	s.publish(ctx, em.events)
	return nil
}

// RemoveChannelMember removes a member (the user leaving, or an administrator);
// nobody leaves the default channel, and conversations have fixed members.
func (s *Service) RemoveChannelMember(ctx context.Context, sub Subject, channelID, userID string) error {
	var em emitter
	err := withChannelLock(ctx, s.DB, channelID, func(tx *gorm.DB, ch *Channel) error {
		a, _, err := accessTx(ctx, tx, sub, ch)
		if err != nil {
			return err
		}
		if ch.Type == ChannelDirect || ch.Type == ChannelGroup {
			if !CanDiscover(sub, a) {
				return ErrNotFound
			}
			return ErrDirectChannel
		}
		if userID != sub.UserID {
			if err := requireAdmin(sub, a); err != nil {
				return err
			}
		} else if a.ChannelRole == "" {
			return ReadError(sub, a)
		}
		if ch.IsDefault {
			return ErrDefaultChannel
		}
		if ch.ArchivedAt != 0 {
			return ErrArchived
		}
		if m, err := channelMember(ctx, tx, ch.ID, userID); err != nil {
			return err
		} else if m == nil {
			return ErrNotFound
		}
		if err := leaveChannelTx(ctx, tx, ch, userID, &em); err != nil {
			return err
		}
		if ch.Type == ChannelPrivate && userID != sub.UserID {
			return audit.Record(ctx, tx, auditEvent(sub, audit.ActionChannelMemberRem, "channel", ch.ID, map[string]any{"user_id": userID}))
		}
		return nil
	})
	if err != nil {
		return err
	}
	s.publish(ctx, em.events)
	return nil
}

// SetChannelRole changes a member's channel role (never the caller's own).
func (s *Service) SetChannelRole(ctx context.Context, sub Subject, channelID, userID, role string) error {
	if role != ChannelAdmin && role != ChannelUser {
		return ErrInvalidRole
	}
	var em emitter
	err := withChannelLock(ctx, s.DB, channelID, func(tx *gorm.DB, ch *Channel) error {
		a, _, err := accessTx(ctx, tx, sub, ch)
		if err != nil {
			return err
		}
		if err := requireAdmin(sub, a); err != nil {
			return err
		}
		if userID == sub.UserID {
			return ErrForbidden
		}
		m, err := channelMember(ctx, tx, ch.ID, userID)
		if err != nil {
			return err
		}
		if m == nil {
			return ErrNotFound
		}
		if err := tx.Model(&ChannelMember{}).Where("channel_id = ? AND user_id = ?", ch.ID, userID).Update("role", role).Error; err != nil {
			return err
		}
		var admins []string
		if err := tx.Model(&ChannelMember{}).Where("channel_id = ? AND role = ?", ch.ID, ChannelAdmin).Pluck("user_id", &admins).Error; err != nil {
			return err
		}
		if e := em.add("channel_member_updated", map[string]any{"channel_id": ch.ID, "user_id": userID, "role": role},
			append(trimAll(admins), userID)); e != nil {
			e.ChannelID = ch.ID
		}
		return audit.Record(ctx, tx, auditEvent(sub, audit.ActionChannelRoleChange, "channel", ch.ID,
			map[string]any{"user_id": userID, "from": m.Role, "to": role}))
	})
	if err != nil {
		return err
	}
	s.publish(ctx, em.events)
	return nil
}

// ChannelWithState is a channel in the caller's sidebar.
type ChannelWithState struct {
	Channel
	Role         string `json:"role"`
	MsgCount     int64  `json:"msg_count"`
	MentionCount int64  `json:"mention_count"`
	Unread       int64  `json:"unread"`
}

// MyChannels lists the team channels the caller belongs to, with read state,
// ordered by display name, cursor-paginated.
func (s *Service) MyChannels(ctx context.Context, sub Subject, teamID, cursor string, limit int) ([]ChannelWithState, string, error) {
	tx := s.DB.Gorm.WithContext(ctx)
	if role, err := teamRole(ctx, tx, teamID, sub.UserID); err != nil {
		return nil, "", err
	} else if role == "" {
		return nil, "", ErrNotFound
	}
	limit = clampLimit(limit)
	q := tx.Table("channels c").
		Select("c.*, m.role AS role, m.msg_count AS msg_count, m.mention_count AS mention_count, c.total_msg_count - m.msg_count AS unread").
		Joins("JOIN channel_members m ON m.channel_id = c.id AND m.user_id = ?", sub.UserID).
		Where("c.team_id = ?", teamID)
	if cursor != "" {
		name, id, ok := strings.Cut(cursor, "\x00")
		if !ok {
			return nil, "", ErrNotFound
		}
		q = q.Where("(c.display_name > ? OR (c.display_name = ? AND c.id > ?))", name, name, id)
	}
	var rows []ChannelWithState
	if err := q.Order("c.display_name, c.id").Limit(limit + 1).Scan(&rows).Error; err != nil {
		return nil, "", fmt.Errorf("list channels: %w", err)
	}
	next := ""
	if len(rows) > limit {
		rows = rows[:limit]
		last := rows[limit-1]
		next = last.DisplayName + "\x00" + strings.TrimSpace(last.ID)
	}
	for i := range rows {
		rows[i].trimIDs()
	}
	return rows, next, nil
}

// JoinableChannels lists the team's public, unarchived channels the caller has
// not joined (none for guests).
func (s *Service) JoinableChannels(ctx context.Context, sub Subject, teamID, cursor string, limit int) ([]ChannelInfo, string, error) {
	tx := s.DB.Gorm.WithContext(ctx)
	if role, err := teamRole(ctx, tx, teamID, sub.UserID); err != nil {
		return nil, "", err
	} else if role == "" {
		return nil, "", ErrNotFound
	}
	if sub.Guest() {
		return []ChannelInfo{}, "", nil
	}
	limit = clampLimit(limit)
	q := tx.Table("channels c").
		Select("c.*, (SELECT COUNT(*) FROM channel_members x WHERE x.channel_id = c.id) AS member_count").
		Where("c.team_id = ? AND c.type = ? AND c.archived_at = 0", teamID, ChannelOpen).
		Where("c.id NOT IN (SELECT channel_id FROM channel_members WHERE user_id = ?)", sub.UserID)
	if cursor != "" {
		name, id, _ := strings.Cut(cursor, "\x00")
		q = q.Where("(c.display_name > ? OR (c.display_name = ? AND c.id > ?))", name, name, id)
	}
	var rows []ChannelInfo
	if err := q.Order("c.display_name, c.id").Limit(limit + 1).Scan(&rows).Error; err != nil {
		return nil, "", fmt.Errorf("list joinable channels: %w", err)
	}
	next := ""
	if len(rows) > limit {
		rows = rows[:limit]
		last := rows[limit-1]
		next = last.DisplayName + "\x00" + strings.TrimSpace(last.ID)
	}
	for i := range rows {
		rows[i].trimIDs()
	}
	return rows, next, nil
}

// Member is a channel member as other users see them (no e-mail).
type Member struct {
	identity.PublicUser
	Role string `json:"role"`
}

// Members lists a channel's members (readers only), ordered by username.
func (s *Service) Members(ctx context.Context, sub Subject, channelID, cursor string, limit int) ([]Member, string, error) {
	tx := s.DB.Gorm.WithContext(ctx)
	ch, err := readChannel(ctx, tx, channelID)
	if err != nil {
		return nil, "", err
	}
	a, _, err := accessTx(ctx, tx, sub, ch)
	if err != nil {
		return nil, "", err
	}
	if !CanRead(sub, a) {
		return nil, "", ReadError(sub, a)
	}
	limit = clampLimit(limit)
	q := tx.Table("channel_members m").Select("u.id, u.username, u.display_name, u.status, m.role").
		Joins("JOIN users u ON u.id = m.user_id").Where("m.channel_id = ?", ch.ID)
	if cursor != "" {
		q = q.Where("u.username_normalized > ?", cursor)
	}
	var rows []struct {
		ID, DisplayName, Status, Role string
		Username                      *string
	}
	if err := q.Order("u.username_normalized").Limit(limit + 1).Scan(&rows).Error; err != nil {
		return nil, "", fmt.Errorf("list members: %w", err)
	}
	next := ""
	if len(rows) > limit {
		rows = rows[:limit]
		if u := rows[limit-1].Username; u != nil {
			next = *u
		}
	}
	out := make([]Member, len(rows))
	for i, r := range rows {
		u := identity.User{ID: r.ID, DisplayName: r.DisplayName, Status: r.Status, Username: r.Username}
		out[i] = Member{PublicUser: u.Public(), Role: r.Role}
	}
	return out, next, nil
}
