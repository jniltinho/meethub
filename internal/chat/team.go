package chat

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/oklog/ulid/v2"
	"gorm.io/gorm"

	"github.com/jniltinho/meethub/internal/audit"
	"github.com/jniltinho/meethub/internal/database"
	"github.com/jniltinho/meethub/internal/identity"
)

// DefaultChannelName is the URL name of every team's default channel.
const DefaultChannelName = "general"

// NewTeam is the input of CreateTeam.
type NewTeam struct {
	Name        string
	DisplayName string
	Description string
	Type        string
}

// TeamPatch lists editable team fields; nil means unchanged.
type TeamPatch struct {
	DisplayName *string
	Description *string
	Type        *string
}

func newID() string { return ulid.Make().String() }

func auditEvent(s Subject, action, targetType, targetID string, details map[string]any) audit.Event {
	actor := s.UserID
	return audit.Event{ActorID: &actor, Action: action, TargetType: targetType, TargetID: targetID, Details: details}
}

func checkTeamInput(name, display, description, typ string) (string, string, string, error) {
	n, err := NormalizeTeamName(name)
	if err != nil {
		return "", "", "", err
	}
	d, err := CheckDisplayName(display)
	if err != nil {
		return "", "", "", err
	}
	desc, err := CheckText(description, TeamDescriptionMaxRunes)
	if err != nil {
		return "", "", "", err
	}
	if typ != TeamOpen && typ != TeamInvite {
		return "", "", "", fmt.Errorf("%w: team type must be open or invite", ErrInvalidRole)
	}
	return n, d, desc, nil
}

// CreateTeam creates a team with its default channel; the creator becomes its
// team_admin and a member of the default channel.
func (s *Service) CreateTeam(ctx context.Context, sub Subject, in NewTeam) (*Team, error) {
	if !CanCreateTeam(sub) {
		return nil, ErrForbidden
	}
	if in.Type == "" {
		in.Type = TeamOpen
	}
	if strings.TrimSpace(in.Name) == "" {
		// the name is optional, as for channels; team names have no underscore
		in.Name = strings.Trim(strings.ReplaceAll(SlugFromDisplayName(in.DisplayName), "_", "-"), "-")
	}
	name, display, desc, err := checkTeamInput(in.Name, in.DisplayName, in.Description, in.Type)
	if err != nil {
		return nil, err
	}
	now := s.now()
	t := &Team{ID: newID(), Name: name, DisplayName: display, Description: desc, Type: in.Type, CreatedAt: now, UpdatedAt: now}
	var em emitter
	err = s.DB.Gorm.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := createTeamTx(ctx, tx, t, sub.UserID, now); err != nil {
			return err
		}
		em.add("added_to_team", map[string]any{"team_id": t.ID, "user_id": sub.UserID}, []string{sub.UserID})
		return audit.Record(ctx, tx, auditEvent(sub, audit.ActionTeamCreated, "team", t.ID, map[string]any{"name": name}))
	})
	if err != nil {
		return nil, err
	}
	s.publish(ctx, em.events)
	return t, nil
}

// createTeamTx inserts the team, its default channel and the creator as team
// admin and default-channel member.
func createTeamTx(ctx context.Context, tx *gorm.DB, t *Team, creatorID string, now int64) error {
	if err := database.Translate(tx.WithContext(ctx).Create(t).Error); err != nil {
		if errors.Is(err, database.ErrDuplicate) {
			return ErrNameTaken
		}
		return fmt.Errorf("create team: %w", err)
	}
	ch := &Channel{
		ID: newID(), TeamID: t.ID, Type: ChannelOpen, Name: DefaultChannelName, DisplayName: "General",
		CreatorID: creatorID, IsDefault: true, CreatedAt: now, UpdatedAt: now,
	}
	if err := tx.WithContext(ctx).Create(ch).Error; err != nil {
		return fmt.Errorf("create default channel: %w", err)
	}
	if creatorID == "" {
		return nil
	}
	if err := tx.WithContext(ctx).Create(&TeamMember{TeamID: t.ID, UserID: creatorID, Role: TeamAdmin, CreatedAt: now}).Error; err != nil {
		return fmt.Errorf("add team creator: %w", err)
	}
	return tx.WithContext(ctx).Create(&ChannelMember{
		ChannelID: ch.ID, UserID: creatorID, Role: ChannelAdmin, Visible: true, CreatedAt: now,
	}).Error
}

