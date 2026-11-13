package org_test

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/jniltinho/meethub/internal/audit"
	"github.com/jniltinho/meethub/internal/database"
	"github.com/jniltinho/meethub/internal/database/dbtest"
	"github.com/jniltinho/meethub/internal/identity"
	"github.com/jniltinho/meethub/internal/org"
	"github.com/jniltinho/meethub/internal/session"
)

var ctx = context.Background()

func TestPermissionMatrix(t *testing.T) {
	t.Parallel()
	want := map[string][]org.Permission{
		org.RoleOwner:  {org.PermManageSystem, org.PermManageOwners, org.PermManageUsers, org.PermReadAudits, org.PermEditOwnProfile},
		org.RoleAdmin:  {org.PermManageUsers, org.PermReadAudits, org.PermEditOwnProfile},
		org.RoleMember: {org.PermEditOwnProfile},
		org.RoleGuest:  {org.PermEditOwnProfile},
	}
	all := []org.Permission{org.PermManageSystem, org.PermManageOwners, org.PermManageUsers, org.PermReadAudits, org.PermEditOwnProfile}
	for role, granted := range want {
		for _, p := range all {
			has := false
			for _, g := range granted {
				has = has || g == p
			}
			if org.Can(role, p) != has {
				t.Errorf("Can(%s, %s) = %v, want %v", role, p, !has, has)
			}
		}
	}
	tests := []struct {
		actor, target, newRole string
		want                   error
	}{
		{org.RoleOwner, org.RoleMember, org.RoleAdmin, nil},
		{org.RoleOwner, org.RoleAdmin, org.RoleOwner, nil},
		{org.RoleAdmin, org.RoleMember, org.RoleAdmin, nil},
		{org.RoleAdmin, org.RoleMember, org.RoleOwner, org.ErrForbidden}, // admin cannot create owners
		{org.RoleAdmin, org.RoleOwner, "", org.ErrForbidden},             // nor edit owners
		{org.RoleMember, org.RoleGuest, "", org.ErrForbidden},
		{org.RoleGuest, org.RoleGuest, "", org.ErrForbidden},
		{org.RoleOwner, org.RoleMember, "superuser", org.ErrInvalidRole},
	}
	for _, tt := range tests {
		got := org.Actor{Role: tt.actor}.CanManage(tt.target, tt.newRole)
		if !errors.Is(got, tt.want) {
			t.Errorf("%s managing %s → %q: %v, want %v", tt.actor, tt.target, tt.newRole, got, tt.want)
		}
	}
	if org.SystemActor.CanManage(org.RoleOwner, org.RoleOwner) != nil {
		t.Error("the CLI operator may do anything")
	}
}

func TestSlugify(t *testing.T) {
	t.Parallel()
	for in, want := range map[string]string{"Acme": "acme", "Acme Ltd.": "acme-ltd", "  Équipe  TI ": "quipe-ti", "!!!": "org"} {
		if got := org.Slugify(in); got != want {
			t.Errorf("Slugify(%q) = %q, want %q", in, got, want)
		}
	}
}

type fixture struct {
	db    *database.DB
	users *org.Users
	org   *org.Organization
}

func setup(t *testing.T, db *database.DB) *fixture {
	t.Helper()
	o, err := org.Create(ctx, db.Gorm, "Acme")
	if err != nil {
		t.Fatal(err)
	}
	return &fixture{db: db, org: o, users: &org.Users{
		DB: db, Hasher: identity.NewHasher(identity.Argon2Params{MemoryKiB: 1024, Time: 1, Threads: 1}, 8, time.Second),
	}}
}

func (f *fixture) add(t *testing.T, role string) *org.Member {
	t.Helper()
	m, err := f.users.Create(ctx, org.SystemActor, org.NewUser{
		Email: identity.NewID() + "@acme.io", DisplayName: role + " " + identity.NewID()[20:], Role: role, Password: "a long password!",
	})
	if err != nil {
		t.Fatal(err)
	}
	return m
}

func actorOf(m *org.Member) org.Actor { return org.Actor{UserID: m.ID, Role: m.Role} }

func ptr(s string) *string { return &s }

func TestSecondOrganizationRejected(t *testing.T) {
	dbtest.Each(t, func(t *testing.T, db *database.DB) {
		setup(t, db)
		if _, err := org.Create(ctx, db.Gorm, "Other"); !errors.Is(err, org.ErrAlreadyExists) {
			t.Fatalf("second organization: %v", err)
		}
	})
}

func TestLastOwnerGuard(t *testing.T) {
	dbtest.Each(t, func(t *testing.T, db *database.DB) {
		f := setup(t, db)
		owner := f.add(t, org.RoleOwner)
		if _, err := f.users.Update(ctx, actorOf(owner), owner.ID, org.Patch{Role: ptr(org.RoleAdmin)}); !errors.Is(err, org.ErrLastOwner) {
			t.Fatalf("demote last owner: %v", err)
		}
		if _, err := f.users.Update(ctx, actorOf(owner), owner.ID, org.Patch{Status: ptr(identity.StatusDisabled)}); !errors.Is(err, org.ErrLastOwner) {
			t.Fatalf("disable last owner: %v", err)
		}
	})
}

