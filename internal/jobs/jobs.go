// Package jobs runs MeetHub's background work on Asynq (Redis, required):
// a registry of task types with their attempts, durable enqueueing, workers
// inside the server process, and a UTC period scheduler whose occurrences
// claim their period in the database so periodic work runs at most once per
// period across replicas.
package jobs

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"math/rand/v2"
	"sync"
	"time"

	"github.com/hibiken/asynq"
	"github.com/redis/go-redis/v9"

	"github.com/jniltinho/meethub/internal/database"
)

// DefaultQueue is the Asynq queue MeetHub uses.
const DefaultQueue = "meethub"

// ErrRedisUnavailable wraps failures to reach Redis.
var ErrRedisUnavailable = errors.New("redis unavailable")

// Queue enqueues background tasks; callers depend on this interface.
type Queue interface {
	Enqueue(ctx context.Context, task *asynq.Task, opts ...asynq.Option) error
}

// TaskType declares a task: its name, how many times it may run in total
// (MaxAttempts, including the first run) and its idempotent handler. A
// non-zero Period makes it a periodic task scheduled on UTC period starts.
type TaskType struct {
	Name        string
	MaxAttempts int
	Timeout     time.Duration
	Period      time.Duration
	Handler     func(ctx context.Context, t *asynq.Task) error
}

// Options configure a Runner.
type Options struct {
	Redis          asynq.RedisClientOpt
	ConnectTimeout time.Duration
	Queue          string // default DefaultQueue
	Concurrency    int
	Shutdown       time.Duration
	RetryBase      time.Duration // default 10s
	RetryCap       time.Duration // default 10m
	ClaimLease     time.Duration // default 15m
	Logger         *slog.Logger
	DB             *database.DB // for periodic claims
	Now            func() time.Time
}

// Runner owns the Asynq client, server and the period scheduler.
type Runner struct {
	opts   Options
	client *asynq.Client
	server *asynq.Server
	mux    *asynq.ServeMux
	rdb    *redis.Client
	types  map[string]TaskType
	mu     sync.Mutex
}

// New connects to Redis (retrying until the connect timeout) and prepares the
// runner. It fails when Redis cannot be reached in time.
func New(ctx context.Context, o Options) (*Runner, error) {
	if o.Queue == "" {
		o.Queue = DefaultQueue
	}
	if o.Concurrency <= 0 {
		o.Concurrency = 10
	}
	if o.Shutdown <= 0 {
		o.Shutdown = 15 * time.Second
	}
	if o.RetryBase <= 0 {
		o.RetryBase = 10 * time.Second
	}
	if o.RetryCap <= 0 {
		o.RetryCap = 10 * time.Minute
	}
	if o.ClaimLease <= 0 {
		o.ClaimLease = 15 * time.Minute
	}
	if o.Logger == nil {
		o.Logger = slog.New(slog.DiscardHandler)
	}
	if o.Now == nil {
		o.Now = time.Now
	}
	rdb := redis.NewClient(&redis.Options{
		Addr: o.Redis.Addr, Username: o.Redis.Username, Password: o.Redis.Password, DB: o.Redis.DB,
		DialTimeout: 5 * time.Second,
	})
	if err := waitRedis(ctx, rdb, o.Redis.Addr, o.ConnectTimeout); err != nil {
		_ = rdb.Close()
		return nil, err
	}
	r := &Runner{opts: o, rdb: rdb, mux: asynq.NewServeMux(), types: map[string]TaskType{}}
	r.client = asynq.NewClient(o.Redis)
	r.server = asynq.NewServer(o.Redis, asynq.Config{
		Concurrency:     o.Concurrency,
		Queues:          map[string]int{o.Queue: 1},
		RetryDelayFunc:  r.retryDelay,
		ShutdownTimeout: o.Shutdown,
		Logger:          slogAdapter{o.Logger},
		LogLevel:        asynq.WarnLevel,
		ErrorHandler: asynq.ErrorHandlerFunc(func(ctx context.Context, t *asynq.Task, err error) {
			retried, _ := asynq.GetRetryCount(ctx)
			maxRetry, _ := asynq.GetMaxRetry(ctx)
			if retried >= maxRetry {
				o.Logger.ErrorContext(ctx, "task failed permanently", "task", t.Type(), "attempts", retried+1, "error", err.Error())
				return
			}
			o.Logger.WarnContext(ctx, "task failed, will retry", "task", t.Type(), "attempt", retried+1, "error", err.Error())
		}),
	})
	return r, nil
}

