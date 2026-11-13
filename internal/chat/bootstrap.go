package chat

import (
	"context"
	"fmt"
	"strings"
	"time"

	"gorm.io/gorm"

	"github.com/jniltinho/meethub/internal/database"
	"github.com/jniltinho/meethub/internal/identity"
	"github.com/jniltinho/meethub/internal/org"
)

// BackfillLockTimeout bounds the wait for another process's backfill.
const BackfillLockTimeout = 5 * time.Minute

// ensureDefaultTeamTx returns the default team, creating it (and its general
// channel) from the organization's name when absent. It takes the organization
// row lock first, so concurrent callers create it once.
func ensureDefaultTeamTx(ctx context.Context, tx *gorm.DB, dialect, orgName string) (*Team, error) {
	if err := org.LockOrganization(ctx, tx, dialect); err != nil {
		return nil, err
	}
	var t Team
	err := tx.WithContext(ctx).Where("is_default = ?", true).Take(&t).Error
	if err == nil {
		t.trimIDs()
		return &t, nil
	}
	if !database.IsNotFound(err) {
		return nil, fmt.Errorf("find default team: %w", err)
	}
	name := org.Slugify(orgName)
	if len(name) < 2 {
		name = "team-" + name
	}
	display, derr := CheckDisplayName(orgName)
	if derr != nil {
		display = "Team"
	}
	now := nowMillis(time.Now())
	t = Team{ID: newID(), Name: name, DisplayName: display, Type: TeamOpen, IsDefault: true, CreatedAt: now, UpdatedAt: now}
	if err := createTeamTx(ctx, tx, &t, "", now); err != nil {
		return nil, err
	}
	return &t, nil
}

// EnsureMemberships puts a new non-guest user in the default team and its
// general channel, inside the caller's transaction (organization → team →
// channel lock order). The first owner becomes the default team's admin.
func EnsureMemberships(ctx context.Context, tx *gorm.DB, dialect, orgName, userID, orgRole string) error {
	if orgRole == org.RoleGuest {
		return nil
	}
	t, err := ensureDefaultTeamTx(ctx, tx, dialect, orgName)
	if err != nil {
		return err
	}
	if _, err := lockTeam(ctx, tx, dialect, t.ID); err != nil {
		return err
	}
	if role, err := teamRole(ctx, tx, t.ID, userID); err != nil || role != "" {
		return err
	}
	role := TeamUser
	var admins int64
	if err := tx.Model(&TeamMember{}).Where("team_id = ? AND role = ?", t.ID, TeamAdmin).Count(&admins).Error; err != nil {
		return err
	}
	if admins == 0 && orgRole == org.RoleOwner {
		role = TeamAdmin
	}
	return addTeamMemberTx(ctx, tx, t, userID, role, nowMillis(time.Now()), nil)
}

// BackfillReport says what Backfill changed.
type BackfillReport struct {
	Usernames   int
	DefaultTeam bool
	Memberships int
}

// Backfill brings an install from before the chat up to date: a username for
// every user, the default team, and default-team memberships for every active
// non-guest user. It is idempotent and resumable, and runs under its own
// cross-process lock so replicas starting together do it once.
func Backfill(ctx context.Context, db *database.DB) (BackfillReport, error) {
	var rep BackfillReport
	err := database.WithSessionLock(ctx, db, "backfill", BackfillLockTimeout, func() error {
		var store identity.Store
		var users []identity.User
		if err := db.Gorm.WithContext(ctx).Where("username IS NULL").Find(&users).Error; err != nil {
			return fmt.Errorf("backfill usernames: %w", err)
		}
		for _, u := range users {
			if _, err := store.AssignUsername(ctx, db.Gorm, strings.TrimSpace(u.ID), u.EmailNormalized); err != nil {
				return err
			}
			rep.Usernames++
		}
		o, err := org.Get(ctx, db.Gorm)
		if err != nil {
			if err == org.ErrNoOrganization { //nolint:errorlint // sentinel returned directly
				return nil // not initialized yet: Initialize will do the rest
			}
			return err
		}
		var before int64
		db.Gorm.Model(&Team{}).Where("is_default = ?", true).Count(&before)
		var missing []struct {
			UserID string
			Role   string
		}
		if err := db.Gorm.WithContext(ctx).Table("memberships m").Select("m.user_id, m.role").
			Joins("JOIN users u ON u.id = m.user_id").
			Where("u.status = ? AND m.role <> ?", identity.StatusActive, org.RoleGuest).
			Where("m.user_id NOT IN (SELECT tm.user_id FROM team_members tm JOIN teams t ON t.id = tm.team_id WHERE t.is_default = ?)", true).
			Order("CASE m.role WHEN 'owner' THEN 0 ELSE 1 END, m.user_id").Scan(&missing).Error; err != nil { // owners first: the first becomes team admin
			return fmt.Errorf("backfill memberships: %w", err)
		}
		for _, m := range missing {
			err := db.Gorm.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
				return EnsureMemberships(ctx, tx, db.Dialect, o.Name, strings.TrimSpace(m.UserID), m.Role)
			})
			if err != nil {
				return err
			}
			rep.Memberships++
		}
		if before == 0 && len(missing) > 0 {
			rep.DefaultTeam = true
		}
		return nil
	})
	return rep, err
}
