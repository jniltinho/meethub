package jobs_test

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/hibiken/asynq"

	"github.com/jniltinho/meethub/internal/config"
	"github.com/jniltinho/meethub/internal/database"
	"github.com/jniltinho/meethub/internal/database/dbtest"
	"github.com/jniltinho/meethub/internal/jobs"
	"github.com/jniltinho/meethub/internal/testenv"
)

// newRunner returns a runner on the test Redis with its own queue (removed at
// the end) and fast retries.
func newRunner(t *testing.T, queue string, db *database.DB, types ...jobs.TaskType) *jobs.Runner {
	t.Helper()
	r, err := jobs.New(context.Background(), jobs.Options{
		Redis:          asynq.RedisClientOpt{Addr: testenv.RedisAddr(t)},
		ConnectTimeout: 5 * time.Second,
		Queue:          queue,
		Concurrency:    4,
		Shutdown:       5 * time.Second,
		RetryBase:      100 * time.Millisecond,
		RetryCap:       time.Second,
		DB:             db,
	})
	if err != nil {
		t.Fatal(err)
	}
	r.Register(types...)
	t.Cleanup(func() { _ = r.Close() })
	return r
}

func queueName(t *testing.T) string {
	t.Helper()
	b := make([]byte, 4)
	_, _ = rand.Read(b)
	q := "t_" + hex.EncodeToString(b)
	t.Cleanup(func() {
		insp := asynq.NewInspector(asynq.RedisClientOpt{Addr: testenv.RedisAddr(t)})
		_ = insp.DeleteQueue(q, true)
		_ = insp.Close()
	})
	return q
}

func start(t *testing.T, r *jobs.Runner) (stop func()) {
	t.Helper()
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- r.Run(ctx) }()
	return func() {
		cancel()
		if err := <-done; err != nil {
			t.Errorf("Run: %v", err)
		}
	}
}

func waitFor(t *testing.T, timeout time.Duration, cond func() bool) {
	t.Helper()
	deadline := time.Now().Add(timeout)
	for !cond() {
		if time.Now().After(deadline) {
			t.Fatal("condition not met in time")
		}
		time.Sleep(20 * time.Millisecond)
	}
}

func TestAttemptsAndBackoff(t *testing.T) {
	q := queueName(t)
	var (
		mu    sync.Mutex
		times []time.Time
	)
	r := newRunner(t, q, nil, jobs.TaskType{
		Name: "test:fail", MaxAttempts: 3,
		Handler: func(context.Context, *asynq.Task) error {
			mu.Lock()
			times = append(times, time.Now())
			mu.Unlock()
			return errors.New("boom")
		},
	})
	defer start(t, r)()
	if err := r.Enqueue(context.Background(), asynq.NewTask("test:fail", nil)); err != nil {
		t.Fatal(err)
	}
	insp := asynq.NewInspector(asynq.RedisClientOpt{Addr: testenv.RedisAddr(t)})
	defer func() { _ = insp.Close() }()
	waitFor(t, 30*time.Second, func() bool {
		tasks, _ := insp.ListArchivedTasks(q)
		return len(tasks) == 1
	})
	mu.Lock()
	defer mu.Unlock()
	if len(times) != 3 {
		t.Fatalf("ran %d times, want exactly 3", len(times))
	}
	first, second := times[1].Sub(times[0]), times[2].Sub(times[1])
	if second <= first {
		t.Fatalf("backoff did not grow: %s then %s", first, second)
	}
	tasks, _ := insp.ListArchivedTasks(q)
	if tasks[0].LastErr != "boom" {
		t.Fatalf("archived last error = %q", tasks[0].LastErr)
	}
}

func TestTaskSurvivesRestart(t *testing.T) {
	q := queueName(t)
	var ran atomic.Int32
	tt := jobs.TaskType{Name: "test:durable", MaxAttempts: 1, Handler: func(context.Context, *asynq.Task) error {
		ran.Add(1)
		return nil
	}}
	// first process: enqueues but never runs workers (it "restarts" before)
	before := newRunner(t, q, nil, tt)
	if err := before.Enqueue(context.Background(), asynq.NewTask(tt.Name, []byte(`{"id":"x"}`))); err != nil {
		t.Fatal(err)
	}
	_ = before.Close()
	// second process: runs the task left in Redis
	after := newRunner(t, q, nil, tt)
	defer start(t, after)()
	waitFor(t, 15*time.Second, func() bool { return ran.Load() == 1 })
}

func TestRedisUnavailable(t *testing.T) {
	t.Parallel()
	_, err := jobs.New(context.Background(), jobs.Options{
		Redis: asynq.RedisClientOpt{Addr: "127.0.0.1:1"}, ConnectTimeout: 500 * time.Millisecond,
	})
	if !errors.Is(err, jobs.ErrRedisUnavailable) {
		t.Fatalf("startup without Redis: %v", err)
	}
}

