package identity_test

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/jniltinho/meethub/internal/database"
	"github.com/jniltinho/meethub/internal/database/dbtest"
	"github.com/jniltinho/meethub/internal/identity"
)

var (
	ctx      = context.Background()
	fastHash = identity.Argon2Params{MemoryKiB: 1024, Time: 1, Threads: 1}
)

func TestNormalizeEmail(t *testing.T) {
	t.Parallel()
	for in, want := range map[string]string{
		"Ana@Example.com":    "ana@example.com",
		"  ana@example.com ": "ana@example.com",
		"josé@x.io":         "josé@x.io", // decomposed → NFC
	} {
		if got, err := identity.NormalizeEmail(in); err != nil || got != want {
			t.Errorf("NormalizeEmail(%q) = %q, %v; want %q", in, got, err, want)
		}
	}
	for _, bad := range []string{"", "nope", "Ana <ana@example.com>", "a@b@c", strings.Repeat("a", 330) + "@x.io"} {
		if _, err := identity.NormalizeEmail(bad); !errors.Is(err, identity.ErrInvalidEmail) {
			t.Errorf("NormalizeEmail(%q) accepted", bad)
		}
	}
}

func TestPasswordHashing(t *testing.T) {
	t.Parallel()
	h := identity.NewHasher(fastHash, 2, time.Second)
	hash, err := h.Hash(ctx, "correct horse battery")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(hash, "$argon2id$v=19$m=1024,t=1,p=1$") {
		t.Fatalf("not a PHC string: %q", hash)
	}
	if err := h.Verify(ctx, hash, "correct horse battery"); err != nil {
		t.Fatalf("verify: %v", err)
	}
	if err := h.Verify(ctx, hash, "wrong horse battery!"); !errors.Is(err, identity.ErrPasswordMismatch) {
		t.Fatalf("wrong password: %v", err)
	}
	if err := h.VerifyDummy(ctx, "anything at all"); !errors.Is(err, identity.ErrPasswordMismatch) {
		t.Fatalf("dummy: %v", err)
	}
	// production parameters verify too (hashes carry their own parameters)
	prod := identity.NewHasher(identity.DefaultArgon2, 1, time.Second)
	if err := prod.Verify(ctx, hash, "correct horse battery"); err != nil {
		t.Fatalf("cross-parameter verify: %v", err)
	}
}

func TestPasswordPolicy(t *testing.T) {
	t.Parallel()
	h := identity.NewHasher(fastHash, 1, time.Second)
	if _, err := h.Hash(ctx, "short"); !errors.Is(err, identity.ErrWeakPassword) {
		t.Fatalf("weak: %v", err)
	}
	huge := strings.Repeat("x", 10_000)
	if _, err := h.Hash(ctx, huge); !errors.Is(err, identity.ErrPasswordTooLong) {
		t.Fatalf("oversized hash: %v", err)
	}
	if err := h.Verify(ctx, "$argon2id$v=19$m=1,t=1,p=1$AA$AA", huge); !errors.Is(err, identity.ErrPasswordTooLong) {
		t.Fatalf("oversized verify must not hash: %v", err)
	}
}

func TestHashingSaturation(t *testing.T) {
	t.Parallel()
	slow := identity.Argon2Params{MemoryKiB: 64 * 1024, Time: 4, Threads: 1}
	h := identity.NewHasher(slow, 1, 10*time.Millisecond)
	var wg sync.WaitGroup
	errs := make([]error, 4)
	for i := range errs {
		wg.Go(func() { _, errs[i] = h.Hash(ctx, "a long enough password") })
	}
	wg.Wait()
	busy := 0
	for _, err := range errs {
		if errors.Is(err, identity.ErrBusy) {
			busy++
		}
	}
	if busy == 0 {
		t.Fatal("no request was turned away while the only slot was busy")
	}
}

func TestUserStore(t *testing.T) {
	dbtest.Each(t, func(t *testing.T, db *database.DB) {
		var s identity.Store
		u := &identity.User{Email: "Ana@Example.com", DisplayName: "Ana 🚀", PasswordHash: "h"}
		if err := s.Create(ctx, db.Gorm, u); err != nil {
			t.Fatal(err)
		}
		if len(u.ID) != 26 || u.Status != identity.StatusActive || u.EmailNormalized != "ana@example.com" {
			t.Fatalf("created %+v", u)
		}
		dup := &identity.User{Email: "ANA@example.com", DisplayName: "Other", PasswordHash: "h"}
		if err := s.Create(ctx, db.Gorm, dup); !errors.Is(err, identity.ErrEmailTaken) {
			t.Fatalf("duplicate: %v", err)
		}
		for _, e := range []string{"jose@example.com", "josé@example.com"} {
			if err := s.Create(ctx, db.Gorm, &identity.User{Email: e, DisplayName: "J", PasswordHash: "h"}); err != nil {
				t.Fatalf("%s: %v", e, err)
			}
		}
		got, err := s.ByEmail(ctx, db.Gorm, " ana@EXAMPLE.com")
		if err != nil || got.ID != u.ID || got.DisplayName != "Ana 🚀" {
			t.Fatalf("ByEmail: %+v %v", got, err)
		}
		if err := s.SetStatus(ctx, db.Gorm, u.ID, identity.StatusDisabled); err != nil {
			t.Fatal(err)
		}
		if _, err := s.ByID(ctx, db.Gorm, "01JZZZZZZZZZZZZZZZZZZZZZZZ"); !errors.Is(err, identity.ErrUserNotFound) {
			t.Fatalf("missing user: %v", err)
		}
	})
}

func TestConcurrentPreferenceFieldsDoNotOverwriteEachOther(t *testing.T) {
	dbtest.Each(t, func(t *testing.T, db *database.DB) {
		var s identity.Store
		u := &identity.User{Email: "p@x.io", DisplayName: "P", PasswordHash: "h"}
		if err := s.Create(ctx, db.Gorm, u); err != nil {
			t.Fatal(err)
		}
		for i := range 10 {
			theme, lang := fmt.Sprintf("theme-%d", i), fmt.Sprintf("lang-%d", i)
			var wg sync.WaitGroup
			wg.Go(func() { _ = s.UpdatePreferences(ctx, db.Gorm, u.ID, &theme, nil) })
			wg.Go(func() { _ = s.UpdatePreferences(ctx, db.Gorm, u.ID, nil, &lang) })
			wg.Wait()
			p, err := s.Preferences(ctx, db.Gorm, u.ID)
			if err != nil || p.Theme != theme || p.Language != lang {
				t.Fatalf("round %d: %+v %v", i, p, err)
			}
		}
	})
}
