package auth_test

import (
	"bytes"
	"errors"
	"fmt"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/redis/go-redis/v9"

	"github.com/jniltinho/meethub/internal/audit"
	"github.com/jniltinho/meethub/internal/auth"
	"github.com/jniltinho/meethub/internal/database"
	"github.com/jniltinho/meethub/internal/database/dbtest"
	"github.com/jniltinho/meethub/internal/org"
	"github.com/jniltinho/meethub/internal/testenv"
)

type testClock struct {
	mu sync.Mutex
	t  time.Time
}

func (c *testClock) Now() time.Time      { c.mu.Lock(); defer c.mu.Unlock(); return c.t }
func (c *testClock) Add(d time.Duration) { c.mu.Lock(); defer c.mu.Unlock(); c.t = c.t.Add(d) }

var throwawayKey = bytes.Repeat([]byte{0x42}, 32) // test-only key

// enrolled sets up the owner with two-factor on; it returns the service, the owner's principal, the authenticator
// secret, the recovery codes and the clock.
func enrolled(t *testing.T, db *database.DB) (*auth.Service, *auth.Principal, string, []string, *testClock) {
	t.Helper()
	rdb := redis.NewClient(&redis.Options{Addr: testenv.RedisAddr(t)})
	t.Cleanup(func() { _ = rdb.Close() })
	s, _ := newService(t, db)
	clock := &testClock{t: time.Unix(1_900_000_000, 0)}
	s.Redis, s.SecretKey, s.Now = rdb, throwawayKey, clock.Now
	token, _, _ := s.PrepareSetup(ctx, "")
	res, err := s.Setup(ctx, token, owner, "", "")
	if err != nil {
		t.Fatal(err)
	}
	p, err := s.Authenticate(ctx, res.Token)
	if err != nil {
		t.Fatal(err)
	}
	e, err := s.BeginTwoFactor(ctx, p)
	if err != nil || !strings.HasPrefix(e.QRCode, "data:image/png;base64,") || !strings.Contains(e.URI, e.Secret) {
		t.Fatalf("begin: %+v %v", e, err)
	}
	if _, err := s.ConfirmTwoFactor(ctx, p, "000000", ""); !errors.Is(err, auth.ErrInvalidCode) {
		t.Fatalf("wrong confirmation: %v", err)
	}
	if on, _ := s.TwoFactorEnabled(ctx, p.User.ID); on {
		t.Fatal("a wrong confirmation turned two-factor on")
	}
	codes, err := s.ConfirmTwoFactor(ctx, p, auth.TOTPCodeAt(e.Secret, clock.Now()), "")
	if err != nil || len(codes) != 10 {
		t.Fatalf("confirm: %v %v", codes, err)
	}
	if _, err := s.BeginTwoFactor(ctx, p); !errors.Is(err, auth.ErrTwoFactorEnabled) {
		t.Fatalf("begin while on: %v", err)
	}
	clock.Add(30 * time.Second) // the confirmation used the current step
	return s, p, e.Secret, codes, clock
}