func waitRedis(ctx context.Context, rdb *redis.Client, addr string, timeout time.Duration) error {
	deadline := time.Now().Add(timeout)
	delay := 250 * time.Millisecond
	for {
		pctx, cancel := context.WithTimeout(ctx, 3*time.Second)
		err := rdb.Ping(pctx).Err()
		cancel()
		if err == nil {
			return nil
		}
		if ctx.Err() != nil || time.Now().Add(delay).After(deadline) {
			return fmt.Errorf("%w: connect to redis %s (gave up after %s): %w", ErrRedisUnavailable, addr, timeout, err)
		}
		select {
		case <-ctx.Done():
			return fmt.Errorf("%w: %w", ErrRedisUnavailable, ctx.Err())
		case <-time.After(delay):
		}
		delay = min(delay*2, 5*time.Second)
	}
}

// Register adds task types; it must be called before Run.
func (r *Runner) Register(types ...TaskType) {
	r.mu.Lock()
	defer r.mu.Unlock()
	for _, tt := range types {
		if tt.MaxAttempts < 1 {
			tt.MaxAttempts = 1
		}
		r.types[tt.Name] = tt
		h := tt.Handler
		if tt.Period > 0 {
			h = r.periodic(tt)
		}
		r.mux.HandleFunc(tt.Name, h)
	}
}

// Enqueue stores a task in Redis with its type's attempts and timeout, unless
// opts override them.
func (r *Runner) Enqueue(ctx context.Context, task *asynq.Task, opts ...asynq.Option) error {
	r.mu.Lock()
	tt, ok := r.types[task.Type()]
	r.mu.Unlock()
	if !ok {
		return fmt.Errorf("enqueue %s: unknown task type", task.Type())
	}
	base := []asynq.Option{asynq.Queue(r.opts.Queue), asynq.MaxRetry(tt.MaxAttempts - 1)}
	if tt.Timeout > 0 {
		base = append(base, asynq.Timeout(tt.Timeout))
	}
	if _, err := r.client.EnqueueContext(ctx, task, append(base, opts...)...); err != nil {
		if errors.Is(err, asynq.ErrTaskIDConflict) {
			return err
		}
		return fmt.Errorf("%w: enqueue %s: %w", ErrRedisUnavailable, task.Type(), err)
	}
	return nil
}

// Run starts the workers and the period scheduler and blocks until ctx ends;
// then workers stop taking tasks and running ones get the shutdown timeout.
// Unfinished tasks stay in Redis.
func (r *Runner) Run(ctx context.Context) error {
	if err := r.server.Start(r.mux); err != nil {
		return fmt.Errorf("start workers: %w", err)
	}
	r.opts.Logger.Info("background workers started", "queue", r.opts.Queue, "concurrency", r.opts.Concurrency)
	var wg sync.WaitGroup
	r.mu.Lock()
	for _, tt := range r.types {
		if tt.Period > 0 {
			wg.Go(func() { r.schedule(ctx, tt) })
		}
	}
	r.mu.Unlock()
	<-ctx.Done()
	wg.Wait()
	r.server.Shutdown()
	r.opts.Logger.Info("background workers stopped")
	return nil
}

// Ping is the Redis readiness check.
func (r *Runner) Ping(ctx context.Context) error {
	if err := r.rdb.Ping(ctx).Err(); err != nil {
		return ErrRedisUnavailable
	}
	return nil
}

// Close releases the Redis connections.
func (r *Runner) Close() error {
	return errors.Join(r.client.Close(), r.rdb.Close())
}

// retryDelay is exponential (base·2ⁿ) with ±20 % jitter, capped.
func (r *Runner) retryDelay(n int, _ error, _ *asynq.Task) time.Duration {
	d := r.opts.RetryBase << min(n, 20)
	if d <= 0 || d > r.opts.RetryCap {
		d = r.opts.RetryCap
	}
	jitter := 0.8 + 0.4*rand.Float64() //nolint:gosec // jitter does not need a secure source
	return time.Duration(float64(d) * jitter)
}

// slogAdapter lets Asynq log through slog.
type slogAdapter struct{ l *slog.Logger }

func (a slogAdapter) Debug(args ...any) { a.l.Debug(fmt.Sprint(args...), "component", "asynq") }
func (a slogAdapter) Info(args ...any)  { a.l.Info(fmt.Sprint(args...), "component", "asynq") }
func (a slogAdapter) Warn(args ...any)  { a.l.Warn(fmt.Sprint(args...), "component", "asynq") }
func (a slogAdapter) Error(args ...any) { a.l.Error(fmt.Sprint(args...), "component", "asynq") }
func (a slogAdapter) Fatal(args ...any) {
	a.l.Error(fmt.Sprint(args...), "component", "asynq", "fatal", true)
}
