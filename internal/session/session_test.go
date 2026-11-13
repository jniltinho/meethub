package session_test

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/jniltinho/meethub/internal/database"
	"github.com/jniltinho/meethub/internal/database/dbtest"
	"github.com/jniltinho/meethub/internal/identity"
	"github.com/jniltinho/meethub/internal/session"
)

var ctx = context.Background()

func newUser(t *testing.T, db *database.DB) string {
	t.Helper()
	u := &identity.User{Email: identity.NewID() + "@x.io", DisplayName: "U", PasswordHash: "h"}
	if err := (identity.Store{}).Create(ctx, db.Gorm, u); err != nil {
		t.Fatal(err)
	}
	return u.ID
}

func TestLifetime(t *testing.T) {
	dbtest.Each(t, func(t *testing.T, db *database.DB) {
		s := session.Store{Idle: 7 * 24 * time.Hour, Absolute: 30 * 24 * time.Hour}
		uid := newUser(t, db)
		t0 := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
		token, sess, err := s.Create(ctx, db.Gorm, uid, "10.0.0.1", "test", t0)
		if err != nil {
			t.Fatal(err)
		}
		if sess.TokenHash == token || len(sess.TokenHash) != 64 {
			t.Fatal("the token itself must not be stored")
		}
		if _, err := s.Lookup(ctx, db.Gorm, token, t0.Add(time.Hour)); err != nil {
			t.Fatalf("fresh session: %v", err)
		}
		if _, err := s.Lookup(ctx, db.Gorm, "not-a-token", t0); !errors.Is(err, session.ErrInvalid) {
			t.Fatalf("unknown token: %v", err)
		}
		// idle expiry
		if _, err := s.Lookup(ctx, db.Gorm, token, t0.Add(8*24*time.Hour)); !errors.Is(err, session.ErrExpired) {
			t.Fatalf("idle: %v", err)
		}
		// used every day, but older than the absolute lifetime
		for day := 1; day <= 29; day++ {
			now := t0.Add(time.Duration(day) * 24 * time.Hour)
			cur, err := s.Lookup(ctx, db.Gorm, token, now)
			if err != nil {
				t.Fatalf("day %d: %v", day, err)
			}
			if err := s.Touch(ctx, db.Gorm, cur, now); err != nil {
				t.Fatal(err)
			}
		}
		if _, err := s.Lookup(ctx, db.Gorm, token, t0.Add(30*24*time.Hour+time.Minute)); !errors.Is(err, session.ErrExpired) {
			t.Fatalf("absolute: %v", err)
		}
	})
}

func TestRefreshCannotReviveRevoked(t *testing.T) {
	dbtest.Each(t, func(t *testing.T, db *database.DB) {
		s := session.Store{Idle: time.Hour, Absolute: 24 * time.Hour}
		uid := newUser(t, db)
		now := time.Now().UTC()
		token, sess, err := s.Create(ctx, db.Gorm, uid, "", "", now.Add(-10*time.Minute))
		if err != nil {
			t.Fatal(err)
		}
		var wg sync.WaitGroup
		wg.Go(func() { _ = s.Touch(ctx, db.Gorm, sess, now) })
		wg.Go(func() { _ = session.Revoke(ctx, db.Gorm, sess.ID, now) })
		wg.Wait()
		_ = s.Touch(ctx, db.Gorm, sess, now.Add(2*time.Minute)) // a late refresh after the revoke
		if _, err := s.Lookup(ctx, db.Gorm, token, now.Add(3*time.Minute)); !errors.Is(err, session.ErrInvalid) {
			t.Fatalf("revoked session came back: %v", err)
		}
	})
}

func TestRevokeAllForUser(t *testing.T) {
	dbtest.Each(t, func(t *testing.T, db *database.DB) {
		s := session.Store{Idle: time.Hour, Absolute: 24 * time.Hour}
		uid, other := newUser(t, db), newUser(t, db)
		now := time.Now().UTC()
		keepTok, keep, _ := s.Create(ctx, db.Gorm, uid, "", "", now)
		goneTok, _, _ := s.Create(ctx, db.Gorm, uid, "", "", now)
		otherTok, _, _ := s.Create(ctx, db.Gorm, other, "", "", now)
		if err := session.RevokeAllForUser(ctx, db.Gorm, uid, keep.ID, now); err != nil {
			t.Fatal(err)
		}
		if _, err := s.Lookup(ctx, db.Gorm, goneTok, now); !errors.Is(err, session.ErrInvalid) {
			t.Fatalf("other session of the user: %v", err)
		}
		for _, tok := range []string{keepTok, otherTok} {
			if _, err := s.Lookup(ctx, db.Gorm, tok, now); err != nil {
				t.Fatalf("kept session: %v", err)
			}
		}
	})
}

func TestCSRFTokenStableAndDistinct(t *testing.T) {
	t.Parallel()
	a, b := session.CSRFToken("token-a"), session.CSRFToken("token-b")
	if a == b || a != session.CSRFToken("token-a") || len(a) < 40 {
		t.Fatalf("csrf tokens: %q %q", a, b)
	}
}
