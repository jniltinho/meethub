// Package ratelimit holds MeetHub's limiters: the failed-login reservation and a
// token bucket (posts, typing), shared by replicas through Redis with a bounded
// per-replica memory fallback while Redis is unavailable.
package ratelimit

import (
	"container/list"
	"context"
	"fmt"
	"log/slog"
	"sync"
	"time"

	"github.com/redis/go-redis/v9"
)

// Limit is a fixed-window failure limit.
type Limit struct {
	Max    int
	Window time.Duration
}

// Default login limits: per client address and per normalized e-mail.
var (
	DefaultIPLimit    = Limit{Max: 20, Window: 15 * time.Minute}
	DefaultEmailLimit = Limit{Max: 5, Window: 15 * time.Minute}
)

// RateLimitedError carries how long to wait.
type RateLimitedError struct{ RetryAfter time.Duration }

func (e *RateLimitedError) Error() string {
	return fmt.Sprintf("too many failed attempts, retry after %s", e.RetryAfter.Round(time.Second))
}

// counter counts failures per key within a window.
type counter interface {
	count(ctx context.Context, key string) (n int, ttl time.Duration, err error)
	incr(ctx context.Context, key string, window time.Duration) (int, time.Duration, error)
	decr(ctx context.Context, key string) error
	reset(ctx context.Context, key string) error
}

// Logins enforces failed-login limits in Redis (shared by replicas), falling
// back to a bounded in-memory counter while Redis fails.
type Logins struct {
	IP, Email Limit
	redis     counter
	memory    *memoryCounter
	logger    *slog.Logger
}

// NewLogins builds the failed-login limiter; rdb may be nil (memory only, for tests).
func NewLogins(rdb *redis.Client, logger *slog.Logger, memoryKeys int) *Logins {
	l := &Logins{IP: DefaultIPLimit, Email: DefaultEmailLimit, memory: newMemoryCounter(memoryKeys), logger: logger}
	if rdb != nil {
		l.redis = redisCounter{rdb}
	}
	if l.logger == nil {
		l.logger = slog.New(slog.DiscardHandler)
	}
	return l
}

func (l *Logins) keys(ip, email string) [2]struct {
	key   string
	limit Limit
} {
	return [2]struct {
		key   string
		limit Limit
	}{{"meethub:login:ip:" + ip, l.IP}, {"meethub:login:email:" + email, l.Email}}
}

// Reserve atomically counts an attempt for ip and email before the password is
// checked, so concurrent requests cannot all slip under the limit. Over the
// limit it returns a *RateLimitedError. A failed attempt keeps its
// reservation; a successful one calls Release.
func (l *Logins) Reserve(ctx context.Context, ip, email string) error {
	var limited *RateLimitedError
	for _, k := range l.keys(ip, email) {
		n, ttl, err := l.do(ctx).incr(ctx, k.key, k.limit.Window)
		if err != nil {
			n, ttl, _ = l.memory.incr(ctx, k.key, k.limit.Window)
		}
		if n > k.limit.Max && limited == nil {
			limited = &RateLimitedError{RetryAfter: max(ttl, time.Second)}
		}
	}
	if limited != nil {
		return limited
	}
	return nil
}

// Release undoes a successful attempt's reservation: the address keeps no
// count for it and the e-mail's failures are cleared.
func (l *Logins) Release(ctx context.Context, ip, email string) {
	keys := l.keys(ip, email)
	_ = l.do(ctx).decr(ctx, keys[0].key)
	_ = l.do(ctx).reset(ctx, keys[1].key)
}

// do returns Redis when configured and reachable, else memory (logging once per call site failure).
func (l *Logins) do(context.Context) counter {
	if l.redis == nil {
		return l.memory
	}
	return fallback{primary: l.redis, secondary: l.memory, logger: l.logger}
}

// fallback uses primary and switches to secondary on errors.
type fallback struct {
	primary, secondary counter
	logger             *slog.Logger
}

func (f fallback) count(ctx context.Context, key string) (int, time.Duration, error) {
	n, ttl, err := f.primary.count(ctx, key)
	if err != nil {
		f.logger.WarnContext(ctx, "rate limiter: redis unavailable, using per-replica memory limits", "error", err.Error())
		return f.secondary.count(ctx, key)
	}
	return n, ttl, nil
}

