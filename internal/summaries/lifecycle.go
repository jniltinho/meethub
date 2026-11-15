package summaries

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"strings"
	"time"

	"github.com/hibiken/asynq"

	"github.com/jniltinho/meethub/internal/jobs"
	"github.com/jniltinho/meethub/internal/recordings"
)

// Periods of the lifecycle tasks (they run on the lifecycle workers, design D5 and D9).
const (
	ReconcileEvery = time.Minute
	CleanupEvery   = 24 * time.Hour
)

// LifecycleTasks are the reconciler and the daily cleanup.
func (s *Service) LifecycleTasks() []jobs.TaskType {
	return []jobs.TaskType{
		{Name: "summaries:reconcile", MaxAttempts: 1, Period: ReconcileEvery, Timeout: ReconcileEvery,
			Handler: func(ctx context.Context, _ *asynq.Task) error { return s.Reconcile(ctx) }},
		{Name: "summaries:cleanup", MaxAttempts: 3, Period: CleanupEvery, Timeout: 30 * time.Minute,
			Handler: func(ctx context.Context, _ *asynq.Task) error { return s.Cleanup(ctx) }},
	}
}

// CleanupLoop runs Cleanup now and every CleanupEvery until ctx ends: on a server without the lifecycle workers
// (recordings off), where summaries made earlier may have left objects. Every server may run it; it is idempotent.
func (s *Service) CleanupLoop(ctx context.Context) {
	t := time.NewTicker(CleanupEvery)
	defer t.Stop()
	for {
		if err := s.Cleanup(ctx); err != nil && ctx.Err() == nil {
			s.logger().WarnContext(ctx, "summaries: cleanup", "error", err)
		}
		select {
		case <-ctx.Done():
			return
		case <-t.C:
		}
	}
}

// Reconcile queues again every summary in an active state without a live claim (a lost enqueue, a server that
// stopped): ready, failed and cancelled ones are never restarted on their own.
func (s *Service) Reconcile(ctx context.Context) error {
	var ids []string
	if err := s.DB.Gorm.WithContext(ctx).Model(&recordings.Recording{}).
		Where("summary_status IN ? AND summary_claim_until < ?", recordings.SummaryActive, s.now().UnixMilli()).
		Limit(500).Pluck("id", &ids).Error; err != nil {
		return fmt.Errorf("summaries to reconcile: %w", err)
	}
	for _, id := range ids {
		s.Enqueue(ctx, strings.TrimSpace(id), "")
	}
	return nil
}

// Cleanup removes the derived objects that are due (design D9): of a summary posted, of a recording deleted,
// expired, failed or never summarizable, of an earlier composition, and of sources older than SourceKeep; and
// marks such sources expired. A summary being written keeps its objects.
func (s *Service) Cleanup(ctx context.Context) error {
	now := s.now().UnixMilli()
	if err := s.DB.Gorm.WithContext(ctx).Model(&recordings.Recording{}).
		Where("summary_source = ? AND summary_source_expires_at < ? AND summary_status NOT IN ?", recordings.SourceAvailable, now,
			recordings.SummaryActive).Update("summary_source", recordings.SourceExpired).Error; err != nil {
		return fmt.Errorf("expire summary sources: %w", err)
	}
	// a summary still waiting (or abandoned) when its sources expire ends there: nothing that old is sent
	var aged []recordings.Recording
	if err := s.DB.Gorm.WithContext(ctx).Where("summary_status IN ? AND summary_source_expires_at < ? AND summary_claim_until < ?",
		recordings.SummaryActive, now, now).Limit(500).Find(&aged).Error; err != nil {
		return fmt.Errorf("expired summaries: %w", err)
	}
	for i := range aged {
		rec := &aged[i]
		rec.Trim()
		r := s.DB.Gorm.WithContext(ctx).Model(&recordings.Recording{}).
			Where("id = ? AND summary_generation = ? AND summary_status IN ? AND summary_claim_until < ?", rec.ID,
				rec.SummaryGeneration, recordings.SummaryActive, now).
			Updates(map[string]any{"summary_status": recordings.SummaryFailed, "summary_error": codeExpired,
				"summary_source": recordings.SourceExpired})
		if r.Error == nil && r.RowsAffected == 1 {
			rec.SummaryStatus, rec.SummaryError, rec.SummarySource = recordings.SummaryFailed, codeExpired, recordings.SourceExpired
			s.announce(ctx, rec)
		}
	}
	var ids []string
	if err := s.DB.Gorm.WithContext(ctx).Model(&recordings.Object{}).Distinct("recording_id").Limit(1000).
		Pluck("recording_id", &ids).Error; err != nil {
		return fmt.Errorf("summary objects: %w", err)
	}
	var errs []error
	for _, id := range ids {
		id = strings.TrimSpace(id)
		var rows []recordings.Recording
		if err := s.DB.Gorm.WithContext(ctx).Where("id = ?", id).Limit(1).Find(&rows).Error; err != nil {
			errs = append(errs, err)
			continue
		}
		if len(rows) == 0 {
			s.removeObjects(ctx, id, nil)
			continue
		}
		rec := rows[0]
		rec.Trim()
		gone, err := recordings.Gone(ctx, s.DB.Gorm, id)
		if err != nil {
			errs = append(errs, err)
			continue
		}
		switch {
		case gone, rec.Status == recordings.StatusFailed, rec.SummaryStatus == recordings.SummaryReady:
			s.removeObjects(ctx, id, nil)
		case rec.Status != recordings.StatusReady:
			// still being composed: only an earlier attempt's objects are due
			s.removeObjects(ctx, id, func(o recordings.Object) bool { return o.Generation == rec.Generation })
		case isActive(rec.SummaryStatus):
			s.removeObjects(ctx, id, func(o recordings.Object) bool { return o.Generation == rec.Generation })
		case rec.SummarySource != recordings.SourceAvailable:
			s.removeObjects(ctx, id, nil)
		default:
			s.removeObjects(ctx, id, func(o recordings.Object) bool { return o.Generation == rec.Generation })
		}
	}
	return errors.Join(errs...)
}

func isActive(status string) bool { return slices.Contains(recordings.SummaryActive, status) }
