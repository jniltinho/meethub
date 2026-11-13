package identity_test

import (
	"errors"
	"fmt"
	"sync"
	"testing"

	"github.com/jniltinho/meethub/internal/database"
	"github.com/jniltinho/meethub/internal/database/dbtest"
	"github.com/jniltinho/meethub/internal/identity"
)

func TestNormalizeUsername(t *testing.T) {
	t.Parallel()
	ok := map[string]string{"Ana.Silva": "ana.silva", " @bruno_m ": "bruno_m", "c-3po": "c-3po", "abc": "abc"}
	for in, want := range ok {
		if got, err := identity.NormalizeUsername(in); err != nil || got != want {
			t.Errorf("NormalizeUsername(%q) = %q, %v; want %q", in, got, err, want)
		}
	}
	for _, bad := range []string{"ab", "1abc", "_abc", "ana silva", "ana@x", "here", "ALL", "channel", "system",
		"abcdefghijklmnopqrstuvw", "joão"} {
		if _, err := identity.NormalizeUsername(bad); !errors.Is(err, identity.ErrInvalidUsername) {
			t.Errorf("NormalizeUsername(%q) accepted", bad)
		}
	}
}

func TestUsernamesOnCreate(t *testing.T) {
	dbtest.Each(t, func(t *testing.T, db *database.DB) {
		var s identity.Store
		create := func(email string, username *string) *identity.User {
			t.Helper()
			u := &identity.User{Email: email, DisplayName: "X", PasswordHash: "h", Username: username}
			if err := s.Create(ctx, db.Gorm, u); err != nil {
				t.Fatalf("%s: %v", email, err)
			}
			return u
		}
		a := create("ana.silva@example.com", nil)
		b := create("Ana.Silva@other.org", nil)
		c := create("9lives+tag@x.io", nil)
		long := create("averyveryverylonglocalpartname@x.io", nil)
		if *a.Username != "ana.silva" || *b.Username != "ana.silva2" || *c.Username != "user9lives.tag" {
			t.Fatalf("derived: %q %q %q", *a.Username, *b.Username, *c.Username)
		}
		if n := len(*long.Username); n > identity.MaxUsernameLen {
			t.Fatalf("too long: %q", *long.Username)
		}
		given := "bruno"
		create("b@x.io", &given)
		dup := "BRUNO"
		u := &identity.User{Email: "b2@x.io", DisplayName: "X", PasswordHash: "h", Username: &dup}
		if err := s.Create(ctx, db.Gorm, u); !errors.Is(err, identity.ErrUsernameTaken) {
			t.Fatalf("duplicate explicit username: %v", err)
		}
		if _, err := s.SetUsername(ctx, db.Gorm, a.ID, "here"); !errors.Is(err, identity.ErrInvalidUsername) {
			t.Fatalf("reserved: %v", err)
		}
		if p := a.Public(); p.Username != "ana.silva" || p.DisplayName != "X" {
			t.Fatalf("public: %+v", p)
		}
	})
}

func TestConcurrentUsernameDerivation(t *testing.T) {
	dbtest.Each(t, func(t *testing.T, db *database.DB) {
		var s identity.Store
		var wg sync.WaitGroup
		users := make([]*identity.User, 6)
		errs := make([]error, 6)
		for i := range users {
			users[i] = &identity.User{Email: fmt.Sprintf("carla@d%d.io", i), DisplayName: "C", PasswordHash: "h"}
			wg.Go(func() { errs[i] = s.Create(ctx, db.Gorm, users[i]) })
		}
		wg.Wait()
		seen := map[string]bool{}
		for i, u := range users {
			if errs[i] != nil {
				t.Fatalf("create %d: %v", i, errs[i])
			}
			if seen[*u.Username] {
				t.Fatalf("username %q given twice", *u.Username)
			}
			seen[*u.Username] = true
		}
	})
}