func (f fallback) incr(ctx context.Context, key string, w time.Duration) (int, time.Duration, error) {
	n, ttl, err := f.primary.incr(ctx, key, w)
	if err != nil {
		f.logger.WarnContext(ctx, "rate limiter: redis unavailable, using per-replica memory limits", "error", err.Error())
		return f.secondary.incr(ctx, key, w)
	}
	return n, ttl, nil
}

func (f fallback) decr(ctx context.Context, key string) error {
	_ = f.secondary.decr(ctx, key)
	return f.primary.decr(ctx, key)
}

func (f fallback) reset(ctx context.Context, key string) error {
	_ = f.secondary.reset(ctx, key)
	return f.primary.reset(ctx, key)
}

type redisCounter struct{ rdb *redis.Client }

func (r redisCounter) count(ctx context.Context, key string) (int, time.Duration, error) {
	n, err := r.rdb.Get(ctx, key).Int()
	if err == redis.Nil { //nolint:errorlint // go-redis returns the sentinel itself
		return 0, 0, nil
	}
	if err != nil {
		return 0, 0, err
	}
	ttl, err := r.rdb.PTTL(ctx, key).Result()
	return n, ttl, err
}

func (r redisCounter) incr(ctx context.Context, key string, window time.Duration) (int, time.Duration, error) {
	pipe := r.rdb.TxPipeline()
	n := pipe.Incr(ctx, key)
	pipe.ExpireNX(ctx, key, window)
	ttl := pipe.PTTL(ctx, key)
	if _, err := pipe.Exec(ctx); err != nil {
		return 0, 0, err
	}
	return int(n.Val()), ttl.Val(), nil
}

func (r redisCounter) decr(ctx context.Context, key string) error {
	// never below zero: decrement only an existing positive counter
	const script = `local v = tonumber(redis.call('GET', KEYS[1]) or '0'); if v > 0 then return redis.call('DECR', KEYS[1]) end; return 0`
	return r.rdb.Eval(ctx, script, []string{key}).Err()
}

func (r redisCounter) reset(ctx context.Context, key string) error { return r.rdb.Del(ctx, key).Err() }

// memoryCounter is an LRU of fixed-window counters bounded to max keys.
type memoryCounter struct {
	mu    sync.Mutex
	max   int
	order *list.List // front = most recent
	items map[string]*list.Element
	now   func() time.Time
}

type memoryItem struct {
	key     string
	n       int
	expires time.Time
}

func newMemoryCounter(max int) *memoryCounter {
	if max <= 0 {
		max = 100_000
	}
	return &memoryCounter{max: max, order: list.New(), items: map[string]*list.Element{}, now: time.Now}
}

func (m *memoryCounter) count(_ context.Context, key string) (int, time.Duration, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	el, ok := m.items[key]
	if !ok {
		return 0, 0, nil
	}
	it := el.Value.(*memoryItem)
	ttl := it.expires.Sub(m.now())
	if ttl <= 0 {
		m.order.Remove(el)
		delete(m.items, key)
		return 0, 0, nil
	}
	return it.n, ttl, nil
}

func (m *memoryCounter) incr(_ context.Context, key string, window time.Duration) (int, time.Duration, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	now := m.now()
	if el, ok := m.items[key]; ok {
		it := el.Value.(*memoryItem)
		if now.Before(it.expires) {
			it.n++
			m.order.MoveToFront(el)
			return it.n, it.expires.Sub(now), nil
		}
		m.order.Remove(el)
		delete(m.items, key)
	}
	m.items[key] = m.order.PushFront(&memoryItem{key: key, n: 1, expires: now.Add(window)})
	for m.order.Len() > m.max {
		oldest := m.order.Back()
		m.order.Remove(oldest)
		delete(m.items, oldest.Value.(*memoryItem).key)
	}
	return 1, window, nil
}

func (m *memoryCounter) decr(_ context.Context, key string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if el, ok := m.items[key]; ok {
		if it := el.Value.(*memoryItem); it.n > 0 {
			it.n--
		}
	}
	return nil
}

func (m *memoryCounter) reset(_ context.Context, key string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if el, ok := m.items[key]; ok {
		m.order.Remove(el)
		delete(m.items, key)
	}
	return nil
}

// size is the number of tracked keys (tests).
func (m *memoryCounter) size() int {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.order.Len()
}
