// Package auth handles authentication: first-run setup, password login,
// server-side sessions, CSRF protection and login rate limits.
package auth

import (
	"context"
	"fmt"
	"time"

	"github.com/hibiken/asynq"

	"github.com/jniltinho/meethub/internal/database"
	"github.com/jniltinho/meethub/internal/jobs"
)

// SessionRetention is how long ended sessions are kept before purging.
const SessionRetention = 7 * 24 * time.Hour

// TaskSessionCleanup is the hourly session purge task type.
const TaskSessionCleanup = "session:cleanup"

// PurgeSessions deletes sessions that ended (absolute expiry, idle expiry or
// revocation) more than SessionRetention before now, and returns how many.
func PurgeSessions(ctx context.Context, db *database.DB, now time.Time, idle time.Duration) (int64, error) {
	cutoff := now.UTC().Add(-SessionRetention)
	res := db.Gorm.WithContext(ctx).Exec(
		`DELETE FROM sessions WHERE expires_at < ? OR last_seen_at < ? OR (revoked_at IS NOT NULL AND revoked_at < ?)`,
		cutoff, cutoff.Add(-idle), cutoff)
	if res.Error != nil {
		return 0, fmt.Errorf("purge sessions: %w", res.Error)
	}
	return res.RowsAffected, nil
}

// SessionCleanupTask is the periodic task purging old sessions every hour.
func SessionCleanupTask(db *database.DB, idle time.Duration, now func() time.Time) jobs.TaskType {
	return jobs.TaskType{
		Name:        TaskSessionCleanup,
		MaxAttempts: 3,
		Period:      time.Hour,
		Timeout:     5 * time.Minute,
		Handler: func(ctx context.Context, _ *asynq.Task) error {
			_, err := PurgeSessions(ctx, db, now(), idle)
			return err
		},
	}
}
