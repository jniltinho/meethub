package chat_test

import (
	"context"
	"sync"
	"testing"
	"time"

	"gorm.io/gorm"

	"github.com/jniltinho/meethub/internal/auth"
	"github.com/jniltinho/meethub/internal/chat"
	"github.com/jniltinho/meethub/internal/database"
	"github.com/jniltinho/meethub/internal/database/dbtest"
	"github.com/jniltinho/meethub/internal/identity"
	"github.com/jniltinho/meethub/internal/org"
	"github.com/jniltinho/meethub/internal/ratelimit"
	"github.com/jniltinho/meethub/internal/session"
)

type ctxT = context.Context

var fastHasher = identity.NewHasher(identity.Argon2Params{MemoryKiB: 1024, Time: 1, Threads: 1}, 8, time.Second)

func defaultTeamMembers(t *testing.T, db *database.DB) map[string]string {
	t.Helper()
	var rows []struct{ UserID, Role string }
	db.Gorm.Table("team_members tm").Select("tm.user_id, tm.role").Joins("JOIN teams t ON t.id = tm.team_id").
		Where("t.is_default = ?", true).Scan(&rows)
	out := map[string]string{}
	for _, r := range rows {
		out[trim(r.UserID)] = r.Role
	}
	return out
}

func trim(s string) string {
	for len(s) > 0 && s[len(s)-1] == ' ' {
		s = s[:len(s)-1]
	}
	return s
}

func TestFreshInstallMemberships(t *testing.T) {
	dbtest.Each(t, func(t *testing.T, db *database.DB) {
		a := &auth.Service{DB: db, Hasher: fastHasher, Limiter: ratelimit.NewLogins(nil, nil, 0), DataDir: t.TempDir(),
			Sessions: session.Store{Idle: time.Hour, Absolute: time.Hour}}
		a.OnInitialize = func(ctx2 ctxT, tx *gorm.DB, orgName, ownerID string) error {
			return chat.EnsureMemberships(ctx2, tx, db.Dialect, orgName, ownerID, org.RoleOwner)
		}
		_, owner, err := a.Initialize(ctx, auth.InitInput{OrganizationName: "Example Company Ltd", DisplayName: "Ana", Email: "ana@x.io", Password: "a long password!"}, "")
		if err != nil {
			t.Fatal(err)
		}
		users := &org.Users{DB: db, Hasher: fastHasher, OnCreate: func(ctx2 ctxT, tx *gorm.DB, userID, role string) error {
			o, err := org.Get(ctx2, tx)
			if err != nil {
				return err
			}
			return chat.EnsureMemberships(ctx2, tx, db.Dialect, o.Name, userID, role)
		}}
		member, _ := users.Create(ctx, org.SystemActor, org.NewUser{Email: "m@x.io", DisplayName: "M", Role: org.RoleMember, Password: "a long password!"})
		guest, _ := users.Create(ctx, org.SystemActor, org.NewUser{Email: "g@x.io", DisplayName: "G", Role: org.RoleGuest, Password: "a long password!"})
		got := defaultTeamMembers(t, db)
		if got[owner.ID] != chat.TeamAdmin || got[member.ID] != chat.TeamUser || got[guest.ID] != "" {
			t.Fatalf("default team members: %v", got)
		}
		var team chat.Team
		db.Gorm.Where("is_default = ?", true).Take(&team)
		if team.Name != "example-company-ltd" || team.DisplayName != "Example Company Ltd" {
			t.Fatalf("default team: %+v", team)
		}
	})
}

