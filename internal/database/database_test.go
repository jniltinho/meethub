package database_test

import (
	"context"
	"database/sql"
	"errors"
	"io/fs"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/jniltinho/meethub/internal/config"
	"github.com/jniltinho/meethub/internal/database"
	"github.com/jniltinho/meethub/internal/database/dbtest"
)

var ctx = context.Background()

func TestSQLitePragmasOnEveryConnection(t *testing.T) {
	db := dbtest.Open(t, config.DriverSQLite)
	// hold several pool connections at once so each one is checked, not one reused
	conns := make([]*sql.Conn, 4)
	for i := range conns {
		c, err := db.SQL.Conn(ctx)
		if err != nil {
			t.Fatal(err)
		}
		conns[i] = c
	}
	for i, c := range conns {
		for pragma, want := range map[string]string{"journal_mode": "wal", "busy_timeout": "5000", "foreign_keys": "1"} {
			var got string
			if err := c.QueryRowContext(ctx, "PRAGMA "+pragma).Scan(&got); err != nil {
				t.Fatal(err)
			}
			if got != want {
				t.Errorf("conn %d: %s = %q, want %q", i, pragma, got, want)
			}
		}
		_ = c.Close()
	}
}

func TestTranslateConstraintErrors(t *testing.T) {
	dbtest.Each(t, func(t *testing.T, db *database.DB) {
		now := time.Now().UTC()
		insertUser := func(id, normalized string) error {
			return database.Translate(db.Gorm.WithContext(ctx).Exec(
				`INSERT INTO users (id, email, email_normalized, display_name, password_hash, status, created_at, updated_at)
				 VALUES (?, ?, ?, 'X', 'h', 'active', ?, ?)`, id, normalized, normalized, now, now).Error)
		}
		if err := insertUser("01J00000000000000000000001", "ana@example.com"); err != nil {
			t.Fatal(err)
		}
		if err := insertUser("01J00000000000000000000002", "ana@example.com"); !errors.Is(err, database.ErrDuplicate) {
			t.Fatalf("duplicate e-mail: %v", err)
		}
		// byte-wise comparison: an accented address is a different e-mail on every dialect
		if err := insertUser("01J00000000000000000000003", "josé@example.com"); err != nil {
			t.Fatal(err)
		}
		if err := insertUser("01J00000000000000000000004", "jose@example.com"); err != nil {
			t.Fatalf("jose vs josé must not collide: %v", err)
		}
		err := database.Translate(db.Gorm.WithContext(ctx).Exec(
			`INSERT INTO memberships (org_id, user_id, role, joined_at) VALUES ('nope', '01J00000000000000000000001', 'member', ?)`, now).Error)
		if !errors.Is(err, database.ErrForeignKey) {
			t.Fatalf("foreign key: %v", err)
		}
	})
}

func TestEmojiRoundTrip(t *testing.T) {
	dbtest.Each(t, func(t *testing.T, db *database.DB) {
		now := time.Now().UTC()
		const name = "Ana 🚀 Silva"
		if err := db.Gorm.Exec(`INSERT INTO users (id, email, email_normalized, display_name, password_hash, status, created_at, updated_at)
			VALUES ('01J00000000000000000000009', 'e@x.io', 'e@x.io', ?, 'h', 'active', ?, ?)`, name, now, now).Error; err != nil {
			t.Fatal(err)
		}
		var got string
		if err := db.Gorm.Raw(`SELECT display_name FROM users WHERE id = '01J00000000000000000000009'`).Scan(&got).Error; err != nil {
			t.Fatal(err)
		}
		if got != name {
			t.Fatalf("got %q", got)
		}
	})
}

func TestMigrationSetsMatchAcrossDialects(t *testing.T) {
	t.Parallel()
	var want []string
	for _, d := range dbtest.Dialects() {
		sources, err := database.MigrationSources(d)
		if err != nil {
			t.Fatal(err)
		}
		names, err := fs.Glob(sources, "*.sql")
		if err != nil {
			t.Fatal(err)
		}
		if len(names) == 0 {
			t.Fatalf("%s: no migrations", d)
		}
		if want == nil {
			want = names
			continue
		}
		if !slices.Equal(names, want) {
			t.Errorf("%s migrations %v differ from %v", d, names, want)
		}
	}
}