func TestTwoFactorLogin(t *testing.T) {
	dbtest.Each(t, func(t *testing.T, db *database.DB) {
		s, p, secret, codes, clock := enrolled(t, db)
		var stored string
		db.Gorm.Raw(`SELECT secret FROM user_second_factors`).Scan(&stored)
		if stored == "" || strings.Contains(stored, secret) {
			t.Fatal("the authenticator secret is stored in clear")
		}

		// the password alone creates no session
		first, err := s.Login(ctx, owner.Email, owner.Password, "10.0.0.1", "ua")
		if err != nil || first.TwoFactorTicket == "" || first.Token != "" || first.Session != nil {
			t.Fatalf("first step: %+v %v", first, err)
		}
		code := auth.TOTPCodeAt(secret, clock.Now())
		if _, err := s.LoginTwoFactor(ctx, first.TwoFactorTicket, code, "10.9.9.9", "ua"); !errors.Is(err, auth.ErrInvalidTicket) {
			t.Fatalf("ticket from another address: %v", err)
		}
		res, err := s.LoginTwoFactor(ctx, first.TwoFactorTicket, code, "10.0.0.1", "ua")
		if err != nil || res.Token == "" {
			t.Fatalf("second step: %v", err)
		}
		if _, err := s.LoginTwoFactor(ctx, first.TwoFactorTicket, code, "10.0.0.1", "ua"); !errors.Is(err, auth.ErrInvalidTicket) {
			t.Fatalf("ticket reused: %v", err)
		}

		// the same code for another login within its 30 seconds
		again, _ := s.Login(ctx, owner.Email, owner.Password, "10.0.0.1", "ua")
		if _, err := s.LoginTwoFactor(ctx, again.TwoFactorTicket, code, "10.0.0.1", "ua"); !errors.Is(err, auth.ErrInvalidCode) {
			t.Fatalf("code replayed: %v", err)
		}
		// a recovery code instead, typed loosely; then it no longer works
		if _, err := s.LoginTwoFactor(ctx, again.TwoFactorTicket, " "+strings.ToUpper(codes[3])+" ", "10.0.0.1", "ua"); err != nil {
			t.Fatalf("recovery code: %v", err)
		}
		third, _ := s.Login(ctx, owner.Email, owner.Password, "10.0.0.1", "ua")
		if _, err := s.LoginTwoFactor(ctx, third.TwoFactorTicket, codes[3], "10.0.0.1", "ua"); !errors.Is(err, auth.ErrInvalidCode) {
			t.Fatalf("recovery code reused: %v", err)
		}
		if st, _ := s.TwoFactor(ctx, p); !st.Enabled || st.RecoveryCodes != 9 {
			t.Fatalf("status %+v", st)
		}

		// five wrong codes void the ticket, even for the right code
		guess, _ := s.Login(ctx, owner.Email, owner.Password, "10.0.0.1", "ua")
		for i := range 5 {
			err := func() error {
				_, err := s.LoginTwoFactor(ctx, guess.TwoFactorTicket, "123456", "10.0.0.1", "ua")
				return err
			}()
			want := auth.ErrInvalidCode
			if i == 4 {
				want = auth.ErrInvalidTicket
			}
			if !errors.Is(err, want) {
				t.Fatalf("guess %d: %v", i+1, err)
			}
		}
		clock.Add(30 * time.Second)
		if _, err := s.LoginTwoFactor(ctx, guess.TwoFactorTicket, auth.TOTPCodeAt(secret, clock.Now()), "10.0.0.1", "ua"); !errors.Is(err, auth.ErrInvalidTicket) {
			t.Fatalf("right code on a voided ticket: %v", err)
		}
		for action, n := range map[string]int{audit.ActionTwoFactorVoided: 1, audit.ActionTwoFactorFailed: 7, audit.ActionRecoveryCodeUsed: 1, audit.ActionTwoFactorEnabled: 1} {
			page, _ := audit.List(ctx, db.Gorm, audit.Filter{Action: action})
			if len(page.Events) != n {
				t.Errorf("%s: %d events, want %d", action, len(page.Events), n)
			}
			for _, e := range page.Events {
				for k, v := range e.Details {
					if strings.Contains(fmt.Sprint(v), secret) || strings.Contains(fmt.Sprint(v), codes[3]) {
						t.Errorf("%s detail %s carries a secret", action, k)
					}
				}
			}
		}
	})
}

