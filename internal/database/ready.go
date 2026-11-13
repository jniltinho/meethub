package database

import (
	"context"
	"errors"
)

// Readiness errors, shown by /api/v1/ready (no driver details).
var (
	ErrUnreachable       = errors.New("database unreachable")
	ErrMigrationsPending = errors.New("migrations pending")
)

// PendingChecker reports whether migrations are pending (Migrator implements it).
type PendingChecker interface {
	HasPending(ctx context.Context) (bool, error)
}

// ReadyCheck returns a readiness probe: the database answers a ping and no
// migration is pending.
func ReadyCheck(db *DB, migrations PendingChecker) func(context.Context) error {
	return func(ctx context.Context) error {
		if err := db.SQL.PingContext(ctx); err != nil {
			return ErrUnreachable
		}
		pending, err := migrations.HasPending(ctx)
		if err != nil {
			return ErrUnreachable
		}
		if pending {
			return ErrMigrationsPending
		}
		return nil
	}
}