func TestConcurrentOwnersDisableEachOther(t *testing.T) {
	dbtest.Each(t, func(t *testing.T, db *database.DB) {
		f := setup(t, db)
		a, b := f.add(t, org.RoleOwner), f.add(t, org.RoleOwner)
		var wg sync.WaitGroup
		errs := make([]error, 2)
		start := make(chan struct{})
		for i, pair := range [][2]*org.Member{{a, b}, {b, a}} {
			wg.Go(func() {
				<-start
				_, errs[i] = f.users.Update(ctx, actorOf(pair[0]), pair[1].ID, org.Patch{Status: ptr(identity.StatusDisabled)})
			})
		}
		close(start)
		wg.Wait()
		ok, last := 0, 0
		for _, err := range errs {
			switch {
			case err == nil:
				ok++
			case errors.Is(err, org.ErrLastOwner):
				last++
			default:
				t.Fatalf("unexpected error: %v", err)
			}
		}
		if ok != 1 || last != 1 {
			t.Fatalf("results %v: want one success and one last_owner", errs)
		}
	})
}

func TestAdminRulesAndAudit(t *testing.T) {
	dbtest.Each(t, func(t *testing.T, db *database.DB) {
		f := setup(t, db)
		owner, admin, member := f.add(t, org.RoleOwner), f.add(t, org.RoleAdmin), f.add(t, org.RoleMember)

		if _, err := f.users.Update(ctx, actorOf(admin), owner.ID, org.Patch{Status: ptr(identity.StatusDisabled)}); !errors.Is(err, org.ErrForbidden) {
			t.Fatalf("admin disabling owner: %v", err)
		}
		if err := f.users.ResetPassword(ctx, actorOf(admin), owner.ID, "another long password"); !errors.Is(err, org.ErrForbidden) {
			t.Fatalf("admin resetting owner: %v", err)
		}
		if _, err := f.users.Create(ctx, actorOf(admin), org.NewUser{Email: "o@acme.io", DisplayName: "O", Role: org.RoleOwner, Password: "a long password!"}); !errors.Is(err, org.ErrForbidden) {
			t.Fatalf("admin creating owner: %v", err)
		}
		if _, err := f.users.List(ctx, actorOf(member), org.ListFilter{}); !errors.Is(err, org.ErrForbidden) {
			t.Fatalf("member listing: %v", err)
		}

		// role change by admin: audited with old and new roles
		if _, err := f.users.Update(ctx, actorOf(admin), member.ID, org.Patch{Role: ptr(org.RoleAdmin)}); err != nil {
			t.Fatal(err)
		}
		page, err := audit.List(ctx, db.Gorm, audit.Filter{Action: audit.ActionRoleChanged})
		if err != nil || len(page.Events) != 1 {
			t.Fatalf("audit: %v %+v", err, page)
		}
		e := page.Events[0]
		if *e.ActorID != admin.ID || e.TargetID != member.ID || e.Details["from"] != "member" || e.Details["to"] != "admin" {
			t.Fatalf("role change event: %+v", e)
		}
	})
}

func TestDisableEndsSessions(t *testing.T) {
	dbtest.Each(t, func(t *testing.T, db *database.DB) {
		f := setup(t, db)
		owner, member := f.add(t, org.RoleOwner), f.add(t, org.RoleMember)
		store := session.Store{Idle: time.Hour, Absolute: 24 * time.Hour}
		token, _, err := store.Create(ctx, db.Gorm, member.ID, "", "", time.Now())
		if err != nil {
			t.Fatal(err)
		}
		if _, err := f.users.Update(ctx, actorOf(owner), member.ID, org.Patch{Status: ptr(identity.StatusDisabled)}); err != nil {
			t.Fatal(err)
		}
		if _, err := store.Lookup(ctx, db.Gorm, token, time.Now()); !errors.Is(err, session.ErrInvalid) {
			t.Fatalf("session of a disabled user: %v", err)
		}
	})
}

func TestListPaging(t *testing.T) {
	dbtest.Each(t, func(t *testing.T, db *database.DB) {
		f := setup(t, db)
		owner := f.add(t, org.RoleOwner)
		for i := range 4 {
			if _, err := f.users.Create(ctx, org.SystemActor, org.NewUser{
				Email: fmt.Sprintf("same%d@acme.io", i), DisplayName: "Same Name", Role: org.RoleMember, Password: "a long password!",
			}); err != nil {
				t.Fatal(err)
			}
		}
		seen, cursor := map[string]bool{}, ""
		for range 10 {
			page, err := f.users.List(ctx, actorOf(owner), org.ListFilter{Limit: 2, Cursor: cursor})
			if err != nil {
				t.Fatal(err)
			}
			for _, u := range page.Users {
				if seen[u.ID] {
					t.Fatalf("user %s twice", u.ID)
				}
				seen[u.ID] = true
			}
			if cursor = page.NextCursor; cursor == "" {
				break
			}
		}
		if len(seen) != 5 {
			t.Fatalf("listed %d users, want 5", len(seen))
		}
		members, _ := f.users.List(ctx, actorOf(owner), org.ListFilter{Role: org.RoleMember, Query: "same"})
		if len(members.Users) != 4 {
			t.Fatalf("filter: %d", len(members.Users))
		}
	})
}

func TestResetPasswordEndsSessions(t *testing.T) {
	dbtest.Each(t, func(t *testing.T, db *database.DB) {
		f := setup(t, db)
		member := f.add(t, org.RoleMember)
		store := session.Store{Idle: time.Hour, Absolute: 24 * time.Hour}
		t1, _, _ := store.Create(ctx, db.Gorm, member.ID, "", "", time.Now())
		t2, _, _ := store.Create(ctx, db.Gorm, member.ID, "", "", time.Now())
		if err := f.users.ResetPassword(ctx, org.SystemActor, member.ID, "a reset long password"); err != nil {
			t.Fatal(err)
		}
		for _, tok := range []string{t1, t2} {
			if _, err := store.Lookup(ctx, db.Gorm, tok, time.Now()); !errors.Is(err, session.ErrInvalid) {
				t.Fatalf("session after reset: %v", err)
			}
		}
	})
}
