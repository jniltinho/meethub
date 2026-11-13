package org

import (
	"context"
	"errors"
	"fmt"
	"regexp"
	"strings"
	"time"

	"github.com/oklog/ulid/v2"
	"gorm.io/gorm"

	"github.com/jniltinho/meethub/internal/config"
	"github.com/jniltinho/meethub/internal/database"
)

// Errors about the organization.
var (
	ErrNoOrganization  = errors.New("the install is not initialized")
	ErrAlreadyExists   = errors.New("the install is already initialized")
	ErrInvalidOrgName  = errors.New("organization name must be 1 to 200 characters")
	ErrNotMember       = errors.New("user is not a member of the organization")
	defaultTheme       = "meethub-dark"
	nonSlugChars       = regexp.MustCompile(`[^a-z0-9]+`)
	maxOrganizationLen = 200
)

// Organization is the single organization of an install.
type Organization struct {
	ID           string    `gorm:"primaryKey;size:26" json:"id"`
	Singleton    int       `json:"-"`
	Slug         string    `json:"slug"`
	Name         string    `json:"name"`
	DefaultTheme string    `json:"default_theme"`
	CreatedAt    time.Time `json:"created_at"`
	UpdatedAt    time.Time `json:"updated_at"`
}

// Membership is a user's role in the organization.
type Membership struct {
	OrgID    string    `gorm:"primaryKey;size:26" json:"org_id"`
	UserID   string    `gorm:"primaryKey;size:26" json:"user_id"`
	Role     string    `json:"role"`
	JoinedAt time.Time `json:"joined_at"`
}

// Slugify derives a URL slug from a name ("Acme Ltd." → "acme-ltd").
func Slugify(name string) string {
	s := strings.Trim(nonSlugChars.ReplaceAllString(strings.ToLower(name), "-"), "-")
	if len(s) > 64 {
		s = strings.TrimRight(s[:64], "-")
	}
	if s == "" {
		s = "org"
	}
	return s
}

// Get returns the organization, or ErrNoOrganization.
func Get(ctx context.Context, db *gorm.DB) (*Organization, error) {
	var o Organization
	if err := db.WithContext(ctx).Take(&o).Error; err != nil {
		if database.IsNotFound(err) {
			return nil, ErrNoOrganization
		}
		return nil, fmt.Errorf("get organization: %w", err)
	}
	o.ID = strings.TrimSpace(o.ID)
	return &o, nil
}

// Create inserts the organization; the singleton index makes a second one fail
// with ErrAlreadyExists on every dialect.
func Create(ctx context.Context, tx *gorm.DB, name string) (*Organization, error) {
	name = strings.TrimSpace(name)
	if n := len([]rune(name)); n == 0 || n > maxOrganizationLen {
		return nil, ErrInvalidOrgName
	}
	now := time.Now().UTC()
	o := &Organization{
		ID: ulid.Make().String(), Singleton: 1, Slug: Slugify(name), Name: name,
		DefaultTheme: defaultTheme, CreatedAt: now, UpdatedAt: now,
	}
	if err := database.Translate(tx.WithContext(ctx).Create(o).Error); err != nil {
		if errors.Is(err, database.ErrDuplicate) {
			return nil, ErrAlreadyExists
		}
		return nil, fmt.Errorf("create organization: %w", err)
	}
	return o, nil
}

// AddMember gives userID a role in the organization.
func AddMember(ctx context.Context, tx *gorm.DB, orgID, userID, role string) error {
	if !ValidRole(role) {
		return ErrInvalidRole
	}
	m := Membership{OrgID: orgID, UserID: userID, Role: role, JoinedAt: time.Now().UTC()}
	if err := tx.WithContext(ctx).Create(&m).Error; err != nil {
		return fmt.Errorf("add member: %w", database.Translate(err))
	}
	return nil
}

// MembershipOf returns userID's membership.
func MembershipOf(ctx context.Context, db *gorm.DB, userID string) (*Membership, error) {
	var m Membership
	if err := db.WithContext(ctx).Where("user_id = ?", userID).Take(&m).Error; err != nil {
		if database.IsNotFound(err) {
			return nil, ErrNotMember
		}
		return nil, fmt.Errorf("get membership: %w", err)
	}
	m.OrgID, m.UserID = strings.TrimSpace(m.OrgID), strings.TrimSpace(m.UserID)
	return &m, nil
}

// lockOrganization takes the organization row lock that serializes every user
// management write (MariaDB/PostgreSQL; SQLite write transactions are already
// exclusive). It is always taken before any user row lock, so lock order is
// fixed and concurrent changes cannot deadlock.
func lockOrganization(ctx context.Context, tx *gorm.DB, dialect string) error {
	if dialect == config.DriverSQLite {
		return nil
	}
	var id string
	if err := tx.WithContext(ctx).Raw(`SELECT id FROM organizations WHERE singleton = 1 FOR UPDATE`).Scan(&id).Error; err != nil {
		return fmt.Errorf("lock organization: %w", err)
	}
	return nil
}

// activeOwners returns the active owners' ids, read under the organization lock.
func activeOwners(ctx context.Context, tx *gorm.DB, dialect, orgID string) ([]string, error) {
	q := `SELECT m.user_id FROM memberships m JOIN users u ON u.id = m.user_id
	      WHERE m.org_id = ? AND m.role = 'owner' AND u.status = 'active'`
	if dialect != config.DriverSQLite {
		q += " FOR UPDATE"
	}
	var ids []string
	if err := tx.WithContext(ctx).Raw(q, orgID).Scan(&ids).Error; err != nil {
		return nil, fmt.Errorf("read owners: %w", err)
	}
	for i := range ids {
		ids[i] = strings.TrimSpace(ids[i])
	}
	return ids, nil
}

// LockOrganization takes the organization row lock in tx (see lockOrganization);
// other packages use it to serialize organization-wide setup such as the
// default chat team.
func LockOrganization(ctx context.Context, tx *gorm.DB, dialect string) error {
	return lockOrganization(ctx, tx, dialect)
}