// UpdateTeam changes a team's display name, description or type.
func (s *Service) UpdateTeam(ctx context.Context, sub Subject, teamID string, p TeamPatch) (*Team, error) {
	var out *Team
	var em emitter
	err := s.DB.Gorm.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		t, err := lockTeam(ctx, tx, s.DB.Dialect, teamID)
		if err != nil {
			return err
		}
		role, err := teamRole(ctx, tx, t.ID, sub.UserID)
		if err != nil {
			return err
		}
		if !CanAdministerTeam(sub, role) {
			return teamReadError(role)
		}
		display, desc, typ := t.DisplayName, t.Description, t.Type
		if p.DisplayName != nil {
			display = *p.DisplayName
		}
		if p.Description != nil {
			desc = *p.Description
		}
		if p.Type != nil {
			typ = *p.Type
		}
		_, display, desc, err = checkTeamInput(t.Name, display, desc, typ)
		if err != nil {
			return err
		}
		t.DisplayName, t.Description, t.Type, t.UpdatedAt = display, desc, typ, s.now()
		if err := tx.Model(&Team{}).Where("id = ?", t.ID).Updates(map[string]any{
			"display_name": display, "description": desc, "type": typ, "updated_at": t.UpdatedAt,
		}).Error; err != nil {
			return fmt.Errorf("update team: %w", err)
		}
		members, err := teamMemberIDs(ctx, tx, t.ID)
		if err != nil {
			return err
		}
		em.add("update_team", map[string]any{"team": t}, members)
		out = t
		return audit.Record(ctx, tx, auditEvent(sub, audit.ActionTeamUpdated, "team", t.ID, nil))
	})
	if err != nil {
		return nil, err
	}
	s.publish(ctx, em.events)
	return out, nil
}

// teamReadError hides a team from non-members (404) and refuses members (403).
func teamReadError(role string) error {
	if role == "" {
		return ErrNotFound
	}
	return ErrForbidden
}

// SetTeamArchived archives or restores a team (never the default team). Every
// channel of the team is locked in id order, so the change serializes with
// posts in those channels.
func (s *Service) SetTeamArchived(ctx context.Context, sub Subject, teamID string, archived bool) error {
	var em emitter
	err := s.DB.Gorm.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		t, err := lockTeam(ctx, tx, s.DB.Dialect, teamID)
		if err != nil {
			return err
		}
		role, err := teamRole(ctx, tx, t.ID, sub.UserID)
		if err != nil {
			return err
		}
		if !CanAdministerTeam(sub, role) {
			return teamReadError(role)
		}
		if t.IsDefault {
			return ErrDefaultTeam
		}
		at, action, event := int64(0), audit.ActionTeamRestored, "restore_team"
		if archived {
			at, action, event = s.now(), audit.ActionTeamArchived, "delete_team"
		}
		var ids []string
		if err := tx.Model(&Channel{}).Where("team_id = ?", t.ID).Order("id").Pluck("id", &ids).Error; err != nil {
			return err
		}
		for _, id := range ids {
			if _, err := lockChannel(ctx, tx, strings.TrimSpace(id)); err != nil {
				return err
			}
		}
		if err := tx.Model(&Team{}).Where("id = ?", t.ID).Updates(map[string]any{"archived_at": at, "updated_at": s.now()}).Error; err != nil {
			return err
		}
		members, err := teamMemberIDs(ctx, tx, t.ID)
		if err != nil {
			return err
		}
		em.add(event, map[string]any{"team_id": t.ID}, members)
		return audit.Record(ctx, tx, auditEvent(sub, action, "team", t.ID, nil))
	})
	if err != nil {
		return err
	}
	s.publish(ctx, em.events)
	return nil
}

