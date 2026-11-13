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
// starting at start (extra options, e.g. ProcessAt, are appended).
func (r *Runner) EnqueueOccurrence(ctx context.Context, name string, start time.Time, extra ...asynq.Option) error {
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
	opts := append([]asynq.Option{
		asynq.TaskID(name + ":" + start.UTC().Format(time.RFC3339)),
		asynq.Retention(tt.Period),
	}, extra...)
	return r.Enqueue(ctx, asynq.NewTask(name, payload), opts...)
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
		claimed, leasedUntil, err := Claim(ctx, r.opts.DB, tt.Name, p.PeriodStart, r.opts.Now(), r.opts.ClaimLease)
		if err != nil {
			return err
		}
		if !claimed {
			if leasedUntil.IsZero() { // completed: nothing to do
				return nil
			}
			// another worker holds the period (or crashed holding it): look again when its lease ends,
			// so a crashed occurrence is still completed
			r.opts.Logger.Debug("periodic task leased elsewhere, rechecking later", "task", tt.Name, "until", leasedUntil)
			err := r.EnqueueOccurrence(context.WithoutCancel(ctx), tt.Name, p.PeriodStart,
				asynq.TaskID(fmt.Sprintf("%s:%s:recheck:%d", tt.Name, p.PeriodStart.UTC().Format(time.RFC3339), leasedUntil.UnixNano())),
				asynq.ProcessAt(leasedUntil.Add(time.Second)))
			if err != nil && !errors.Is(err, asynq.ErrTaskIDConflict) {
				return err
			}
			return nil
		}
		if err := tt.Handler(ctx, t); err != nil {
			_ = Release(context.WithoutCancel(ctx), r.opts.DB, tt.Name, p.PeriodStart, r.opts.Now())
			return err
		}
		return Complete(context.WithoutCancel(ctx), r.opts.DB, tt.Name, p.PeriodStart, r.opts.Now())
	}
}

// Claim atomically claims (taskType, periodStart) for lease. When it cannot,
// leasedUntil is the end of the current holder's lease, or zero when the
// period is already completed.
func Claim(ctx context.Context, db *database.DB, taskType string, periodStart, now time.Time, lease time.Duration) (claimed bool, leasedUntil time.Time, err error) {
	periodStart, now = periodStart.UTC(), now.UTC()
	err = database.Translate(db.Gorm.WithContext(ctx).Exec(
		`INSERT INTO job_runs (task_type, period_start, claimed_until, attempts) VALUES (?, ?, ?, 1)`,
		taskType, periodStart, now.Add(lease)).Error)
	if err == nil {
		return true, time.Time{}, nil
	}
	if !errors.Is(err, database.ErrDuplicate) {
		return false, time.Time{}, fmt.Errorf("claim %s %s: %w", taskType, periodStart, err)
	}
	res := db.Gorm.WithContext(ctx).Exec(
		`UPDATE job_runs SET claimed_until = ?, attempts = attempts + 1
		 WHERE task_type = ? AND period_start = ? AND completed_at IS NULL AND claimed_until < ?`,
		now.Add(lease), taskType, periodStart, now)
	if res.Error != nil {
		return false, time.Time{}, fmt.Errorf("claim %s %s: %w", taskType, periodStart, res.Error)
	}
	if res.RowsAffected == 1 {
		return true, time.Time{}, nil
	}
	var row struct {
		ClaimedUntil time.Time
		CompletedAt  *time.Time
	}
	if err := db.Gorm.WithContext(ctx).Raw(`SELECT claimed_until, completed_at FROM job_runs WHERE task_type = ? AND period_start = ?`,
		taskType, periodStart).Scan(&row).Error; err != nil {
		return false, time.Time{}, fmt.Errorf("claim %s %s: %w", taskType, periodStart, err)
	}
	if row.CompletedAt != nil {
		return false, time.Time{}, nil
	}
	return false, row.ClaimedUntil.UTC(), nil
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
