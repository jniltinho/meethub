// Package database opens the one relational database MeetHub uses — SQLite
// (default, pure Go through modernc.org/sqlite), MariaDB or PostgreSQL — and
// translates driver errors into portable domain errors.
package database

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"net"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	mysqldriver "github.com/go-sql-driver/mysql"
	_ "github.com/jackc/pgx/v5/stdlib" // registers the "pgx" database/sql driver
	"gorm.io/driver/mysql"
	"gorm.io/driver/postgres"
	gormsqlite "gorm.io/driver/sqlite"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
	_ "modernc.org/sqlite" // registers the pure-Go "sqlite" database/sql driver

	"github.com/jniltinho/meethub/internal/config"
)

// SQLiteDriverName is the database/sql name registered by modernc.org/sqlite.
const SQLiteDriverName = "sqlite"

// sqlitePragmas are applied on every SQLite connection (modernc _pragma syntax);
// _txlock=immediate makes write transactions take the lock up front.
const sqlitePragmas = "_pragma=busy_timeout(5000)&_pragma=journal_mode(WAL)&_pragma=foreign_keys(1)&_pragma=synchronous(NORMAL)&_txlock=immediate"

// DB is an open database: the GORM handle, its *sql.DB and the dialect.
type DB struct {
	Gorm    *gorm.DB
	SQL     *sql.DB
	Dialect string // config.DriverSQLite, config.DriverMariaDB or config.DriverPostgres
	path    string // SQLite file, for the migration lock
}

// Settings are the inputs Open needs, taken from the configuration.
type Settings struct {
	Driver         string
	SQLitePath     string
	Host           string
	Port           int
	Name           string
	User           string
	Password       string
	Params         string
	ConnectTimeout time.Duration
	MaxOpenConns   int
}

// SettingsFrom extracts the database settings from the configuration.
func SettingsFrom(c *config.Config) Settings {
	return Settings{
		Driver:         c.Database.Driver,
		SQLitePath:     c.SQLitePath(),
		Host:           c.Database.Host,
		Port:           c.DatabasePort(),
		Name:           c.Database.Name,
		User:           c.Database.User,
		Password:       c.Database.Password,
		Params:         c.Database.Params,
		ConnectTimeout: c.Database.ConnectTimeout.Std(),
		MaxOpenConns:   c.Database.MaxOpenConns,
	}
}

// Describe names the database without credentials, for messages.
func (s Settings) Describe() string {
	if s.Driver == config.DriverSQLite {
		return "sqlite " + s.SQLitePath
	}
	return fmt.Sprintf("%s %s/%s", s.Driver, net.JoinHostPort(s.Host, strconv.Itoa(s.Port)), s.Name)
}

// Open connects to the configured database, retrying until the connect
// timeout, and verifies the connection with a ping.
func Open(ctx context.Context, s Settings) (*DB, error) {
	dialector, err := s.dialector()
	if err != nil {
		return nil, s.redact(err)
	}
	gdb, err := gorm.Open(dialector, &gorm.Config{
		Logger:                 logger.Discard,
		NowFunc:                func() time.Time { return time.Now().UTC() },
		SkipDefaultTransaction: true,
		TranslateError:         false,
	})
	if err != nil {
		return nil, s.redact(fmt.Errorf("open %s: %w", s.Describe(), err))
	}
	sqlDB, err := gdb.DB()
	if err != nil {
		return nil, s.redact(fmt.Errorf("open %s: %w", s.Describe(), err))
	}
	sqlDB.SetMaxOpenConns(s.MaxOpenConns)
	sqlDB.SetMaxIdleConns(min(s.MaxOpenConns, 10))
	sqlDB.SetConnMaxIdleTime(5 * time.Minute)

	db := &DB{Gorm: gdb, SQL: sqlDB, Dialect: s.Driver, path: s.SQLitePath}
	if err := db.waitReady(ctx, s); err != nil {
		_ = sqlDB.Close()
		return nil, err
	}
	return db, nil
}

