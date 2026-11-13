package database

import (
	"context"
	"database/sql"
	"embed"
	"errors"
	"fmt"
	"io/fs"
	"log/slog"
	"time"

	"github.com/pressly/goose/v3"
	"github.com/pressly/goose/v3/lock"

	"github.com/jniltinho/meethub/internal/config"
)

//go:embed migrations
var migrationsFS embed.FS

// DefaultLockTimeout bounds the wait for another process's migration lock.
const DefaultLockTimeout = 5 * time.Minute

// ErrLockTimeout is returned when the migration lock is not acquired in time.
var ErrLockTimeout = errors.New("could not acquire the migration lock (another process is migrating)")

// MigrationStatus describes one migration.
type MigrationStatus struct {
	Version   int64
	Name      string
	Applied   bool
	AppliedAt time.Time
}

// Migrator applies the embedded migrations of the database's dialect, holding
// a connection-scoped lock so concurrent processes migrate one at a time.
type Migrator struct {
	provider *goose.Provider
}

// MigratorOption configures a Migrator.
type MigratorOption func(*migratorOptions)

type migratorOptions struct {
	lockTimeout time.Duration
	logger      *slog.Logger
}

// WithLockTimeout sets how long to wait for another process's migration lock.
func WithLockTimeout(d time.Duration) MigratorOption {
	return func(o *migratorOptions) { o.lockTimeout = d }
}

// WithLogger logs each applied migration.
func WithLogger(l *slog.Logger) MigratorOption {
	return func(o *migratorOptions) { o.logger = l }
}

// MigrationSources returns the embedded migration files of a dialect.
func MigrationSources(dialect string) (fs.FS, error) {
	return fs.Sub(migrationsFS, "migrations/"+dialect)
}

// NewMigrator builds a Migrator for db.
func NewMigrator(db *DB, opts ...MigratorOption) (*Migrator, error) {
	o := migratorOptions{lockTimeout: DefaultLockTimeout}
	for _, opt := range opts {
		opt(&o)
	}
	sources, err := MigrationSources(db.Dialect)
	if err != nil {
		return nil, err
	}
	dialect, locker, err := newLocker(db, o.lockTimeout)
	if err != nil {
		return nil, err
	}
	popts := []goose.ProviderOption{goose.WithSessionLocker(locker), goose.WithDisableGlobalRegistry(true)}
	if o.logger != nil {
		popts = append(popts, goose.WithSlog(o.logger))
	}
	p, err := goose.NewProvider(dialect, db.SQL, sources, popts...)
	if err != nil {
		return nil, fmt.Errorf("migrations: %w", err)
	}
	return &Migrator{provider: p}, nil
}

// Up applies every pending migration and returns how many ran.
func (m *Migrator) Up(ctx context.Context) (int, error) {
	res, err := m.provider.Up(ctx)
	if err != nil {
		return len(res), fmt.Errorf("migrate up: %w", err)
	}
	return len(res), nil
}

// Down reverts the last applied migration.
func (m *Migrator) Down(ctx context.Context) (*MigrationStatus, error) {
	res, err := m.provider.Down(ctx)
	if err != nil {
		return nil, fmt.Errorf("migrate down: %w", err)
	}
	return &MigrationStatus{Version: res.Source.Version, Name: baseName(res.Source.Path)}, nil
}

// Status lists every migration and whether it is applied.
func (m *Migrator) Status(ctx context.Context) ([]MigrationStatus, error) {
	st, err := m.provider.Status(ctx)
	if err != nil {
		return nil, fmt.Errorf("migrate status: %w", err)
	}
	out := make([]MigrationStatus, 0, len(st))
	for _, s := range st {
		out = append(out, MigrationStatus{
			Version:   s.Source.Version,
			Name:      baseName(s.Source.Path),
			Applied:   s.State == goose.StateApplied,
			AppliedAt: s.AppliedAt.UTC(),
		})
	}
	return out, nil
}

// HasPending reports whether any migration is not applied.
func (m *Migrator) HasPending(ctx context.Context) (bool, error) {
	return m.provider.HasPending(ctx)
}

func baseName(p string) string {
	for i := len(p) - 1; i >= 0; i-- {
		if p[i] == '/' {
			return p[i+1:]
		}
	}
	return p
}

// newLocker returns the goose dialect and the session locker for db's dialect.
func newLocker(db *DB, timeout time.Duration) (goose.Dialect, lock.SessionLocker, error) {
	switch db.Dialect {
	case config.DriverSQLite:
		return goose.DialectSQLite3, &fileLocker{path: db.path + ".migrate.lock", timeout: timeout}, nil
	case config.DriverMariaDB:
		return goose.DialectMySQL, &mariadbLocker{timeout: timeout}, nil
	case config.DriverPostgres:
		l, err := lock.NewPostgresSessionLocker(
			lock.WithLockTimeout(1, uint64(max(timeout/time.Second, 1))),
			lock.WithUnlockTimeout(1, 30),
		)
		if err != nil {
			return "", nil, err
		}
		return goose.DialectPostgres, &pgLocker{SessionLocker: l}, nil
	default:
		return "", nil, fmt.Errorf("unsupported dialect %q", db.Dialect)
	}
}

// mariadbLocker holds GET_LOCK on the provider's dedicated connection.
type mariadbLocker struct{ timeout time.Duration }

const mariadbLockName = "meethub_migrate"

func (l *mariadbLocker) SessionLock(ctx context.Context, conn *sql.Conn) error {
	var got sql.NullInt64
	secs := max(int64(l.timeout/time.Second), 1)
	if err := conn.QueryRowContext(ctx, "SELECT GET_LOCK(?, ?)", mariadbLockName, secs).Scan(&got); err != nil {
		return fmt.Errorf("migration lock: %w", err)
	}
	if !got.Valid || got.Int64 != 1 {
		return ErrLockTimeout
	}
	return nil
}

func (l *mariadbLocker) SessionUnlock(ctx context.Context, conn *sql.Conn) error {
	_, err := conn.ExecContext(ctx, "SELECT RELEASE_LOCK(?)", mariadbLockName)
	return err
}

// pgLocker reports goose's lock timeout as ErrLockTimeout.
type pgLocker struct{ lock.SessionLocker }

func (l *pgLocker) SessionLock(ctx context.Context, conn *sql.Conn) error {
	if err := l.SessionLocker.SessionLock(ctx, conn); err != nil {
		return errors.Join(ErrLockTimeout, err)
	}
	return nil
}