func TestTwoFactorKeyAndTurnOff(t *testing.T) {
	dbtest.Each(t, func(t *testing.T, db *database.DB) {
		s, p, secret, _, clock := enrolled(t, db)

		// a wrong key: enrolled people fail closed at the code
		s.SecretKey = bytes.Repeat([]byte{0x43}, 32)
		tk, _ := s.Login(ctx, owner.Email, owner.Password, "", "")
		if _, err := s.LoginTwoFactor(ctx, tk.TwoFactorTicket, auth.TOTPCodeAt(secret, clock.Now()), "", ""); !errors.Is(err, auth.ErrTwoFactorUnavailable) {
			t.Fatalf("wrong key: %v", err)
		}
		// no key: unavailable, and the password alone is not enough
		s.SecretKey = nil
		if _, err := s.TwoFactor(ctx, p); !errors.Is(err, auth.ErrTwoFactorUnavailable) {
			t.Fatalf("no key: %v", err)
		}
		if _, err := s.Login(ctx, owner.Email, owner.Password, "", ""); !errors.Is(err, auth.ErrTwoFactorUnavailable) {
			t.Fatalf("login without a key: %v", err)
		}
		s.SecretKey = throwawayKey

		// turning off needs the password and a current code
		code := auth.TOTPCodeAt(secret, clock.Now())
		if err := s.DisableTwoFactor(ctx, p, "not the password", code, ""); !errors.Is(err, auth.ErrInvalidCredentials) {
			t.Fatalf("wrong password: %v", err)
		}
		if err := s.DisableTwoFactor(ctx, p, owner.Password, "000000", ""); !errors.Is(err, auth.ErrInvalidCode) {
			t.Fatalf("wrong code: %v", err)
		}
		if err := s.DisableTwoFactor(ctx, p, owner.Password, code, ""); err != nil {
			t.Fatalf("disable: %v", err)
		}
		if res, err := s.Login(ctx, owner.Email, owner.Password, "", ""); err != nil || res.Token == "" {
			t.Fatalf("login after turning off: %+v %v", res, err)
		}
		var rows int64
		db.Gorm.Raw(`SELECT COUNT(*) FROM user_recovery_codes`).Scan(&rows)
		if rows != 0 {
			t.Fatal("recovery codes left behind")
		}
	})
}

func TestTwoFactorAdminReset(t *testing.T) {
	dbtest.Each(t, func(t *testing.T, db *database.DB) {
		s, p, _, _, _ := enrolled(t, db)
		users := &org.Users{DB: db, Hasher: s.Hasher.(*countingHasher).Hasher}
		admin, err := users.Create(ctx, org.SystemActor, org.NewUser{Email: "bruno@acme.io", DisplayName: "Bruno", Role: org.RoleAdmin, Password: "a long password!"})
		if err != nil {
			t.Fatal(err)
		}
		adminActor := org.Actor{UserID: admin.ID, Role: org.RoleAdmin, ClientIP: "10.0.0.5"}
		// an admin cannot reset an owner
		if err := users.ResetTwoFactor(ctx, adminActor, p.User.ID); !errors.Is(err, org.ErrForbidden) {
			t.Fatalf("admin on owner: %v", err)
		}
		ownerActor := p.Actor("10.0.0.6")
		if err := users.ResetTwoFactor(ctx, ownerActor, p.User.ID); err != nil {
			t.Fatal(err)
		}
		if _, err := s.Authenticate(ctx, p.Token); err == nil {
			t.Fatal("a session survived the reset")
		}
		if res, err := s.Login(ctx, owner.Email, owner.Password, "", ""); err != nil || res.Token == "" {
			t.Fatalf("login after the reset needs only the password: %+v %v", res, err)
		}
		page, _ := audit.List(ctx, db.Gorm, audit.Filter{Action: audit.ActionTwoFactorReset})
		if len(page.Events) != 1 || *page.Events[0].ActorID != p.User.ID || page.Events[0].TargetID != p.User.ID {
			t.Fatalf("reset audit: %+v", page.Events)
		}
	})
}

func TestRecoveryCodesRegenerated(t *testing.T) {
	db := dbtest.Open(t, "sqlite")
	s, p, secret, codes, clock := enrolled(t, db)
	if _, err := s.RegenerateRecoveryCodes(ctx, p, "000000", ""); !errors.Is(err, auth.ErrInvalidCode) {
		t.Fatalf("wrong code: %v", err)
	}
	fresh, err := s.RegenerateRecoveryCodes(ctx, p, auth.TOTPCodeAt(secret, clock.Now()), "")
	if err != nil || len(fresh) != 10 {
		t.Fatal(err)
	}
	tk, _ := s.Login(ctx, owner.Email, owner.Password, "", "")
	if _, err := s.LoginTwoFactor(ctx, tk.TwoFactorTicket, codes[0], "", ""); !errors.Is(err, auth.ErrInvalidCode) {
		t.Fatalf("an old recovery code still works: %v", err)
	}
	if _, err := s.LoginTwoFactor(ctx, tk.TwoFactorTicket, fresh[0], "", ""); err != nil {
		t.Fatalf("a new recovery code: %v", err)
	}
}
