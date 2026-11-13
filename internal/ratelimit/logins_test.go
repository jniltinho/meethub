package ratelimit_test

import (
	"context"
	"errors"
	"strconv"
	"testing"
	"time"

	"github.com/redis/go-redis/v9"

	"github.com/jniltinho/meethub/internal/ratelimit"
	"github.com/jniltinho/meethub/internal/testenv"
)

var ctx = context.Background()

func TestRateLimits(t *testing.T) {
	t.Parallel()
	l := ratelimit.NewLogins(nil, nil, 50)
	for range 5 {
		if err := l.Reserve(ctx, "1.1.1.1", "ana@acme.io"); err != nil {
			t.Fatal(err)
		}
	}
	var rl *ratelimit.RateLimitedError
	if err := l.Reserve(ctx, "9.9.9.9", "ana@acme.io"); !errors.As(err, &rl) || rl.RetryAfter <= 0 {
		t.Fatalf("6th attempt for one e-mail: %v", err)
	}
	// spraying unknown e-mails from one address
	for i := range 20 {
		_ = l.Reserve(ctx, "2.2.2.2", string(rune('a'+i))+"@spray.io")
	}
	if err := l.Reserve(ctx, "2.2.2.2", "fresh@spray.io"); !errors.As(err, &rl) {
		t.Fatalf("21st attempt from one address: %v", err)
	}
	// a success releases its reservation and clears the e-mail
	for range 4 {
		_ = l.Reserve(ctx, "3.3.3.3", "bob@acme.io")
	}
	l.Release(ctx, "3.3.3.3", "bob@acme.io")
	for range 5 {
		if err := l.Reserve(ctx, "4.4.4.4", "bob@acme.io"); err != nil {
			t.Fatalf("after a success the e-mail starts over: %v", err)
		}
	}
	for i := range 200 {
		_ = l.Reserve(ctx, "5.5.5.5", string(rune(0x100+i))+"@flood.io")
	}
	if n := ratelimit.MemoryKeys(l); n > 50 {
		t.Fatalf("limiter memory holds %d keys, bound is 50", n)
	}
}

func TestRateLimitsSharedInRedis(t *testing.T) {
	addr := testenv.RedisAddr(t)
	rdb := redis.NewClient(&redis.Options{Addr: addr})
	defer func() { _ = rdb.Close() }()
	email := strconv.FormatInt(time.Now().UnixNano(), 36) + "@acme.io"
	a, b := ratelimit.NewLogins(rdb, nil, 0), ratelimit.NewLogins(rdb, nil, 0) // two replicas
	for range 5 {
		_ = a.Reserve(ctx, "4.4.4.4", email)
	}
	if err := b.Reserve(ctx, "5.5.5.5", email); err == nil {
		t.Fatal("replica b does not see replica a's failures")
	}
	// Redis down: limits still apply, per replica, in memory
	down := redis.NewClient(&redis.Options{Addr: "127.0.0.1:1", DialTimeout: 100 * time.Millisecond, MaxRetries: -1})
	defer func() { _ = down.Close() }()
	c := ratelimit.NewLogins(down, nil, 0)
	for range 5 {
		_ = c.Reserve(ctx, "6.6.6.6", email)
	}
	if err := c.Reserve(ctx, "6.6.6.6", email); err == nil {
		t.Fatal("no limit while Redis is down")
	}
}