func TestEnqueueErrorWhenRedisLost(t *testing.T) {
	q := queueName(t)
	r := newRunner(t, q, nil, jobs.TaskType{Name: "test:x", Handler: func(context.Context, *asynq.Task) error { return nil }})
	_ = r.Close() // connection gone, as if Redis went away
	if err := r.Enqueue(context.Background(), asynq.NewTask("test:x", nil)); err == nil {
		t.Fatal("enqueue must fail without Redis")
	}
	if err := r.Ping(context.Background()); !errors.Is(err, jobs.ErrRedisUnavailable) {
		t.Fatalf("readiness ping: %v", err)
	}
}

func TestShutdownKeepsQueuedTasks(t *testing.T) {
	q := queueName(t)
	release := make(chan struct{})
	var started, finished atomic.Int32
	r := newRunner(t, q, nil, jobs.TaskType{Name: "test:slow", MaxAttempts: 1, Handler: func(context.Context, *asynq.Task) error {
		started.Add(1)
		<-release
		finished.Add(1)
		return nil
	}})
	stop := start(t, r)
	for range 6 { // concurrency 4: two stay queued
		if err := r.Enqueue(context.Background(), asynq.NewTask("test:slow", nil)); err != nil {
			t.Fatal(err)
		}
	}
	waitFor(t, 10*time.Second, func() bool { return started.Load() == 4 })
	go func() { time.Sleep(300 * time.Millisecond); close(release) }()
	stop() // running tasks finish within the shutdown timeout
	if finished.Load() != 4 {
		t.Fatalf("finished %d running tasks, want 4", finished.Load())
	}
	insp := asynq.NewInspector(asynq.RedisClientOpt{Addr: testenv.RedisAddr(t)})
	defer func() { _ = insp.Close() }()
	info, err := insp.GetQueueInfo(q)
	if err != nil {
		t.Fatal(err)
	}
	if info.Pending != 2 {
		t.Fatalf("pending after shutdown = %d, want 2 left in Redis", info.Pending)
	}
}

func TestPeriodicRunsOncePerPeriod(t *testing.T) {
	dbtest.Each(t, func(t *testing.T, db *database.DB) {
		q := queueName(t)
		var work atomic.Int32
		tt := jobs.TaskType{Name: "test:hourly", MaxAttempts: 3, Period: time.Hour, Handler: func(context.Context, *asynq.Task) error {
			work.Add(1)
			return nil
		}}
		a, b := newRunner(t, q, db, tt), newRunner(t, q, db, tt)
		defer start(t, a)()
		defer start(t, b)()
		period := jobs.PeriodStart(time.Now(), time.Hour)
		// two schedulers enqueue the same period, the second after the first completed
		_ = a.EnqueueOccurrence(context.Background(), tt.Name, period)
		waitFor(t, 10*time.Second, func() bool { return work.Load() == 1 })
		// a later enqueue of the same period (TaskID retention expired or another replica)
		if err := b.Enqueue(context.Background(), asynq.NewTask(tt.Name, []byte(`{"period_start":"`+period.Format(time.RFC3339)+`"}`))); err != nil {
			t.Fatal(err)
		}
		time.Sleep(time.Second)
		if work.Load() != 1 {
			t.Fatalf("work ran %d times for one period", work.Load())
		}
	})
}

func TestClaimLease(t *testing.T) {
	db := dbtest.Open(t, config.DriverSQLite)
	ctx := context.Background()
	period := time.Date(2026, 1, 1, 10, 0, 0, 0, time.UTC)
	now := period.Add(time.Minute)
	if ok, err := jobs.Claim(ctx, db, "x", period, now, 15*time.Minute); !ok || err != nil {
		t.Fatalf("first claim: %v %v", ok, err)
	}
	if ok, _ := jobs.Claim(ctx, db, "x", period, now.Add(time.Minute), 15*time.Minute); ok {
		t.Fatal("unexpired claim taken")
	}
	// the worker crashed: after the lease a retry takes the period over
	if ok, err := jobs.Claim(ctx, db, "x", period, now.Add(16*time.Minute), 15*time.Minute); !ok || err != nil {
		t.Fatalf("expired lease not taken over: %v %v", ok, err)
	}
	if err := jobs.Complete(ctx, db, "x", period, now.Add(17*time.Minute)); err != nil {
		t.Fatal(err)
	}
	if ok, _ := jobs.Claim(ctx, db, "x", period, now.Add(time.Hour), 15*time.Minute); ok {
		t.Fatal("completed period claimed again")
	}
}
