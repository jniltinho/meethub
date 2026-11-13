package auth_test

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/redis/go-redis/v9"

	"github.com/jniltinho/meethub/internal/audit"
	"github.com/jniltinho/meethub/internal/auth"
	"github.com/jniltinho/meethub/internal/database"
	"github.com/jniltinho/meethub/internal/database/dbtest"
	"github.com/jniltinho/meethub/internal/identity"
	"github.com/jniltinho/meethub/internal/org"
	"github.com/jniltinho/meethub/internal/session"
	"github.com/jniltinho/meethub/internal/testenv"
)

var ctx = context.Background()

// countingHasher counts verifications to prove failed logins do the same work.
type countingHasher struct {
	*identity.Hasher
	verifies atomic.Int32
}

func (h *countingHasher) Verify(ctx context.Context, enc, pw string) error {
	h.verifies.Add(1)
	return h.Hasher.Verify(ctx, enc, pw)
}

func (h *countingHasher) VerifyDummy(ctx context.Context, pw string) error {
	h.verifies.Add(1)
	return h.Hasher.VerifyDummy(ctx, pw)
}

func newService(t *testing.T, db *database.DB) (*auth.Service, *countingHasher) {
	t.Helper()
	h := &countingHasher{Hasher: identity.NewHasher(identity.Argon2Params{MemoryKiB: 1024, Time: 1, Threads: 1}, 8, time.Second)}
	return &auth.Service{
		DB: db, Hasher: h, Limiter: auth.NewLimiter(nil, nil, 0), DataDir: t.TempDir(),
		Sessions: session.Store{Idle: time.Hour, Absolute: 24 * time.Hour},
	}, h
}

var owner = auth.InitInput{OrganizationName: "Acme", DisplayName: "Ana", Email: "ana@acme.io", Password: "a long password!"}

func TestSetupToken(t *testing.T) {
	dbtest.Each(t, func(t *testing.T, db *database.DB) {
		s, _ := newService(t, db)
		token, generated, err := s.PrepareSetup(ctx, "")
		if err != nil || !generated || len(token) < 40 {
			t.Fatalf("prepare: %q %v %v", token, generated, err)
		}
		file := filepath.Join(s.DataDir, auth.SetupTokenFile)
		st, err := os.Stat(file)
		if err != nil || st.Mode().Perm() != 0o600 {
			t.Fatalf("token file: %v %v", st, err)
		}
		for _, bad := range []string{"", "wrong"} {
			if _, err := s.Setup(ctx, bad, owner, "1.2.3.4", "ua"); !errors.Is(err, auth.ErrInvalidSetupToken) {
				t.Fatalf("token %q: %v", bad, err)
			}
		}
		if ok, _ := s.Initialized(ctx); ok {
			t.Fatal("a rejected setup created something")
		}
		res, err := s.Setup(ctx, token, owner, "1.2.3.4", "ua")
		if err != nil || res.Token == "" {
			t.Fatalf("setup: %v", err)
		}
		if _, err := os.Stat(file); !errors.Is(err, os.ErrNotExist) {
			t.Fatal("setup token file not deleted")
		}
		if _, err := s.Setup(ctx, token, owner, "1.2.3.4", "ua"); !errors.Is(err, auth.ErrAlreadyInitialized) {
			t.Fatalf("setup after initialization: %v", err)
		}
		m, err := org.MembershipOf(ctx, db.Gorm, res.User.ID)
		if err != nil || m.Role != org.RoleOwner {
			t.Fatalf("owner membership: %+v %v", m, err)
		}
		ev, _ := audit.List(ctx, db.Gorm, audit.Filter{Action: audit.ActionSystemInitialized})
		if len(ev.Events) != 1 {
			t.Fatal("system.initialized not audited")
		}
	})
}