// JoinTeam lets a non-guest join an open team.
func (s *Service) JoinTeam(ctx context.Context, sub Subject, teamID string) error {
	return s.addTeamMember(ctx, sub, teamID, sub.UserID, true)
}

// AddTeamMember lets a team admin add a user (guests included).
func (s *Service) AddTeamMember(ctx context.Context, sub Subject, teamID, userID string) error {
	return s.addTeamMember(ctx, sub, teamID, userID, false)
}

func (s *Service) addTeamMember(ctx context.Context, sub Subject, teamID, userID string, self bool) error {
	var em emitter
	err := s.DB.Gorm.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		t, err := lockTeam(ctx, tx, s.DB.Dialect, teamID)
		if err != nil {
			return err
		}
		callerRole, err := teamRole(ctx, tx, t.ID, sub.UserID)
		if err != nil {
			return err
		}
		if self {
			if callerRole != "" {
				return nil // already a member
			}
			if !CanJoinTeam(sub, t.Type, callerRole) {
				return ErrForbidden
			}
		} else if !CanAdministerTeam(sub, callerRole) {
			return teamReadError(callerRole)
		}
		if t.ArchivedAt != 0 {
			return ErrArchived
		}
		if existing, err := teamRole(ctx, tx, t.ID, userID); err != nil {
			return err
		} else if existing != "" {
			return nil // already a member
		}
		u, err := identity.Store{}.ByID(ctx, tx, userID)
		if err != nil {
			return err
		}
		if u.Status != identity.StatusActive {
			return ErrUserInactive
		}
		if err := addTeamMemberTx(ctx, tx, t, userID, TeamUser, s.now(), &em); err != nil {
			return err
		}
		return audit.Record(ctx, tx, auditEvent(sub, audit.ActionTeamMemberAdded, "team", t.ID, map[string]any{"user_id": userID}))
	})
	if err != nil {
		return err
	}
	s.publish(ctx, em.events)
	return nil
}

// addTeamMemberTx inserts the membership and joins the team's default channel
// (under that channel's lock); the caller holds the team lock.
func addTeamMemberTx(ctx context.Context, tx *gorm.DB, t *Team, userID, role string, now int64, em *emitter) error {
	if err := tx.WithContext(ctx).Create(&TeamMember{TeamID: t.ID, UserID: userID, Role: role, CreatedAt: now}).Error; err != nil {
		return fmt.Errorf("add team member: %w", database.Translate(err))
	}
	if em != nil {
		em.add("added_to_team", map[string]any{"team_id": t.ID, "user_id": userID}, []string{userID})
	}
	var ids []string
	if err := tx.Model(&Channel{}).Where("team_id = ? AND is_default = ?", t.ID, true).Limit(1).Pluck("id", &ids).Error; err != nil {
		return err
	}
	if len(ids) == 0 {
		return nil
	}
	ch, err := lockChannel(ctx, tx, strings.TrimSpace(ids[0]))
	if err != nil {
		return err
	}
	return joinChannelTx(ctx, tx, ch, userID, ChannelUser, now, em)
}

// joinChannelTx adds a member to a locked channel with nothing unread.
func joinChannelTx(ctx context.Context, tx *gorm.DB, ch *Channel, userID, role string, now int64, em *emitter) error {
	if m, err := channelMember(ctx, tx, ch.ID, userID); err != nil || m != nil {
		return err
	}
	if err := tx.WithContext(ctx).Create(&ChannelMember{
		ChannelID: ch.ID, UserID: userID, Role: role, MsgCount: ch.TotalMsgCount, LastViewedAt: now, Visible: true, CreatedAt: now,
	}).Error; err != nil {
		return fmt.Errorf("join channel: %w", database.Translate(err))
	}
	if em != nil {
		members, err := channelMemberIDs(ctx, tx, ch.ID)
		if err != nil {
			return err
		}
		if e := em.add("user_added", map[string]any{"channel_id": ch.ID, "team_id": ch.TeamID, "user_id": userID}, members); e != nil {
			e.ChannelID = ch.ID
		}
	}
	return nil
}