func (s Settings) dialector() (gorm.Dialector, error) {
	switch s.Driver {
	case config.DriverSQLite:
		if err := os.MkdirAll(filepath.Dir(s.SQLitePath), 0o750); err != nil {
			return nil, fmt.Errorf("create database directory: %w", err)
		}
		dsn := "file:" + s.SQLitePath + "?" + sqlitePragmas
		return gormsqlite.Dialector{DriverName: SQLiteDriverName, DSN: dsn}, nil
	case config.DriverMariaDB:
		mc := mysqldriver.NewConfig()
		mc.User, mc.Passwd = s.User, s.Password
		mc.Net, mc.Addr, mc.DBName = "tcp", net.JoinHostPort(s.Host, strconv.Itoa(s.Port)), s.Name
		mc.ParseTime, mc.Loc = true, time.UTC
		mc.Collation = "utf8mb4_unicode_ci"
		mc.Params = map[string]string{"charset": "utf8mb4", "time_zone": "'+00:00'"}
		extra, err := url.ParseQuery(s.Params)
		if err != nil {
			return nil, fmt.Errorf("database.params: %w", err)
		}
		for k := range extra {
			mc.Params[k] = extra.Get(k)
		}
		return mysql.New(mysql.Config{DSNConfig: mc}), nil
	case config.DriverPostgres:
		u := url.URL{
			Scheme: "postgres",
			User:   url.UserPassword(s.User, s.Password),
			Host:   net.JoinHostPort(s.Host, strconv.Itoa(s.Port)),
			Path:   "/" + s.Name,
		}
		q, err := url.ParseQuery(s.Params)
		if err != nil {
			return nil, fmt.Errorf("database.params: %w", err)
		}
		if q.Get("sslmode") == "" {
			q.Set("sslmode", "prefer")
		}
		q.Set("timezone", "UTC")
		u.RawQuery = q.Encode()
		return postgres.New(postgres.Config{DriverName: "pgx", DSN: u.String()}), nil
	default:
		return nil, fmt.Errorf("unsupported database driver %q", s.Driver)
	}
}

// waitReady pings until success or the connect timeout.
func (db *DB) waitReady(ctx context.Context, s Settings) error {
	deadline := time.Now().Add(s.ConnectTimeout)
	delay := 250 * time.Millisecond
	for {
		pingCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
		err := db.SQL.PingContext(pingCtx)
		cancel()
		if err == nil {
			return nil
		}
		if ctx.Err() != nil || time.Now().Add(delay).After(deadline) {
			return s.redact(fmt.Errorf("connect to %s (gave up after %s): %w", s.Describe(), s.ConnectTimeout, err))
		}
		select {
		case <-ctx.Done():
			return s.redact(fmt.Errorf("connect to %s: %w", s.Describe(), ctx.Err()))
		case <-time.After(delay):
		}
		delay = min(delay*2, 5*time.Second)
	}
}

// Ping checks the database is reachable (readiness).
func (db *DB) Ping(ctx context.Context) error { return db.SQL.PingContext(ctx) }

// Close closes the connection pool.
func (db *DB) Close() error { return db.SQL.Close() }

// redact removes the password from err's message (also from wrapped driver errors).
func (s Settings) redact(err error) error {
	if err == nil || s.Password == "" {
		return err
	}
	msg := err.Error()
	if !strings.Contains(msg, s.Password) && !strings.Contains(msg, url.QueryEscape(s.Password)) {
		return err
	}
	msg = strings.ReplaceAll(msg, s.Password, "***")
	msg = strings.ReplaceAll(msg, url.QueryEscape(s.Password), "***")
	return &redactedError{msg: msg, err: err}
}

type redactedError struct {
	msg string
	err error
}

func (e *redactedError) Error() string { return e.msg }
func (e *redactedError) Unwrap() error { return e.err }

// IsNotFound reports whether err is GORM's record-not-found.
func IsNotFound(err error) bool { return errors.Is(err, gorm.ErrRecordNotFound) }
