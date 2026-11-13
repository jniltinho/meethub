package ratelimit_test

import (
	"strconv"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/redis/go-redis/v9"

	"github.com/jniltinho/meethub/internal/ratelimit"
	"github.com/jniltinho/meethub/internal/testenv"
)

func uniqueKey() string { return "u" + strconv.FormatInt(time.Now().UnixNano(), 36) }

func checkBucket(t *testing.T, a, b *ratelimit.Window) {
	t.Helper()
	key := uniqueKey()
	var allowed atomic.Int32
	var wg sync.WaitGroup
	for i := range 31 { // concurrent requests spread over two "replicas"
		bk := a
		if i%2 == 1 {
			bk = b
		}
		wg.Go(func() {
			if ok, _ := bk.Allow(ctx, key); ok {
				allowed.Add(1)
			}
		})
	}
	wg.Wait()
	if n := allowed.Load(); n != 30 {
		t.Fatalf("allowed %d of 31, want 30", n)
	}
	ok, wait := a.Allow(ctx, key)
	if ok || wait <= 0 || wait > 10*time.Second {
		t.Fatalf("over the limit: ok=%v wait=%s", ok, wait)
	}
}

func TestWindowSharedInRedis(t *testing.T) {
	rdb := redis.NewClient(&redis.Options{Addr: testenv.RedisAddr(t)})
	defer func() { _ = rdb.Close() }()
	checkBucket(t,
		ratelimit.NewWindow(rdb, nil, "test:posts:", 30, 10*time.Second),
		ratelimit.NewWindow(rdb, nil, "test:posts:", 30, 10*time.Second))
}

func TestWindowMemoryFallback(t *testing.T) {
	t.Parallel()
	down := redis.NewClient(&redis.Options{Addr: "127.0.0.1:1", DialTimeout: 50 * time.Millisecond, MaxRetries: -1})
	defer func() { _ = down.Close() }()
	b := ratelimit.NewWindow(down, nil, "test:posts:", 30, 10*time.Second)
	checkBucket(t, b, b) // one replica: the same limit applies per replica
}

// TestNeverAboveLimitInAnyInterval: sliding over time, no interval of length Per
// ever holds more than Limit allowed requests (a fixed window or a token bucket would
// allow up to twice the limit). A synthetic clock keeps the test exact under load.
func TestNeverAboveLimitInAnyInterval(t *testing.T) {
	t.Parallel()
	w := ratelimit.NewWindow(nil, nil, "t:", 3, 200*time.Millisecond)
	start := time.Unix(1_700_000_000, 0)
	var allowed []time.Time
	for ms := 0; ms <= 700; ms += 3 { // a request every 3 ms for 700 ms
		now := start.Add(time.Duration(ms) * time.Millisecond)
		if ratelimit.AllowAt(w, "k", now) {
			allowed = append(allowed, now)
		}
	}
	for i := range allowed {
		n := 0
		for _, t2 := range allowed[i:] {
			if t2.Sub(allowed[i]) < 200*time.Millisecond {
				n++
			}
		}
		if n > 3 {
			t.Fatalf("%d requests allowed within 200ms starting at #%d", n, i)
		}
	}
	if len(allowed) < 9 { // 3 per 200 ms over 700 ms: the window slides
		t.Fatalf("only %d allowed over 700ms", len(allowed))
	}
}