func TestConcurrentSetup(t *testing.T) {
	dbtest.Each(t, func(t *testing.T, db *database.DB) {
		s, _ := newService(t, db)
		token, _, _ := s.PrepareSetup(ctx, "fixed-token-from-env")
		if token != "fixed-token-from-env" {
			t.Fatal("configured token not used")
		}
		var wg sync.WaitGroup
		errs := make([]error, 4)
		for i := range errs {
			in := owner
			in.Email = string(rune('a'+i)) + "@acme.io"
			wg.Go(func() { _, errs[i] = s.Setup(ctx, token, in, "", "") })
		}
		wg.Wait()
		ok := 0
		for _, err := range errs {
			if err == nil {
				ok++
			} else if !errors.Is(err, auth.ErrAlreadyInitialized) && !errors.Is(err, auth.ErrInvalidSetupToken) {
				t.Fatalf("unexpected: %v", err)
			}
		}
		var orgs, users int64
		db.Gorm.Raw(`SELECT COUNT(*) FROM organizations`).Scan(&orgs)
		db.Gorm.Raw(`SELECT COUNT(*) FROM users`).Scan(&users)
		if ok != 1 || orgs != 1 || users != 1 {
			t.Fatalf("%d succeeded, %d orgs, %d users", ok, orgs, users)
		}
	})
}

func TestSetupPartialFailureRollsBack(t *testing.T) {
	dbtest.Each(t, func(t *testing.T, db *database.DB) {
		s, _ := newService(t, db)
		token, _, _ := s.PrepareSetup(ctx, "")
		bad := owner
		bad.Email = "not-an-email" // the owner insert fails after the organization row
		if _, err := s.Setup(ctx, token, bad, "", ""); !errors.Is(err, identity.ErrInvalidEmail) {
			t.Fatalf("setup: %v", err)
		}
		if ok, _ := s.Initialized(ctx); ok {
			t.Fatal("organization left behind after a failed setup")
		}
		if _, err := s.Setup(ctx, token, owner, "", ""); err != nil {
			t.Fatalf("retry after rollback: %v", err)
		}
	})
}

func TestLogin(t *testing.T) {
	dbtest.Each(t, func(t *testing.T, db *database.DB) {
		s, h := newService(t, db)
		token, _, _ := s.PrepareSetup(ctx, "")
		if _, err := s.Setup(ctx, token, owner, "", ""); err != nil {
			t.Fatal(err)
		}
		res, err := s.Login(ctx, " ANA@acme.io", owner.Password, "10.0.0.1", "ua")
		if err != nil {
			t.Fatalf("login: %v", err)
		}
		p, err := s.Authenticate(ctx, res.Token)
		if err != nil || p.Membership.Role != org.RoleOwner || p.CSRFToken() == "" {
			t.Fatalf("authenticate: %+v %v", p, err)
		}
		before := h.verifies.Load()
		_, errUnknown := s.Login(ctx, "nobody@acme.io", "a long password!", "10.0.0.2", "ua")
		_, errWrong := s.Login(ctx, "ana@acme.io", "a wrong password!", "10.0.0.2", "ua")
		if !errors.Is(errUnknown, auth.ErrInvalidCredentials) || !errors.Is(errWrong, auth.ErrInvalidCredentials) ||
			errUnknown.Error() != errWrong.Error() {
			t.Fatalf("not uniform: %v / %v", errUnknown, errWrong)
		}
		if got := h.verifies.Load() - before; got != 2 {
			t.Fatalf("%d hash verifications for 2 failed logins, want 2", got)
		}
		failed, _ := audit.List(ctx, db.Gorm, audit.Filter{Action: audit.ActionLoginFailed})
		if len(failed.Events) != 2 || failed.Events[0].ClientIP != "10.0.0.2" || failed.Events[0].Details["email"] == nil {
			t.Fatalf("failed logins audited: %+v", failed.Events)
		}
		// a disabled user gets the same answer with the right password
		if err := db.Gorm.Exec(`UPDATE users SET status = 'disabled'`).Error; err != nil {
			t.Fatal(err)
		}
		if _, err := s.Login(ctx, "ana@acme.io", owner.Password, "10.0.0.3", "ua"); !errors.Is(err, auth.ErrInvalidCredentials) {
			t.Fatalf("disabled: %v", err)
		}
	})
}

