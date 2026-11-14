package org

import (
	"context"
	"encoding/base64"
	"errors"
	"fmt"
	"strings"
	"time"

	"gorm.io/gorm"

	"github.com/jniltinho/meethub/internal/audit"
	"github.com/jniltinho/meethub/internal/database"
	"github.com/jniltinho/meethub/internal/identity"
	"github.com/jniltinho/meethub/internal/session"
)

// ErrBadCursor is returned for an undecodable list cursor.
var ErrBadCursor = errors.New("invalid cursor")

// Users manages accounts on behalf of an actor, enforcing the permission
// matrix and the last-owner guard, auditing every change in its transaction.
type Users struct {
	DB     *database.DB
	Hasher *identity.Hasher
	// OnCreate runs inside the transaction that creates a user (the chat adds
	// them to the default team); nil means nothing to do.
	OnCreate func(ctx context.Context, tx *gorm.DB, userID, role string) error
	store    identity.Store
}

// Member is a user with their role, as listed to administrators.
type Member struct {
	identity.User
	Role string `json:"role"`
}

// NewUser is the input of Create.
type NewUser struct {
	Email       string
	DisplayName string
	Role        string
	Password    string
}

// Patch lists the fields an administrator may change; nil means unchanged.
type Patch struct {
	DisplayName *string
	Role        *string
	Status      *string
}

func actorPtr(a Actor) *string {
	if a.System || a.UserID == "" {
		return nil
	}
	id := a.UserID
	return &id
}

// Create adds a user with a role and an initial password.
func (s *Users) Create(ctx context.Context, actor Actor, in NewUser) (*Member, error) {
	if err := actor.CanManage("", in.Role); err != nil {
		return nil, err
	}
	hash, err := s.Hasher.Hash(ctx, in.Password)
	if err != nil {
		return nil, err
	}
	var out *Member
	err = s.DB.Gorm.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		o, err := Get(ctx, tx)
		if err != nil {
			return err
		}
		u := &identity.User{Email: in.Email, DisplayName: in.DisplayName, PasswordHash: hash}
		if err := s.store.Create(ctx, tx, u); err != nil {
			return err
		}
		if err := AddMember(ctx, tx, o.ID, u.ID, in.Role); err != nil {
			return err
		}
		if s.OnCreate != nil {
			if err := s.OnCreate(ctx, tx, u.ID, in.Role); err != nil {
				return err
			}
		}
		out = &Member{User: *u, Role: in.Role}
		return audit.Record(ctx, tx, audit.Event{
			OrgID: &o.ID, ActorID: actorPtr(actor), Action: audit.ActionUserCreated,
			TargetType: "user", TargetID: u.ID, ClientIP: actor.ClientIP, Details: map[string]any{"role": in.Role},
		})
	})
	if err != nil {
		return nil, err
	}
	return out, nil
}

// Update applies an administrator's patch to targetID.
func (s *Users) Update(ctx context.Context, actor Actor, targetID string, p Patch) (*Member, error) {
	if p.Status != nil && *p.Status != identity.StatusActive && *p.Status != identity.StatusDisabled {
		return nil, identity.ErrInvalidStatus
	}
	var out *Member
	err := s.DB.Gorm.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		// lock first, then read: authorization and the owner invariant use the current state
		target, m, err := s.lockAndLoad(ctx, tx, targetID)
		if err != nil {
			return err
		}
		newRole := ""
		if p.Role != nil && *p.Role != m.Role {
			newRole = *p.Role
		}
		if err := actor.CanManage(m.Role, newRole); err != nil {
			return err
		}
		losesOwner := m.Role == RoleOwner && target.Status == identity.StatusActive &&
			(newRole != "" || (p.Status != nil && *p.Status == identity.StatusDisabled))
		if losesOwner {
			owners, err := activeOwners(ctx, tx, s.DB.Dialect, m.OrgID)
			if err != nil {
				return err
			}
			if len(owners) <= 1 {
				return ErrLastOwner
			}
		}
		now := time.Now().UTC()
		ev := func(action string, details map[string]any) error {
			return audit.Record(ctx, tx, audit.Event{
				OrgID: &m.OrgID, ActorID: actorPtr(actor), Action: action,
				TargetType: "user", TargetID: targetID, ClientIP: actor.ClientIP, Details: details,
			})
		}
		if p.DisplayName != nil && *p.DisplayName != target.DisplayName {
			if err := s.store.SetDisplayName(ctx, tx, targetID, *p.DisplayName); err != nil {
				return err
			}
			if err := ev(audit.ActionUserUpdated, map[string]any{"field": "display_name"}); err != nil {
				return err
			}
		}
		if newRole != "" {
			if err := tx.Exec(`UPDATE memberships SET role = ? WHERE org_id = ? AND user_id = ?`, newRole, m.OrgID, targetID).Error; err != nil {
				return fmt.Errorf("change role: %w", err)
			}
			if err := ev(audit.ActionRoleChanged, map[string]any{"from": m.Role, "to": newRole}); err != nil {
				return err
			}
			m.Role = newRole
		}
		if p.Status != nil && *p.Status != target.Status {
			if err := s.store.SetStatus(ctx, tx, targetID, *p.Status); err != nil {
				return err
			}
			action := audit.ActionUserEnabled
			if *p.Status == identity.StatusDisabled {
				action = audit.ActionUserDisabled
				if err := session.RevokeAllForUser(ctx, tx, targetID, "", now); err != nil {
					return err
				}
			}
			if err := ev(action, nil); err != nil {
				return err
			}
		}
		u, err := s.store.ByID(ctx, tx, targetID)
		if err != nil {
			return err
		}
		out = &Member{User: *u, Role: m.Role}
		return nil
	})
	if err != nil {
		return nil, err
	}
	return out, nil
}