// RemoveTeamMember removes a user from a team (a team admin, or the user
// leaving) and, in the same transaction, from every channel of the team.
func (s *Service) RemoveTeamMember(ctx context.Context, sub Subject, teamID, userID string) error {
	var em emitter
	err := s.DB.Gorm.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		t, err := lockTeam(ctx, tx, s.DB.Dialect, teamID)
		if err != nil {
			return err
		}
		callerRole, err := teamRole(ctx, tx, t.ID, sub.UserID)
		if err != nil {
			return err
		}
		if sub.UserID != userID && !CanAdministerTeam(sub, callerRole) {
			return teamReadError(callerRole)
		}
		role, err := teamRole(ctx, tx, t.ID, userID)
		if err != nil {
			return err
		}
		if role == "" {
			return ErrNotFound
		}
		if t.IsDefault && role == TeamAdmin {
			var admins int64
			if err := tx.Model(&TeamMember{}).Where("team_id = ? AND role = ?", t.ID, TeamAdmin).Count(&admins).Error; err != nil {
				return err
			}
			if admins <= 1 {
				return ErrDefaultTeam
			}
		}
		var chIDs []string
		if err := tx.Table("channel_members m").Joins("JOIN channels c ON c.id = m.channel_id").
			Where("c.team_id = ? AND m.user_id = ?", t.ID, userID).Order("c.id").Pluck("c.id", &chIDs).Error; err != nil {
			return err
		}
		for _, id := range chIDs {
			ch, err := lockChannel(ctx, tx, strings.TrimSpace(id))
			if err != nil {
				return err
			}
			if err := leaveChannelTx(ctx, tx, ch, userID, &em); err != nil {
				return err
			}
		}
		if err := tx.Where("team_id = ? AND user_id = ?", t.ID, userID).Delete(&TeamMember{}).Error; err != nil {
			return err
		}
		em.add("leave_team", map[string]any{"team_id": t.ID, "user_id": userID}, []string{userID})
		return audit.Record(ctx, tx, auditEvent(sub, audit.ActionTeamMemberRemoved, "team", t.ID, map[string]any{"user_id": userID}))
	})
	if err != nil {
		return err
	}
	s.publish(ctx, em.events)
	return nil
}

// leaveChannelTx removes a member from a locked channel and tells the remaining
// members and the removed user.
func leaveChannelTx(ctx context.Context, tx *gorm.DB, ch *Channel, userID string, em *emitter) error {
	if err := tx.WithContext(ctx).Where("channel_id = ? AND user_id = ?", ch.ID, userID).Delete(&ChannelMember{}).Error; err != nil {
		return fmt.Errorf("leave channel: %w", err)
	}
	if em != nil {
		members, err := channelMemberIDs(ctx, tx, ch.ID)
		if err != nil {
			return err
		}
		if e := em.add("user_removed", map[string]any{"channel_id": ch.ID, "team_id": ch.TeamID, "user_id": userID},
			append(members, userID)); e != nil {
			e.ChannelID = ch.ID
		}
	}
	return nil
}

// SetTeamRole changes a member's team role (never the caller's own).
func (s *Service) SetTeamRole(ctx context.Context, sub Subject, teamID, userID, role string) error {
	if role != TeamAdmin && role != TeamUser {
		return ErrInvalidRole
	}
	var em emitter
	err := s.DB.Gorm.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		t, err := lockTeam(ctx, tx, s.DB.Dialect, teamID)
		if err != nil {
			return err
		}
		callerRole, err := teamRole(ctx, tx, t.ID, sub.UserID)
		if err != nil {
			return err
		}
		if !CanAdministerTeam(sub, callerRole) {
			return teamReadError(callerRole)
		}
		if userID == sub.UserID {
			return ErrForbidden
		}
		old, err := teamRole(ctx, tx, t.ID, userID)
		if err != nil {
			return err
		}
		if old == "" {
			return ErrNotFound
		}
		if err := tx.Model(&TeamMember{}).Where("team_id = ? AND user_id = ?", t.ID, userID).Update("role", role).Error; err != nil {
			return err
		}
		var admins []string
		if err := tx.Model(&TeamMember{}).Where("team_id = ? AND role = ?", t.ID, TeamAdmin).Pluck("user_id", &admins).Error; err != nil {
			return err
		}
		em.add("user_role_updated", map[string]any{"team_id": t.ID, "user_id": userID, "role": role}, trimAll(append(admins, userID)))
		return audit.Record(ctx, tx, auditEvent(sub, audit.ActionTeamRoleChanged, "team", t.ID,
			map[string]any{"user_id": userID, "from": old, "to": role}))
	})
	if err != nil {
		return err
	}
	s.publish(ctx, em.events)
	return nil
}

