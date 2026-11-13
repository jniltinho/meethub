package ratelimit

import (
	"container/list"
	"context"
	"log/slog"
	"strconv"
	"sync"
	"time"

	"github.com/redis/go-redis/v9"
)

// Window allows at most Limit requests per key in ANY interval of length Per
// (a sliding-window log: exact, unlike a fixed window or a token bucket, which
// both allow bursts above the limit within one interval). It is shared by
// replicas through Redis and falls back to per-replica memory during outages.
// Only allowed requests are recorded; callers check idempotent repeats first.
type Window struct {
	Limit  int
	Per    time.Duration
	Prefix string // Redis key prefix, e.g. "meethub:posts:"
	rdb    *redis.Client
	logger *slog.Logger
	memory *memoryWindows
}

// NewWindow builds a sliding-window limiter; rdb may be nil (memory only).
func NewWindow(rdb *redis.Client, logger *slog.Logger, prefix string, limit int, per time.Duration) *Window {
	if logger == nil {
		logger = slog.New(slog.DiscardHandler)
	}
	return &Window{Limit: limit, Per: per, Prefix: prefix, rdb: rdb, logger: logger, memory: newMemoryWindows(100_000)}
}

// windowScript keeps the timestamps (Redis server clock, so skewed replicas
// agree) of the allowed requests of the last interval in a sorted set and
// answers {allowed, wait_ms}.
var windowScript = redis.NewScript(`
local limit = tonumber(ARGV[1])
local per = tonumber(ARGV[2])
local t = redis.call('TIME')
local now = t[1] * 1000 + math.floor(t[2] / 1000)
redis.call('ZREMRANGEBYSCORE', KEYS[1], '-inf', now - per)
local n = redis.call('ZCARD', KEYS[1])
if n < limit then
  redis.call('ZADD', KEYS[1], now, now .. ':' .. ARGV[3])
  redis.call('PEXPIRE', KEYS[1], per)
  return {1, 0}
end
local oldest = redis.call('ZRANGE', KEYS[1], 0, 0, 'WITHSCORES')
return {0, tonumber(oldest[2]) + per - now}
`)

var seq struct {
	mu sync.Mutex
	n  uint64
}

func nextNonce() string {
	seq.mu.Lock()
	defer seq.mu.Unlock()
	seq.n++
	return strconv.FormatUint(seq.n, 36) + strconv.FormatInt(time.Now().UnixNano(), 36)
}

// Allow records a request for key if fewer than Limit happened in the last Per;
// otherwise it returns false and how long until one expires.
func (w *Window) Allow(ctx context.Context, key string) (bool, time.Duration) {
	if w.rdb != nil {
		res, err := windowScript.Run(ctx, w.rdb, []string{w.Prefix + key}, w.Limit, w.Per.Milliseconds(), nextNonce()).Int64Slice()
		if err == nil && len(res) == 2 {
			if res[0] == 1 {
				return true, 0
			}
			return false, time.Duration(max(res[1], 1)) * time.Millisecond
		}
		msg := "unexpected reply"
		if err != nil {
			msg = err.Error()
		}
		w.logger.WarnContext(ctx, "rate limiter: redis unavailable, using per-replica memory windows", "error", msg)
	}
	return w.memory.allow(w.Prefix+key, w.Limit, w.Per, time.Now())
}

// memoryWindows is an LRU of sliding-window logs bounded to max keys.
type memoryWindows struct {
	mu    sync.Mutex
	max   int
	order *list.List
	items map[string]*list.Element
}

type memoryWindow struct {
	key   string
	times []time.Time // allowed requests, oldest first, at most Limit
}

func newMemoryWindows(max int) *memoryWindows {
	return &memoryWindows{max: max, order: list.New(), items: map[string]*list.Element{}}
}

func (m *memoryWindows) allow(key string, limit int, per time.Duration, now time.Time) (bool, time.Duration) {
	m.mu.Lock()
	defer m.mu.Unlock()
	el, ok := m.items[key]
	if !ok {
		el = m.order.PushFront(&memoryWindow{key: key})
		m.items[key] = el
		for m.order.Len() > m.max {
			old := m.order.Back()
			m.order.Remove(old)
			delete(m.items, old.Value.(*memoryWindow).key)
		}
	} else {
		m.order.MoveToFront(el)
	}
	w := el.Value.(*memoryWindow)
	i := 0
	for i < len(w.times) && !w.times[i].After(now.Add(-per)) {
		i++
	}
	w.times = w.times[i:]
	if len(w.times) < limit {
		w.times = append(w.times, now)
		return true, 0
	}
	return false, max(w.times[0].Add(per).Sub(now), time.Millisecond)
}