// ResetPassword sets a new password for targetID and ends all their sessions.
func (s *Users) ResetPassword(ctx context.Context, actor Actor, targetID, password string) error {
	hash, err := s.Hasher.Hash(ctx, password)
	if err != nil {
		return err
	}
	return s.DB.Gorm.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		_, m, err := s.lockAndLoad(ctx, tx, targetID)
		if err != nil {
			return err
		}
		if err := actor.CanManage(m.Role, ""); err != nil {
			return err
		}
		if err := s.store.SetPassword(ctx, tx, targetID, hash); err != nil {
			return err
		}
		if err := session.RevokeAllForUser(ctx, tx, targetID, "", time.Now()); err != nil {
			return err
		}
		return audit.Record(ctx, tx, audit.Event{
			OrgID: &m.OrgID, ActorID: actorPtr(actor), Action: audit.ActionPasswordReset,
			TargetType: "user", TargetID: targetID, ClientIP: actor.ClientIP,
		})
	})
}

// RemovePicture removes another user's profile picture (moderation), audited.
func (s *Users) RemovePicture(ctx context.Context, actor Actor, targetID string) error {
	return s.DB.Gorm.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		_, m, err := s.lockAndLoad(ctx, tx, targetID)
		if err != nil {
			return err
		}
		if err := actor.CanManage(m.Role, ""); err != nil {
			return err
		}
		if err := identity.RemovePicture(ctx, tx, targetID); err != nil {
			return err
		}
		return audit.Record(ctx, tx, audit.Event{
			OrgID: &m.OrgID, ActorID: actorPtr(actor), Action: audit.ActionPictureRemoved,
			TargetType: "user", TargetID: targetID, ClientIP: actor.ClientIP,
		})
	})
}

// lockAndLoad takes the organization lock, then the user's row lock, and reads
// the user and membership as they are now.
func (s *Users) lockAndLoad(ctx context.Context, tx *gorm.DB, id string) (*identity.User, *Membership, error) {
	if err := lockOrganization(ctx, tx, s.DB.Dialect); err != nil {
		return nil, nil, err
	}
	u, err := s.store.LockByID(ctx, tx, s.DB.Dialect, id)
	if err != nil {
		return nil, nil, err
	}
	m, err := MembershipOf(ctx, tx, id)
	if err != nil {
		return nil, nil, err
	}
	return u, m, nil
}

// ListFilter selects users; zero values mean any.
type ListFilter struct {
	Role   string
	Status string
	Query  string // matches name or e-mail
	Limit  int
	Cursor string
}

// ListPage is one page of members ordered by display name, then id.
type ListPage struct {
	Users      []Member `json:"users"`
	NextCursor string   `json:"next_cursor,omitempty"`
}

// List returns members for an actor with manage_users.
func (s *Users) List(ctx context.Context, actor Actor, f ListFilter) (*ListPage, error) {
	if err := actor.Require(PermManageUsers); err != nil {
		return nil, err
	}
	limit := f.Limit
	if limit <= 0 {
		limit = 50
	}
	limit = min(limit, 200)
	q := s.DB.Gorm.WithContext(ctx).Table("users u").
		Select("u.*, m.role AS role").Joins("JOIN memberships m ON m.user_id = u.id")
	if f.Role != "" {
		q = q.Where("m.role = ?", f.Role)
	}
	if f.Status != "" {
		q = q.Where("u.status = ?", f.Status)
	}
	if f.Query != "" {
		like := "%" + strings.ToLower(strings.NewReplacer("%", "", "_", "").Replace(f.Query)) + "%"
		q = q.Where("(LOWER(u.display_name) LIKE ? OR u.email_normalized LIKE ?)", like, like)
	}
	if f.Cursor != "" {
		name, id, err := decodeListCursor(f.Cursor)
		if err != nil {
			return nil, err
		}
		q = q.Where("(u.display_name > ? OR (u.display_name = ? AND u.id > ?))", name, name, id)
	}
	var rows []Member
	if err := q.Order("u.display_name ASC").Order("u.id ASC").Limit(limit + 1).Scan(&rows).Error; err != nil {
		return nil, fmt.Errorf("list users: %w", err)
	}
	page := &ListPage{Users: rows}
	if len(rows) > limit {
		page.Users = rows[:limit]
		last := page.Users[limit-1]
		page.NextCursor = base64.RawURLEncoding.EncodeToString([]byte(last.DisplayName + "\x00" + strings.TrimSpace(last.ID)))
	}
	for i := range page.Users {
		page.Users[i].ID = strings.TrimSpace(page.Users[i].ID)
	}
	return page, nil
}

func decodeListCursor(c string) (string, string, error) {
	b, err := base64.RawURLEncoding.DecodeString(c)
	if err != nil {
		return "", "", ErrBadCursor
	}
	name, id, ok := strings.Cut(string(b), "\x00")
	if !ok || len(id) != 26 {
		return "", "", ErrBadCursor
	}
	return name, id, nil
}
