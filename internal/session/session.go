// Package session stores login sessions server-side. The browser holds a
// random token; only its SHA-256 is stored, so a database leak yields no
// usable session. Sessions end on idle timeout, absolute lifetime or
// revocation, checked on every request.
package session

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"fmt"
	"time"

	"github.com/oklog/ulid/v2"
	"gorm.io/gorm"

	"github.com/jniltinho/meethub/internal/database"
)

// Errors returned by Lookup.
var (
	ErrInvalid = errors.New("unknown or revoked session")
	ErrExpired = errors.New("session expired")
)

// touchInterval limits activity writes to one per minute per session.
const touchInterval = time.Minute

// Session is a stored session (never the token itself).
type Session struct {
	ID         string `gorm:"primaryKey;size:26"`
	UserID     string
	TokenHash  string
	CreatedAt  time.Time
	LastSeenAt time.Time
	ExpiresAt  time.Time // absolute lifetime
	RevokedAt  *time.Time
	ClientIP   string
	UserAgent  string
}

// Store creates, finds and ends sessions.
type Store struct {
	Idle     time.Duration
	Absolute time.Duration
}

// HashToken returns the stored form of a token.
func HashToken(token string) string {
	sum := sha256.Sum256([]byte(token))
	return hex.EncodeToString(sum[:])
}

// CSRFToken derives the session's CSRF token from its secret token, so it is
// stable for the session's life and needs no storage.
func CSRFToken(token string) string {
	sum := sha256.Sum256([]byte("meethub-csrf:" + token))
	return base64.RawURLEncoding.EncodeToString(sum[:])
}

// Create starts a new session for userID and returns its secret token.
func (s Store) Create(ctx context.Context, tx *gorm.DB, userID, clientIP, userAgent string, now time.Time) (string, *Session, error) {
	raw := make([]byte, 32)
	if _, err := rand.Read(raw); err != nil {
		return "", nil, err
	}
	token := base64.RawURLEncoding.EncodeToString(raw)
	now = now.UTC()
	if len(userAgent) > 255 {
		userAgent = userAgent[:255]
	}
	sess := &Session{
		ID: ulid.Make().String(), UserID: userID, TokenHash: HashToken(token),
		CreatedAt: now, LastSeenAt: now, ExpiresAt: now.Add(s.Absolute),
		ClientIP: clientIP, UserAgent: userAgent,
	}
	if err := tx.WithContext(ctx).Create(sess).Error; err != nil {
		return "", nil, fmt.Errorf("create session: %w", err)
	}
	return token, sess, nil
}

// Lookup finds the live session for token, or ErrInvalid / ErrExpired.
func (s Store) Lookup(ctx context.Context, db *gorm.DB, token string, now time.Time) (*Session, error) {
	if token == "" {
		return nil, ErrInvalid
	}
	var sess Session
	if err := db.WithContext(ctx).Where("token_hash = ?", HashToken(token)).Take(&sess).Error; err != nil {
		if database.IsNotFound(err) {
			return nil, ErrInvalid
		}
		return nil, fmt.Errorf("find session: %w", err)
	}
	now = now.UTC()
	switch {
	case sess.RevokedAt != nil:
		return nil, ErrInvalid
	case !now.Before(sess.ExpiresAt), now.Sub(sess.LastSeenAt) >= s.Idle:
		return nil, ErrExpired
	}
	return &sess, nil
}

// Touch records activity, at most once per minute. The update only applies to
// a live session, so it can never revive one revoked or expired meanwhile.
func (s Store) Touch(ctx context.Context, db *gorm.DB, sess *Session, now time.Time) error {
	now = now.UTC()
	if now.Sub(sess.LastSeenAt) < touchInterval {
		return nil
	}
	return db.WithContext(ctx).Exec(
		`UPDATE sessions SET last_seen_at = ? WHERE id = ? AND revoked_at IS NULL AND expires_at > ? AND last_seen_at > ?`,
		now, sess.ID, now, now.Add(-s.Idle)).Error
}

// Revoke ends one session.
func Revoke(ctx context.Context, tx *gorm.DB, id string, now time.Time) error {
	return tx.WithContext(ctx).Exec(`UPDATE sessions SET revoked_at = ? WHERE id = ? AND revoked_at IS NULL`, now.UTC(), id).Error
}

// RevokeAllForUser ends every session of userID except keepID (may be empty).
func RevokeAllForUser(ctx context.Context, tx *gorm.DB, userID, keepID string, now time.Time) error {
	return tx.WithContext(ctx).Exec(`UPDATE sessions SET revoked_at = ? WHERE user_id = ? AND id <> ? AND revoked_at IS NULL`,
		now.UTC(), userID, keepID).Error
}