func TestMigrateUpDownStatus(t *testing.T) {
	dbtest.EachEmpty(t, func(t *testing.T, s database.Settings) {
		db := dbtest.OpenEmpty(t, s)
		m, err := database.NewMigrator(db)
		if err != nil {
			t.Fatal(err)
		}
		if pending, _ := m.HasPending(ctx); !pending {
			t.Fatal("fresh database must have pending migrations")
		}
		n, err := m.Up(ctx)
		if err != nil || n == 0 {
			t.Fatalf("up: %d %v", n, err)
		}
		if n, err := m.Up(ctx); err != nil || n != 0 {
			t.Fatalf("second up must apply nothing: %d %v", n, err)
		}
		st, err := m.Status(ctx)
		if err != nil {
			t.Fatal(err)
		}
		for _, s := range st {
			if !s.Applied {
				t.Errorf("%s not applied", s.Name)
			}
		}
		last, err := m.Down(ctx)
		if err != nil {
			t.Fatal(err)
		}
		if pending, _ := m.HasPending(ctx); !pending {
			t.Fatalf("after down of %s, a migration must be pending", last.Name)
		}
		if _, err := m.Up(ctx); err != nil {
			t.Fatalf("up after down: %v", err)
		}
	})
}

func TestConcurrentMigrators(t *testing.T) {
	dbtest.EachEmpty(t, func(t *testing.T, s database.Settings) {
		// two independent pools, as two server processes would have
		dbs := []*database.DB{dbtest.OpenEmpty(t, s), dbtest.OpenEmpty(t, s)}
		var wg sync.WaitGroup
		errs := make([]error, len(dbs))
		for i, db := range dbs {
			wg.Go(func() {
				m, err := database.NewMigrator(db, database.WithLockTimeout(time.Minute))
				if err == nil {
					_, err = m.Up(ctx)
				}
				errs[i] = err
			})
		}
		wg.Wait()
		for _, err := range errs {
			if err != nil {
				t.Fatalf("concurrent up: %v", err)
			}
		}
		var applied int
		if err := dbs[0].Gorm.Raw(`SELECT COUNT(*) FROM goose_db_version WHERE version_id > 0`).Scan(&applied).Error; err != nil {
			t.Fatal(err)
		}
		st, _ := mustMigrator(t, dbs[0]).Status(ctx)
		if applied != len(st) {
			t.Fatalf("%d version rows for %d migrations: a migration ran twice", applied, len(st))
		}
	})
}

func TestInterruptedMariaDBMigrationReruns(t *testing.T) {
	db := dbtest.OpenEmpty(t, dbtest.NewSettings(t, config.DriverMariaDB))
	m := mustMigrator(t, db)
	if _, err := m.Up(ctx); err != nil {
		t.Fatal(err)
	}
	// simulate a run that created the tables but failed before recording the version
	if err := db.Gorm.Exec(`DELETE FROM goose_db_version WHERE version_id = 1`).Error; err != nil {
		t.Fatal(err)
	}
	if _, err := m.Up(ctx); err != nil {
		t.Fatalf("re-run after interruption: %v", err)
	}
}

func TestUnreachableDatabase(t *testing.T) {
	t.Parallel()
	s := database.Settings{
		Driver: config.DriverPostgres, Host: "127.0.0.1", Port: 1, Name: "meethub", User: "meethub",
		Password: "very-secret-pw", ConnectTimeout: time.Second, MaxOpenConns: 1,
	}
	start := time.Now()
	_, err := database.Open(ctx, s)
	if err == nil {
		t.Fatal("expected an error")
	}
	if d := time.Since(start); d < 700*time.Millisecond || d > 10*time.Second {
		t.Fatalf("gave up after %s, want about the 1s connect timeout (retrying)", d)
	}
	msg := err.Error()
	if !strings.Contains(msg, "127.0.0.1:1/meethub") || strings.Contains(msg, "very-secret-pw") {
		t.Fatalf("error must name the server and hide the password: %q", msg)
	}
}

func TestWrongPasswordNotEchoed(t *testing.T) {
	s := dbtest.NewSettings(t, config.DriverPostgres)
	s.Password = "wrong-pass-123"
	s.ConnectTimeout = time.Second
	_, err := database.Open(ctx, s)
	if err == nil {
		t.Fatal("expected an authentication error")
	}
	if strings.Contains(err.Error(), "wrong-pass-123") {
		t.Fatalf("password leaked: %q", err)
	}
}

func mustMigrator(t *testing.T, db *database.DB) *database.Migrator {
	t.Helper()
	m, err := database.NewMigrator(db)
	if err != nil {
		t.Fatal(err)
	}
	return m
}

func TestOpenHonorsCancellation(t *testing.T) {
	t.Parallel()
	s := database.Settings{Driver: config.DriverPostgres, Host: "127.0.0.1", Port: 1, Name: "x", User: "x",
		ConnectTimeout: time.Minute, MaxOpenConns: 1}
	c, cancel := context.WithTimeout(context.Background(), 500*time.Millisecond)
	defer cancel()
	start := time.Now()
	if _, err := database.Open(c, s); err == nil || time.Since(start) > 5*time.Second {
		t.Fatalf("cancellation not honored: %v after %s", err, time.Since(start))
	}
}