func TestChangePasswordAndLogout(t *testing.T) {
	dbtest.Each(t, func(t *testing.T, db *database.DB) {
		s, _ := newService(t, db)
		token, _, _ := s.PrepareSetup(ctx, "")
		first, err := s.Setup(ctx, token, owner, "", "")
		if err != nil {
			t.Fatal(err)
		}
		second, _ := s.Login(ctx, owner.Email, owner.Password, "", "")
		p, _ := s.Authenticate(ctx, first.Token)
		if _, err := s.ChangePassword(ctx, p, "wrong current pw", "a brand new password", "", ""); !errors.Is(err, auth.ErrInvalidCredentials) {
			t.Fatalf("wrong current: %v", err)
		}
		next, err := s.ChangePassword(ctx, p, owner.Password, "a brand new password", "", "")
		if err != nil {
			t.Fatal(err)
		}
		for _, old := range []string{first.Token, second.Token} {
			if _, err := s.Authenticate(ctx, old); err == nil {
				t.Fatal("an old session survived the password change")
			}
		}
		np, err := s.Authenticate(ctx, next.Token)
		if err != nil {
			t.Fatalf("rotated session: %v", err)
		}
		if err := s.Logout(ctx, np, ""); err != nil {
			t.Fatal(err)
		}
		if _, err := s.Authenticate(ctx, next.Token); !errors.Is(err, session.ErrInvalid) {
			t.Fatalf("after logout: %v", err)
		}
	})
}

func TestRateLimits(t *testing.T) {
	t.Parallel()
	l := auth.NewLimiter(nil, nil, 50)
	for range 5 {
		if err := l.Reserve(ctx, "1.1.1.1", "ana@acme.io"); err != nil {
			t.Fatal(err)
		}
	}
	var rl *auth.RateLimitedError
	if err := l.Reserve(ctx, "9.9.9.9", "ana@acme.io"); !errors.As(err, &rl) || rl.RetryAfter <= 0 {
		t.Fatalf("6th attempt for one e-mail: %v", err)
	}
	// spraying unknown e-mails from one address
	for i := range 20 {
		_ = l.Reserve(ctx, "2.2.2.2", string(rune('a'+i))+"@spray.io")
	}
	if err := l.Reserve(ctx, "2.2.2.2", "fresh@spray.io"); !errors.As(err, &rl) {
		t.Fatalf("21st attempt from one address: %v", err)
	}
	// a success releases its reservation and clears the e-mail
	for range 4 {
		_ = l.Reserve(ctx, "3.3.3.3", "bob@acme.io")
	}
	l.Release(ctx, "3.3.3.3", "bob@acme.io")
	for range 5 {
		if err := l.Reserve(ctx, "4.4.4.4", "bob@acme.io"); err != nil {
			t.Fatalf("after a success the e-mail starts over: %v", err)
		}
	}
	for i := range 200 {
		_ = l.Reserve(ctx, "5.5.5.5", string(rune(0x100+i))+"@flood.io")
	}
	if n := auth.MemoryKeys(l); n > 50 {
		t.Fatalf("limiter memory holds %d keys, bound is 50", n)
	}
}

// TestConcurrentLoginsCannotExceedTheLimit fires 20 simultaneous wrong logins
// for one account: at most 5 may reach the password check.
func TestConcurrentLoginsCannotExceedTheLimit(t *testing.T) {
	dbtest.Each(t, func(t *testing.T, db *database.DB) {
		s, h := newService(t, db)
		token, _, _ := s.PrepareSetup(ctx, "")
		if _, err := s.Setup(ctx, token, owner, "", ""); err != nil {
			t.Fatal(err)
		}
		before := h.verifies.Load()
		var wg sync.WaitGroup
		for i := range 20 {
			wg.Go(func() { _, _ = s.Login(ctx, owner.Email, "wrong password!!", fmt.Sprintf("10.1.0.%d", i), "") })
		}
		wg.Wait()
		if n := h.verifies.Load() - before; n > 5 {
			t.Fatalf("%d password checks for one account, limit is 5", n)
		}
	})
}

// hookHasher runs onVerify while a verification is in flight, to interleave a
// concurrent change deterministically.
type hookHasher struct {
	*identity.Hasher
	onVerify func()
}

func (h *hookHasher) Verify(ctx context.Context, enc, pw string) error {
	err := h.Hasher.Verify(ctx, enc, pw)
	if h.onVerify != nil {
		f := h.onVerify
		h.onVerify = nil
		f()
	}
	return err
}

func TestResetDuringLoginWins(t *testing.T) {
	dbtest.Each(t, func(t *testing.T, db *database.DB) {
		s, _ := newService(t, db)
		base := identity.NewHasher(identity.Argon2Params{MemoryKiB: 1024, Time: 1, Threads: 1}, 8, time.Second)
		hh := &hookHasher{Hasher: base}
		s.Hasher = hh
		token, _, _ := s.PrepareSetup(ctx, "")
		res, err := s.Setup(ctx, token, owner, "", "")
		if err != nil {
			t.Fatal(err)
		}
		users := &org.Users{DB: db, Hasher: base}
		// the old password verifies, then an administrator resets it before the session is issued
		hh.onVerify = func() {
			if err := users.ResetPassword(ctx, org.SystemActor, res.User.ID, "a brand new reset password"); err != nil {
				t.Error(err)
			}
		}
		if _, err := s.Login(ctx, owner.Email, owner.Password, "", ""); !errors.Is(err, auth.ErrInvalidCredentials) {
			t.Fatalf("login with the reset password issued a session: %v", err)
		}
	})
}