// foundationEra builds a database as the foundation left it: migration 00001
// only, an organization and users without usernames.
func foundationEra(t *testing.T, dialect string) (*database.DB, database.Settings) {
	t.Helper()
	settings := dbtest.NewSettings(t, dialect)
	db := dbtest.OpenEmpty(t, settings)
	m, err := database.NewMigrator(db)
	if err != nil {
		t.Fatal(err)
	}
	if err := m.UpTo(ctx, 1); err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC()
	exec := func(q string, args ...any) {
		t.Helper()
		if err := db.Gorm.Exec(q, args...).Error; err != nil {
			t.Fatalf("%s: %v", q, err)
		}
	}
	exec(`INSERT INTO organizations (id, singleton, slug, name, default_theme, created_at, updated_at) VALUES ('01JORG0000000000000000ACME', 1, 'acme', 'Acme', 'meethub-dark', ?, ?)`, now, now)
	for _, u := range []struct{ id, email, role string }{
		{"01JUSER0000000000000OWNER1", "ana.silva@acme.io", "owner"},
		{"01JUSER000000000000MEMBER1", "Ana.Silva@other.io", "member"},
		{"01JUSER0000000000000GUEST1", "guest@acme.io", "guest"},
	} {
		exec(`INSERT INTO users (id, email, email_normalized, display_name, password_hash, status, created_at, updated_at) VALUES (?, ?, LOWER(?), 'X', 'h', 'active', ?, ?)`, u.id, u.email, u.email, now, now)
		exec(`INSERT INTO memberships (org_id, user_id, role, joined_at) VALUES ('01JORG0000000000000000ACME', ?, ?, ?)`, u.id, u.role, now)
	}
	if _, err := m.Up(ctx); err != nil {
		t.Fatalf("upgrade: %v", err)
	}
	return db, settings
}

func TestBackfillUpgradesAFoundationDatabase(t *testing.T) {
	for _, dialect := range dbtest.Dialects() {
		t.Run(dialect, func(t *testing.T) {
			db, _ := foundationEra(t, dialect)
			rep, err := chat.Backfill(ctx, db)
			if err != nil {
				t.Fatal(err)
			}
			if rep.Usernames != 3 || !rep.DefaultTeam || rep.Memberships != 2 {
				t.Fatalf("report: %+v", rep)
			}
			var names []string
			db.Gorm.Table("users").Order("username").Pluck("username", &names)
			if len(names) != 3 || names[0] != "ana.silva" || names[1] != "ana.silva2" || names[2] != "guest" {
				t.Fatalf("usernames: %v", names)
			}
			got := defaultTeamMembers(t, db)
			if got["01JUSER0000000000000OWNER1"] != chat.TeamAdmin || got["01JUSER000000000000MEMBER1"] != chat.TeamUser || len(got) != 2 {
				t.Fatalf("memberships: %v", got)
			}
			// idempotent
			if again, err := chat.Backfill(ctx, db); err != nil || again != (chat.BackfillReport{}) {
				t.Fatalf("second backfill: %+v %v", again, err)
			}
			// interrupted, then resumed: a lost membership and a missing username come back
			db.Gorm.Exec(`DELETE FROM team_members WHERE user_id = '01JUSER000000000000MEMBER1'`)
			db.Gorm.Exec(`UPDATE users SET username = NULL, username_normalized = NULL WHERE id = '01JUSER0000000000000GUEST1'`)
			resumed, err := chat.Backfill(ctx, db)
			if err != nil || resumed.Usernames != 1 || resumed.Memberships != 1 || resumed.DefaultTeam {
				t.Fatalf("resumed: %+v %v", resumed, err)
			}
		})
	}
}

func TestConcurrentBackfills(t *testing.T) {
	for _, dialect := range dbtest.Dialects() {
		t.Run(dialect, func(t *testing.T) {
			db, settings := foundationEra(t, dialect)
			second := dbtest.OpenEmpty(t, settings) // a second process's pool
			var wg sync.WaitGroup
			errs := make([]error, 2)
			for i, d := range []*database.DB{db, second} {
				wg.Go(func() { _, errs[i] = chat.Backfill(ctx, d) })
			}
			wg.Wait()
			for _, err := range errs {
				if err != nil {
					t.Fatal(err)
				}
			}
			var teams int64
			db.Gorm.Model(&chat.Team{}).Where("is_default = ?", true).Count(&teams)
			if teams != 1 || len(defaultTeamMembers(t, db)) != 2 {
				t.Fatalf("%d default teams, members %v", teams, defaultTeamMembers(t, db))
			}
		})
	}
}
