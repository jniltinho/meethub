// Package identity owns user accounts: e-mail normalization, the password
// policy and Argon2id hashing, and the users table.
package identity

import (
	"context"
	"errors"
	"fmt"
	"net/mail"
	"strings"
	"time"

	"github.com/oklog/ulid/v2"
	"golang.org/x/text/unicode/norm"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"

	"github.com/jniltinho/meethub/internal/database"
)

// User statuses.
const (
	StatusActive   = "active"
	StatusDisabled = "disabled"
)

// Errors returned by the user store.
var (
	ErrEmailTaken     = errors.New("e-mail already in use")
	ErrInvalidEmail   = errors.New("invalid e-mail address")
	ErrInvalidName    = errors.New("display name must be 1 to 200 characters")
	ErrUserNotFound   = errors.New("user not found")
	ErrInvalidStatus  = errors.New("status must be active or disabled")
	maxEmailBytes     = 320
	maxDisplayNameLen = 200
)

// User is an account. PasswordHash never leaves the server.
type User struct {
	ID              string     `gorm:"primaryKey;size:26" json:"id"`
	Email           string     `json:"email"`
	EmailNormalized string     `json:"-"`
	Username        *string    `json:"username"`
	UsernameNorm    *string    `gorm:"column:username_normalized" json:"-"`
	DisplayName     string     `json:"display_name"`
	PasswordHash    string     `json:"-"`
	Status          string     `json:"status"`
	CreatedAt       time.Time  `json:"created_at"`
	UpdatedAt       time.Time  `json:"updated_at"`
	LastLoginAt     *time.Time `json:"last_login_at,omitempty"`
	// LastPictureUpdate is the time (ms) of the current profile picture; 0: none
	LastPictureUpdate int64 `json:"last_picture_update"`
}

// NewID returns a new ULID string.
func NewID() string { return ulid.Make().String() }

// NormalizeEmail trims, applies Unicode NFC and lower-cases an address; it
// returns ErrInvalidEmail for anything that is not a single bare address.
func NormalizeEmail(s string) (string, error) {
	s = norm.NFC.String(strings.TrimSpace(s))
	if s == "" || len(s) > maxEmailBytes {
		return "", ErrInvalidEmail
	}
	addr, err := mail.ParseAddress(s)
	if err != nil || addr.Address != s || addr.Name != "" {
		return "", ErrInvalidEmail
	}
	return strings.ToLower(s), nil
}

// CheckDisplayName validates a display name.
func CheckDisplayName(s string) (string, error) {
	s = strings.TrimSpace(s)
	if n := len([]rune(s)); n == 0 || n > maxDisplayNameLen {
		return "", ErrInvalidName
	}
	return s, nil
}

// Store reads and writes users. Every method takes the *gorm.DB to use, so
// callers can run it inside their transaction.
type Store struct{}

// Create inserts u (ID, timestamps and normalized e-mail are filled in).
func (Store) Create(ctx context.Context, tx *gorm.DB, u *User) error {
	email, err := NormalizeEmail(u.Email)
	if err != nil {
		return err
	}
	name, err := CheckDisplayName(u.DisplayName)
	if err != nil {
		return err
	}
	now := time.Now().UTC()
	u.Email, u.EmailNormalized, u.DisplayName = strings.TrimSpace(u.Email), email, name
	if u.ID == "" {
		u.ID = NewID()
	}
	if u.Status == "" {
		u.Status = StatusActive
	}
	u.CreatedAt, u.UpdatedAt = now, now
	var wanted string
	if u.Username != nil && *u.Username != "" {
		n, err := NormalizeUsername(*u.Username)
		if err != nil {
			return err
		}
		wanted = n
	}
	u.Username, u.UsernameNorm = nil, nil
	if err := database.Translate(tx.WithContext(ctx).Create(u).Error); err != nil {
		if errors.Is(err, database.ErrDuplicate) {
			return ErrEmailTaken
		}
		return fmt.Errorf("create user: %w", err)
	}
	var username string
	if wanted != "" {
		username, err = Store{}.SetUsername(ctx, tx, u.ID, wanted)
	} else {
		username, err = Store{}.AssignUsername(ctx, tx, u.ID, u.EmailNormalized)
	}
	if err != nil {
		return err
	}
	u.Username, u.UsernameNorm = &username, &username
	return nil
}

// ByEmail finds a user by any spelling of their e-mail.
func (Store) ByEmail(ctx context.Context, tx *gorm.DB, email string) (*User, error) {
	norm, err := NormalizeEmail(email)
	if err != nil {
		return nil, ErrUserNotFound
	}
	return first(tx.WithContext(ctx).Where("email_normalized = ?", norm))
}

// ByID finds a user by ID.
func (Store) ByID(ctx context.Context, tx *gorm.DB, id string) (*User, error) {
	return first(tx.WithContext(ctx).Where("id = ?", id))
}

func first(q *gorm.DB) (*User, error) {
	var u User
	if err := q.Take(&u).Error; err != nil {
		if database.IsNotFound(err) {
			return nil, ErrUserNotFound
		}
		return nil, fmt.Errorf("find user: %w", err)
	}
	return &u, nil
}

// SetPassword stores a new password hash.
func (Store) SetPassword(ctx context.Context, tx *gorm.DB, id, hash string) error {
	return update(ctx, tx, id, map[string]any{"password_hash": hash})
}

// SetStatus sets active or disabled.
func (Store) SetStatus(ctx context.Context, tx *gorm.DB, id, status string) error {
	if status != StatusActive && status != StatusDisabled {
		return ErrInvalidStatus
	}
	return update(ctx, tx, id, map[string]any{"status": status})
}

// SetDisplayName changes the display name.
func (Store) SetDisplayName(ctx context.Context, tx *gorm.DB, id, name string) error {
	name, err := CheckDisplayName(name)
	if err != nil {
		return err
	}
	return update(ctx, tx, id, map[string]any{"display_name": name})
}

// TouchLogin records a successful login.
func (Store) TouchLogin(ctx context.Context, tx *gorm.DB, id string) error {
	return update(ctx, tx, id, map[string]any{"last_login_at": time.Now().UTC()})
}

// Count returns the number of users.
func (Store) Count(ctx context.Context, tx *gorm.DB) (int64, error) {
	var n int64
	err := tx.WithContext(ctx).Model(&User{}).Count(&n).Error
	return n, err
}

func update(ctx context.Context, tx *gorm.DB, id string, fields map[string]any) error {
	fields["updated_at"] = time.Now().UTC()
	res := tx.WithContext(ctx).Model(&User{}).Where("id = ?", id).Updates(fields)
	if res.Error != nil {
		return fmt.Errorf("update user: %w", res.Error)
	}
	if res.RowsAffected == 0 {
		return ErrUserNotFound
	}
	return nil
}

// LockByID reads the user with a row lock held until the transaction ends
// (SELECT … FOR UPDATE on MariaDB/PostgreSQL; SQLite write transactions are
// already exclusive). Credential, role and status changes and session issuance
// read the user this way, so they serialize on the user's row.
func (Store) LockByID(ctx context.Context, tx *gorm.DB, dialect, id string) (*User, error) {
	q := tx.WithContext(ctx).Where("id = ?", id)
	if dialect != "sqlite" {
		q = q.Clauses(clause.Locking{Strength: "UPDATE"})
	}
	return first(q)
}