func TestResetDuringSelfChangeWins(t *testing.T) {
	dbtest.Each(t, func(t *testing.T, db *database.DB) {
		s, _ := newService(t, db)
		base := identity.NewHasher(identity.Argon2Params{MemoryKiB: 1024, Time: 1, Threads: 1}, 8, time.Second)
		hh := &hookHasher{Hasher: base}
		s.Hasher = hh
		token, _, _ := s.PrepareSetup(ctx, "")
		res, _ := s.Setup(ctx, token, owner, "", "")
		p, err := s.Authenticate(ctx, res.Token)
		if err != nil {
			t.Fatal(err)
		}
		users := &org.Users{DB: db, Hasher: base}
		hh.onVerify = func() {
			if err := users.ResetPassword(ctx, org.SystemActor, res.User.ID, "the administrator's password"); err != nil {
				t.Error(err)
			}
		}
		if _, err := s.ChangePassword(ctx, p, owner.Password, "an attacker's new password", "", ""); err == nil {
			t.Fatal("a self change overwrote the administrator's reset")
		}
		if _, err := s.Login(ctx, owner.Email, "the administrator's password", "", ""); err != nil {
			t.Fatalf("the reset password must be the one in force: %v", err)
		}
	})
}

func TestSetupTokenFileReplacedPrivately(t *testing.T) {
	db := dbtest.Open(t, "sqlite")
	s, _ := newService(t, db)
	file := filepath.Join(s.DataDir, auth.SetupTokenFile)
	if err := os.WriteFile(file, []byte("old\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(file, 0o644); err != nil {
		t.Fatal(err)
	}
	if _, _, err := s.PrepareSetup(ctx, ""); err != nil {
		t.Fatal(err)
	}
	st, err := os.Stat(file)
	if err != nil || st.Mode().Perm() != 0o600 {
		t.Fatalf("token file mode %v %v", st.Mode(), err)
	}
}

func TestFailedLoginAuditKeepsTheSubmittedEmail(t *testing.T) {
	db := dbtest.Open(t, "sqlite")
	s, _ := newService(t, db)
	token, _, _ := s.PrepareSetup(ctx, "")
	if _, err := s.Setup(ctx, token, owner, "", ""); err != nil {
		t.Fatal(err)
	}
	long := strings.Repeat("ñ", 100) + "@example.com" // 212 bytes, multibyte
	_, _ = s.Login(ctx, long, "whatever password", "", "")
	page, _ := audit.List(ctx, db.Gorm, audit.Filter{Action: audit.ActionLoginFailed})
	if len(page.Events) != 1 || page.Events[0].Details["email"] != long {
		t.Fatalf("audited %+v", page.Events)
	}
}

func TestRateLimitsSharedInRedis(t *testing.T) {
	addr := testenv.RedisAddr(t)
	rdb := redis.NewClient(&redis.Options{Addr: addr})
	defer func() { _ = rdb.Close() }()
	email := identity.NewID() + "@acme.io"
	a, b := auth.NewLimiter(rdb, nil, 0), auth.NewLimiter(rdb, nil, 0) // two replicas
	for range 5 {
		_ = a.Reserve(ctx, "4.4.4.4", email)
	}
	if err := b.Reserve(ctx, "5.5.5.5", email); err == nil {
		t.Fatal("replica b does not see replica a's failures")
	}
	// Redis down: limits still apply, per replica, in memory
	down := redis.NewClient(&redis.Options{Addr: "127.0.0.1:1", DialTimeout: 100 * time.Millisecond, MaxRetries: -1})
	defer func() { _ = down.Close() }()
	c := auth.NewLimiter(down, nil, 0)
	for range 5 {
		_ = c.Reserve(ctx, "6.6.6.6", email)
	}
	if err := c.Reserve(ctx, "6.6.6.6", email); err == nil {
		t.Fatal("no limit while Redis is down")
	}
}