// TeamWithState is a team as listed to one of its members.
type TeamWithState struct {
	Team
	Role         string `json:"role"`
	HasUnread    bool   `json:"has_unread"`
	MentionCount int64  `json:"mention_count"`
}

// MyTeams lists the caller's unarchived teams with unread state.
func (s *Service) MyTeams(ctx context.Context, sub Subject) ([]TeamWithState, error) {
	var rows []TeamWithState
	err := s.DB.Gorm.WithContext(ctx).Table("teams t").
		Select(`t.*, tm.role AS role,
			COALESCE(SUM(CASE WHEN c.total_msg_count > cm.msg_count THEN 1 ELSE 0 END), 0) > 0 AS has_unread,
			COALESCE(SUM(cm.mention_count), 0) AS mention_count`).
		Joins("JOIN team_members tm ON tm.team_id = t.id AND tm.user_id = ?", sub.UserID).
		Joins("LEFT JOIN channels c ON c.team_id = t.id AND c.archived_at = 0").
		Joins("LEFT JOIN channel_members cm ON cm.channel_id = c.id AND cm.user_id = ?", sub.UserID).
		Where("t.archived_at = 0").
		Group("t.id, t.name, t.display_name, t.description, t.type, t.is_default, t.archived_at, t.created_at, t.updated_at, tm.role").
		Order("t.display_name, t.id").Scan(&rows).Error
	if err != nil {
		return nil, fmt.Errorf("list my teams: %w", err)
	}
	for i := range rows {
		rows[i].trimIDs()
	}
	return rows, nil
}

// JoinableTeams lists open, unarchived teams the caller (not a guest) may join.
func (s *Service) JoinableTeams(ctx context.Context, sub Subject, cursor string, limit int) ([]Team, string, error) {
	if sub.Guest() {
		return []Team{}, "", nil
	}
	limit = clampLimit(limit)
	q := s.DB.Gorm.WithContext(ctx).Model(&Team{}).
		Where("type = ? AND archived_at = 0", TeamOpen).
		Where("id NOT IN (SELECT team_id FROM team_members WHERE user_id = ?)", sub.UserID)
	if cursor != "" {
		q = q.Where("id > ?", cursor)
	}
	var rows []Team
	if err := q.Order("id").Limit(limit + 1).Find(&rows).Error; err != nil {
		return nil, "", fmt.Errorf("list joinable teams: %w", err)
	}
	next := ""
	if len(rows) > limit {
		rows = rows[:limit]
		next = strings.TrimSpace(rows[limit-1].ID)
	}
	for i := range rows {
		rows[i].trimIDs()
	}
	return rows, next, nil
}

func clampLimit(n int) int {
	if n <= 0 {
		return 60
	}
	return min(n, 200)
}

func teamMemberIDs(ctx context.Context, tx *gorm.DB, teamID string) ([]string, error) {
	var ids []string
	err := tx.WithContext(ctx).Model(&TeamMember{}).Where("team_id = ?", teamID).Pluck("user_id", &ids).Error
	return trimAll(ids), err
}

func channelMemberIDs(ctx context.Context, tx *gorm.DB, channelID string) ([]string, error) {
	var ids []string
	err := tx.WithContext(ctx).Model(&ChannelMember{}).Where("channel_id = ?", channelID).Pluck("user_id", &ids).Error
	return trimAll(ids), err
}

func trimAll(ids []string) []string {
	for i := range ids {
		ids[i] = strings.TrimSpace(ids[i])
	}
	return ids
}
