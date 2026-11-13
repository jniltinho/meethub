// Package dbtest gives tests a fresh, migrated database per test on every
// dialect the environment provides: SQLite always, MariaDB and PostgreSQL when
// the test stack runs (make test-db).
package dbtest

import (
	"context"
	"crypto/rand"
	"database/sql"
	"encoding/hex"
	"fmt"
	"net"
	"path/filepath"
	"strconv"
	"testing"
	"time"

	_ "github.com/go-sql-driver/mysql"
	_ "github.com/jackc/pgx/v5/stdlib"

	"github.com/jniltinho/meethub/internal/config"
	"github.com/jniltinho/meethub/internal/database"
	"github.com/jniltinho/meethub/internal/testenv"
)

// Dialects lists every database dialect tests can use here.
func Dialects() []string {
	return []string{config.DriverSQLite, config.DriverMariaDB, config.DriverPostgres}
}

// Each runs fn as a subtest for every dialect (skipping unavailable servers),
// with a fresh migrated database.
func Each(t *testing.T, fn func(t *testing.T, db *database.DB)) {
	t.Helper()
	for _, d := range Dialects() {
		t.Run(d, func(t *testing.T) {
			fn(t, Open(t, d))
		})
	}
}

// EachEmpty is Each without running the migrations.
func EachEmpty(t *testing.T, fn func(t *testing.T, s database.Settings)) {
	t.Helper()
	for _, d := range Dialects() {
		t.Run(d, func(t *testing.T) {
			fn(t, NewSettings(t, d))
		})
	}
}

// Open returns a fresh, migrated database of the dialect, closed and dropped
// when the test ends.
func Open(t *testing.T, dialect string) *database.DB {
	t.Helper()
	db := OpenEmpty(t, NewSettings(t, dialect))
	m, err := database.NewMigrator(db)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := m.Up(context.Background()); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	return db
}

// OpenEmpty opens the database described by s, closed when the test ends.
func OpenEmpty(t *testing.T, s database.Settings) *database.DB {
	t.Helper()
	db, err := database.Open(context.Background(), s)
	if err != nil {
		t.Fatalf("open %s: %v", s.Describe(), err)
	}
	t.Cleanup(func() { _ = db.Close() })
	return db
}

// NewSettings creates a new empty database of the dialect (skipping the test
// when its server is not available) and returns settings to reach it.
func NewSettings(t *testing.T, dialect string) database.Settings {
	t.Helper()
	s := database.Settings{Driver: dialect, ConnectTimeout: 10 * time.Second, MaxOpenConns: 8}
	switch dialect {
	case config.DriverSQLite:
		s.SQLitePath = filepath.Join(t.TempDir(), "meethub.db")
		return s
	case config.DriverMariaDB:
		srv := testenv.MariaDB(t)
		name := "t_" + randomHex(t)
		admin := fmt.Sprintf("%s:%s@tcp(%s)/", srv.AdminUser, srv.AdminPassword, srv.Addr)
		adminExec(t, "mysql", admin,
			"CREATE DATABASE `"+name+"` CHARACTER SET utf8mb4 COLLATE utf8mb4_unicode_ci",
			"GRANT ALL ON `"+name+"`.* TO '"+srv.User+"'@'%'")
		t.Cleanup(func() { adminExec(t, "mysql", admin, "DROP DATABASE IF EXISTS `"+name+"`") })
		fill(&s, srv, name)
	case config.DriverPostgres:
		srv := testenv.Postgres(t)
		name := "t_" + randomHex(t)
		admin := fmt.Sprintf("postgres://%s:%s@%s/%s?sslmode=disable", srv.AdminUser, srv.AdminPassword, srv.Addr, srv.Name)
		adminExec(t, "pgx", admin, `CREATE DATABASE "`+name+`"`)
		t.Cleanup(func() { adminExec(t, "pgx", admin, `DROP DATABASE IF EXISTS "`+name+`" WITH (FORCE)`) })
		fill(&s, srv, name)
		s.Params = "sslmode=disable"
	default:
		t.Fatalf("unknown dialect %q", dialect)
	}
	return s
}

func fill(s *database.Settings, srv testenv.Server, name string) {
	host, port, _ := net.SplitHostPort(srv.Addr)
	s.Host = host
	s.Port, _ = strconv.Atoi(port)
	s.User, s.Password, s.Name = srv.User, srv.Password, name
}

func adminExec(t *testing.T, driver, dsn string, stmts ...string) {
	t.Helper()
	db, err := sql.Open(driver, dsn)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = db.Close() }()
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	for _, q := range stmts {
		if _, err := db.ExecContext(ctx, q); err != nil {
			t.Fatalf("%s: %v", q, err)
		}
	}
}

func randomHex(t *testing.T) string {
	t.Helper()
	b := make([]byte, 6)
	if _, err := rand.Read(b); err != nil {
		t.Fatal(err)
	}
	return hex.EncodeToString(b)
}
