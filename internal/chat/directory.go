package chat

import (
	"context"
	"fmt"
	"strings"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"

	"github.com/jniltinho/meethub/internal/database"
	"github.com/jniltinho/meethub/internal/identity"
)

// TeamInfo is a team with the caller's role ("" when not a member).
type TeamInfo struct {
	Team
	Role string `json:"role,omitempty"`
}

// GetTeam returns a team its members (and system administrators) may see;
// non-guests also see open teams they may join. Others get 404.
func (s *Service) GetTeam(ctx context.Context, sub Subject, teamID string) (*TeamInfo, error) {
	tx := s.DB.Gorm.WithContext(ctx)
	var t Team
	if err := tx.Where("id = ?", teamID).Take(&t).Error; err != nil {
		if database.IsNotFound(err) {
			return nil, ErrNotFound
		}
		return nil, fmt.Errorf("read team: %w", err)
	}
	t.trimIDs()
	role, err := teamRole(ctx, tx, t.ID, sub.UserID)
	if err != nil {
		return nil, err
	}
	if role == "" && !sub.System() && (sub.Guest() || t.Type != TeamOpen || t.ArchivedAt != 0) {
		return nil, ErrNotFound
	}
	return &TeamInfo{Team: t, Role: role}, nil
}

// TeamMembers lists a team's members (to its members and system
// administrators), ordered by username.
func (s *Service) TeamMembers(ctx context.Context, sub Subject, teamID, cursor string, limit int) ([]Member, string, error) {
	tx := s.DB.Gorm.WithContext(ctx)
	role, err := teamRole(ctx, tx, teamID, sub.UserID)
	if err != nil {
		return nil, "", err
	}
	if role == "" && !sub.System() {
		return nil, "", ErrNotFound
	}
	q := tx.Table("team_members m").Select("u.id, u.username, u.display_name, u.status, u.last_picture_update, m.role").
		Joins("JOIN users u ON u.id = m.user_id").Where("m.team_id = ?", teamID)
	return memberPage(q, cursor, limit)
}

func memberPage(q *gorm.DB, cursor string, limit int) ([]Member, string, error) {
	limit = clampLimit(limit)
	if cursor != "" {
		q = q.Where("u.username_normalized > ?", cursor)
	}
	var rows []struct {
		ID, DisplayName, Status, Role string
		Username                      *string
		LastPictureUpdate             int64
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
		u := identity.User{ID: r.ID, DisplayName: r.DisplayName, Status: r.Status, Username: r.Username, LastPictureUpdate: r.LastPictureUpdate}
		out[i] = Member{PublicUser: u.Public(), Role: r.Role}
	}
	return out, next, nil
}

// DirectoryQuery filters the user directory.
type DirectoryQuery struct {
	Query     string // username or display name; prefix matches first
	InTeam    string // the caller must be a member
	InChannel string // the caller must be able to read it
	Cursor    string // without Query: the last username of the previous page
	Limit     int
}

