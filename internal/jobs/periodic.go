package jobs

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/hibiken/asynq"

	"github.com/jniltinho/meethub/internal/database"
)

// periodPayload is the payload of a periodic occurrence.
type periodPayload struct {
	PeriodStart time.Time `json:"period_start"`
}

// PeriodStart returns the UTC start of the period containing t.
func PeriodStart(t time.Time, period time.Duration) time.Time {
	return t.UTC().Truncate(period)
}

// schedule enqueues one occurrence per UTC period until ctx ends. Duplicate
// enqueues are reduced by the TaskID; the database claim is the guarantee.
func (r *Runner) schedule(ctx context.Context, tt TaskType) {
	for {
		start := PeriodStart(r.opts.Now(), tt.Period)
		if err := r.EnqueueOccurrence(ctx, tt.Name, start); err != nil && !errors.Is(err, asynq.ErrTaskIDConflict) {
			r.opts.Logger.Warn("schedule periodic task", "task", tt.Name, "period_start", start, "error", err.Error())
		}
		next := start.Add(tt.Period)
		wait := max(time.Until(next), time.Second)
		select {
		case <-ctx.Done():
			return
		case <-time.After(wait):
		}
	}
}

// EnqueueOccurrence enqueues the occurrence of a periodic task for the period
// starting at start.
func (r *Runner) EnqueueOccurrence(ctx context.Context, name string, start time.Time) error {
	r.mu.Lock()
	tt, ok := r.types[name]
	r.mu.Unlock()
	if !ok || tt.Period <= 0 {
		return fmt.Errorf("%s is not a periodic task", name)
	}
	payload, err := json.Marshal(periodPayload{PeriodStart: start.UTC()})
	if err != nil {
		return err
	}
	return r.Enqueue(ctx, asynq.NewTask(name, payload),
		asynq.TaskID(name+":"+start.UTC().Format(time.RFC3339)),
		asynq.Retention(tt.Period))
}

// periodic wraps a periodic task's handler with the period claim: the work
// runs only when this occurrence claims its period; a failure releases the
// claim so a retry can take it, a crash leaves it until the lease expires.
func (r *Runner) periodic(tt TaskType) func(context.Context, *asynq.Task) error {
	return func(ctx context.Context, t *asynq.Task) error {
		var p periodPayload
		if err := json.Unmarshal(t.Payload(), &p); err != nil {
			return fmt.Errorf("%s: bad payload: %w: %w", tt.Name, err, asynq.SkipRetry)
		}
		claimed, err := Claim(ctx, r.opts.DB, tt.Name, p.PeriodStart, r.opts.Now(), r.opts.ClaimLease)
		if err != nil {
			return err
		}
		if !claimed {
			r.opts.Logger.Debug("periodic task already claimed", "task", tt.Name, "period_start", p.PeriodStart)
			return nil
		}
		if err := tt.Handler(ctx, t); err != nil {
			_ = Release(context.WithoutCancel(ctx), r.opts.DB, tt.Name, p.PeriodStart, r.opts.Now())
			return err
		}
		return Complete(context.WithoutCancel(ctx), r.opts.DB, tt.Name, p.PeriodStart, r.opts.Now())
	}
}

// Claim atomically claims (taskType, periodStart) for lease. It returns false
// when the period is completed or claimed by an unexpired lease.
func Claim(ctx context.Context, db *database.DB, taskType string, periodStart, now time.Time, lease time.Duration) (bool, error) {
	periodStart, now = periodStart.UTC(), now.UTC()
	err := database.Translate(db.Gorm.WithContext(ctx).Exec(
		`INSERT INTO job_runs (task_type, period_start, claimed_until, attempts) VALUES (?, ?, ?, 1)`,
		taskType, periodStart, now.Add(lease)).Error)
	if err == nil {
		return true, nil
	}
	if !errors.Is(err, database.ErrDuplicate) {
		return false, fmt.Errorf("claim %s %s: %w", taskType, periodStart, err)
	}
	res := db.Gorm.WithContext(ctx).Exec(
		`UPDATE job_runs SET claimed_until = ?, attempts = attempts + 1
		 WHERE task_type = ? AND period_start = ? AND completed_at IS NULL AND claimed_until < ?`,
		now.Add(lease), taskType, periodStart, now)
	if res.Error != nil {
		return false, fmt.Errorf("claim %s %s: %w", taskType, periodStart, res.Error)
	}
	return res.RowsAffected == 1, nil
}

// Complete marks the period done.
func Complete(ctx context.Context, db *database.DB, taskType string, periodStart, now time.Time) error {
	return db.Gorm.WithContext(ctx).Exec(
		`UPDATE job_runs SET completed_at = ? WHERE task_type = ? AND period_start = ?`,
		now.UTC(), taskType, periodStart.UTC()).Error
}

// Release ends an unfinished claim now, so a retry may claim the period.
func Release(ctx context.Context, db *database.DB, taskType string, periodStart, now time.Time) error {
	return db.Gorm.WithContext(ctx).Exec(
		`UPDATE job_runs SET claimed_until = ? WHERE task_type = ? AND period_start = ? AND completed_at IS NULL`,
		now.UTC().Add(-time.Second), taskType, periodStart.UTC()).Error
}
