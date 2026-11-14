package cmd

import (
	"context"
	"testing"
	"time"

	"github.com/hibiken/asynq"
	"github.com/oklog/ulid/v2"

	"github.com/jniltinho/meethub/internal/database/dbtest"
	"github.com/jniltinho/meethub/internal/jobs"
	"github.com/jniltinho/meethub/internal/testenv"
)

// TestLifecycleWorkersNotStarved (add-meeting-recordings, design D9): with every worker of the main pool busy, the
// periodic sweep on the lifecycle queue still runs on time.
func TestLifecycleWorkersNotStarved(t *testing.T) {
	addr := testenv.RedisAddr(t)
	db := dbtest.Open(t, "sqlite")
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	suffix := ulid.Make().String()
	runner := func(queue string, n int) *jobs.Runner {
		r, err := jobs.New(ctx, jobs.Options{Redis: asynq.RedisClientOpt{Addr: addr}, ConnectTimeout: 5 * time.Second,
			Queue: queue + suffix, Concurrency: n, DB: db, PollInterval: 100 * time.Millisecond, Shutdown: time.Second})
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { _ = r.Close() })
		return r
	}
	main, lifecycle := runner("test-main-", 1), runner(LifecycleQueue+"-", 2)
	release := make(chan struct{})
	busy := make(chan struct{}, 8)
	main.Register(jobs.TaskType{Name: "test:busy" + suffix, Handler: func(ctx context.Context, _ *asynq.Task) error {
		busy <- struct{}{}
		select {
		case <-release:
		case <-ctx.Done():
		}
		return nil
	}})
	swept := make(chan time.Time, 8)
	lifecycle.Register(jobs.TaskType{Name: "test:sweep" + suffix, Period: time.Second, MaxAttempts: 1,
		Handler: func(context.Context, *asynq.Task) error {
			swept <- time.Now()
			return nil
		}})
	defer close(release)
	go func() { _ = main.Run(ctx) }()
	go func() { _ = lifecycle.Run(ctx) }()
	for range 4 { // more work than the main pool has workers
		if err := main.Enqueue(ctx, asynq.NewTask("test:busy"+suffix, nil)); err != nil {
			t.Fatal(err)
		}
	}
	select { // its one worker is busy (and three tasks wait): the main pool is saturated
	case <-busy:
	case <-time.After(10 * time.Second):
		t.Fatal("the main pool never started its work")
	}
	start := time.Now()
	deadline := time.After(10 * time.Second)
	for {
		select {
		case at := <-swept:
			if at.Before(start) {
				continue // a sweep from before the saturation proves nothing
			}
			if d := at.Sub(start); d > 3*time.Second {
				t.Fatalf("the sweep ran %s late", d)
			}
			return
		case <-deadline:
			t.Fatal("the sweep did not run while the main pool was saturated")
		}
	}
}