// Users searches the active users the caller may see (non-guests: everyone;
// guests: people they share a channel or conversation with), as public
// profiles. With a Query, prefix matches come first and there is one page.
func (s *Service) Users(ctx context.Context, sub Subject, f DirectoryQuery) ([]identity.PublicUser, string, error) {
	tx := s.DB.Gorm.WithContext(ctx)
	limit := clampLimit(f.Limit)
	q := tx.Table("users u").Select("u.id, u.username, u.display_name, u.status, u.last_picture_update").
		Where("u.status = ? AND u.username_normalized IS NOT NULL", identity.StatusActive)
	if sub.Guest() {
		q = q.Where(`u.id = ? OR u.id IN (SELECT y.user_id FROM channel_members x
			JOIN channel_members y ON y.channel_id = x.channel_id WHERE x.user_id = ?)`, sub.UserID, sub.UserID)
	}
	if f.InTeam != "" {
		role, err := teamRole(ctx, tx, f.InTeam, sub.UserID)
		if err != nil {
			return nil, "", err
		}
		if role == "" && !sub.System() {
			return nil, "", ErrNotFound
		}
		q = q.Where("u.id IN (SELECT user_id FROM team_members WHERE team_id = ?)", f.InTeam)
	}
	if f.InChannel != "" {
		ch, err := readChannel(ctx, tx, f.InChannel)
		if err != nil {
			return nil, "", err
		}
		a, _, err := accessTx(ctx, tx, sub, ch)
		if err != nil {
			return nil, "", err
		}
		if !CanRead(sub, a) {
			return nil, "", ErrNotFound
		}
		q = q.Where("u.id IN (SELECT user_id FROM channel_members WHERE channel_id = ?)", ch.ID)
	}
	type row struct {
		ID, DisplayName, Status string
		Username                *string
		LastPictureUpdate       int64
	}
	var rows []row
	next := ""
	if term := strings.ToLower(strings.TrimSpace(strings.TrimPrefix(strings.TrimSpace(f.Query), "@"))); term != "" {
		esc := strings.NewReplacer("!", "!!", "%", "!%", "_", "!_").Replace(term) // ESCAPE '!' is portable (MariaDB treats '\' as an escape in literals)
		q = q.Where(`(u.username_normalized LIKE ? ESCAPE '!' OR LOWER(u.display_name) LIKE ? ESCAPE '!')`, esc+"%", "%"+esc+"%").
			Order(clause.OrderBy{Expression: clause.Expr{
				SQL:  `CASE WHEN u.username_normalized LIKE ? ESCAPE '!' OR LOWER(u.display_name) LIKE ? ESCAPE '!' THEN 0 ELSE 1 END`,
				Vars: []any{esc + "%", esc + "%"},
			}}).
			Order("u.username_normalized")
		if err := q.Limit(limit).Scan(&rows).Error; err != nil {
			return nil, "", fmt.Errorf("search users: %w", err)
		}
	} else {
		if f.Cursor != "" {
			q = q.Where("u.username_normalized > ?", f.Cursor)
		}
		if err := q.Order("u.username_normalized").Limit(limit + 1).Scan(&rows).Error; err != nil {
			return nil, "", fmt.Errorf("list users: %w", err)
		}
		if len(rows) > limit {
			rows = rows[:limit]
			if u := rows[limit-1].Username; u != nil {
				next = *u
			}
		}
	}
	out := make([]identity.PublicUser, len(rows))
	for i, r := range rows {
		u := identity.User{ID: r.ID, DisplayName: r.DisplayName, Status: r.Status, Username: r.Username, LastPictureUpdate: r.LastPictureUpdate}
		out[i] = u.Public()
	}
	return out, next, nil
}

// UsersByIDs returns the public profiles of up to 200 users the caller may
// see, disabled ones included (authors of existing messages): non-guests see
// everyone; guests see people they share a channel or conversation with and
// the authors of messages in their channels. Unknown or invisible ids are omitted.
func (s *Service) UsersByIDs(ctx context.Context, sub Subject, ids []string) ([]identity.PublicUser, error) {
	if len(ids) > MaxPerPage {
		return nil, ErrInvalidInput
	}
	if len(ids) == 0 {
		return []identity.PublicUser{}, nil
	}
	q := s.DB.Gorm.WithContext(ctx).Table("users u").Select("u.id, u.username, u.display_name, u.status, u.last_picture_update").Where("u.id IN ?", ids)
	if sub.Guest() {
		q = q.Where(`u.id = ? OR u.id IN (SELECT y.user_id FROM channel_members x
			JOIN channel_members y ON y.channel_id = x.channel_id WHERE x.user_id = ?)
			OR u.id IN (SELECT p.user_id FROM posts p JOIN channel_members m ON m.channel_id = p.channel_id WHERE m.user_id = ?)`,
			sub.UserID, sub.UserID, sub.UserID)
	}
	var rows []struct {
		ID, DisplayName, Status string
		Username                *string
		LastPictureUpdate       int64
	}
	if err := q.Scan(&rows).Error; err != nil {
		return nil, fmt.Errorf("users by ids: %w", err)
	}
	out := make([]identity.PublicUser, len(rows))
	for i, r := range rows {
		u := identity.User{ID: r.ID, DisplayName: r.DisplayName, Status: r.Status, Username: r.Username, LastPictureUpdate: r.LastPictureUpdate}
		out[i] = u.Public()
	}
	return out, nil
}
