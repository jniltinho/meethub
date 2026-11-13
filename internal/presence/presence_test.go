package presence

import (
	"context"
	"errors"
	"net"
	"strconv"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/redis/go-redis/v9"

	"github.com/jniltinho/meethub/internal/testenv"
)

var ctx = context.Background()

type clock struct {
	mu sync.Mutex
	t  time.Time
}

func (c *clock) Now() time.Time { c.mu.Lock(); defer c.mu.Unlock(); return c.t }
func (c *clock) Add(d time.Duration) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.t = c.t.Add(d)
}

type recorder struct {
	mu      sync.Mutex
	changes []Change
}

func (r *recorder) StatusChanged(_ context.Context, ch []Change) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.changes = append(r.changes, ch...)
}

func (r *recorder) count(userID, status string) int {
	r.mu.Lock()
	defer r.mu.Unlock()
	n := 0
	for _, c := range r.changes {
		if c.UserID == userID && c.Status == status {
			n++
		}
	}
	return n
}

// switchable makes a client whose connections fail while down is set (a Redis outage).
type switchable struct{ down atomic.Bool }

type flakyConn struct {
	net.Conn
	s *switchable
}

var errOutage = errors.New("simulated redis outage")

func (c flakyConn) Read(b []byte) (int, error) {
	if c.s.down.Load() {
		return 0, errOutage
	}
	return c.Conn.Read(b)
}

func (c flakyConn) Write(b []byte) (int, error) {
	if c.s.down.Load() {
		return 0, errOutage
	}
	return c.Conn.Write(b)
}

func (s *switchable) client(addr string) *redis.Client {
	return redis.NewClient(&redis.Options{Addr: addr, MaxRetries: -1, Dialer: func(ctx context.Context, network, addr string) (net.Conn, error) {
		if s.down.Load() {
			return nil, errOutage
		}
		c, err := (&net.Dialer{Timeout: time.Second}).DialContext(ctx, network, addr)
		if err != nil {
			return nil, err
		}
		return flakyConn{Conn: c, s: s}, nil
	}})
}

type env struct {
	clk    *clock
	rec    *recorder
	a, b   *Tracker // two replicas
	net    *switchable
	rdb    *redis.Client
	prefix string
}

func newEnv(t *testing.T) *env {
	t.Helper()
	addr := testenv.RedisAddr(t)
	e := &env{clk: &clock{t: time.Unix(1_800_000_000, 0)}, rec: &recorder{}, net: &switchable{},
		prefix: "test:presence:" + strconv.FormatInt(time.Now().UnixNano(), 36) + ":"}
	e.rdb = redis.NewClient(&redis.Options{Addr: addr})
	ra, rb := e.net.client(addr), e.net.client(addr)
	t.Cleanup(func() {
		keys, _ := e.rdb.Keys(ctx, e.prefix+"*").Result()
		if len(keys) > 0 {
			e.rdb.Del(ctx, keys...)
		}
		_ = ra.Close()
		_ = rb.Close()
		_ = e.rdb.Close()
	})
	opts := func(name string) Options {
		return Options{Replica: name, Notifier: e.rec, Now: e.clk.Now, Prefix: e.prefix}
	}
	e.a, e.b = New(ra, opts("a")), New(rb, opts("b"))
	return e
}

func status(t *testing.T, tr *Tracker, id string) Status {
	t.Helper()
	st, err := tr.Get(ctx, []string{id})
	if err != nil {
		t.Fatal(err)
	}
	return st[0]
}

func TestStatusSharedByReplicas(t *testing.T) {
	e := newEnv(t)
	if got := status(t, e.b, "ana").Status; got != Offline {
		t.Fatalf("unknown user: %s", got)
	}
	if err := e.a.Connect(ctx, "ana", "c1"); err != nil {
		t.Fatal(err)
	}
	if got := status(t, e.b, "ana").Status; got != Online {
		t.Fatalf("ana on replica a, read on b: %s", got)
	}
	if e.rec.count("ana", Online) != 1 {
		t.Fatal("online not published once")
	}
	_ = e.a.Connect(ctx, "ana", "c2") // a second tab changes nothing
	if e.rec.count("ana", Online) != 1 {
		t.Fatal("second tab republished online")
	}
}

func TestIdleAwayAndBack(t *testing.T) {
	e := newEnv(t)
	_ = e.a.Connect(ctx, "ana", "c1")
	for range 20 { // 5 minutes of pings without input
		e.clk.Add(15 * time.Second)
		_ = e.a.Heartbeat(ctx, "ana", "c1")
		if err := e.a.Tick(ctx); err != nil {
			t.Fatal(err)
		}
	}
	if got := status(t, e.b, "ana").Status; got != Away {
		t.Fatalf("after 5 idle minutes: %s", got)
	}
	_ = e.a.Activity(ctx, "ana")
	if got := status(t, e.b, "ana").Status; got != Online || e.rec.count("ana", Online) != 2 {
		t.Fatalf("activity: %s", got)
	}
}

// TestCrashDetectedWithin75s: a browser vanishes without closing its socket.
func TestCrashDetectedWithin75s(t *testing.T) {
	e := newEnv(t)
	start := e.clk.Now() // her last heartbeat
	_ = e.a.Connect(ctx, "ana", "c1")
	_ = e.b.Tick(ctx) // b is the sweeper
	e.clk.Add(7 * time.Second)
	for e.rec.count("ana", Offline) == 0 {
		e.clk.Add(SweepEvery)
		if err := e.b.Tick(ctx); err != nil {
			t.Fatal(err)
		}
		if e.clk.Now().Sub(start) > 90*time.Second {
			t.Fatal("never went offline")
		}
	}
	if took := e.clk.Now().Sub(start); took > 75*time.Second || took <= ConnTTL {
		t.Fatalf("offline after %s, want (60s, 75s]", took)
	}
}

func TestSimultaneousTabClose(t *testing.T) {
	e := newEnv(t)
	_ = e.a.Connect(ctx, "ana", "c1")
	_ = e.b.Connect(ctx, "ana", "c2")
	var wg sync.WaitGroup
	wg.Go(func() { _ = e.a.Disconnect(ctx, "ana", "c1") })
	wg.Go(func() { _ = e.b.Disconnect(ctx, "ana", "c2") })
	wg.Wait()
	if got := status(t, e.a, "ana").Status; got != Offline || e.rec.count("ana", Offline) != 1 {
		t.Fatalf("status %s, offline published %d times", got, e.rec.count("ana", Offline))
	}
	if n, _ := e.rdb.SCard(ctx, e.prefix+"users").Result(); n != 0 {
		t.Fatal("offline user left in the sweep set")
	}
}

func TestCloseHereOpenElsewhere(t *testing.T) {
	e := newEnv(t)
	for i := range 30 {
		user := "u" + strconv.Itoa(i)
		_ = e.a.Connect(ctx, user, "old")
		var wg sync.WaitGroup
		wg.Go(func() { _ = e.a.Disconnect(ctx, user, "old") })
		wg.Go(func() { _ = e.b.Connect(ctx, user, "new") })
		wg.Wait()
		if got := status(t, e.a, user).Status; got != Online {
			t.Fatalf("round %d ended %s", i, got)
		}
	}
}

func TestDoNotDisturb(t *testing.T) {
	e := newEnv(t)
	_ = e.a.Connect(ctx, "bruno", "c1")
	if err := e.a.SetDND(ctx, "bruno", e.clk.Now().Add(time.Hour)); err != nil {
		t.Fatal(err)
	}
	for range 10 {
		e.clk.Add(5 * time.Minute)
		_ = e.a.Activity(ctx, "bruno")
		_ = e.a.Heartbeat(ctx, "bruno", "c1")
		_ = e.a.Tick(ctx)
		if got := status(t, e.b, "bruno"); got.Status != DND || got.DNDUntil == 0 {
			t.Fatalf("active user in dnd: %+v", got)
		}
	}
	e.clk.Add(15 * time.Minute) // past the end time
	_ = e.a.Activity(ctx, "bruno")
	_ = e.a.Heartbeat(ctx, "bruno", "c1")
	if got := status(t, e.b, "bruno").Status; got != Online {
		t.Fatalf("after dnd ended: %s", got)
	}

	_ = e.a.SetDND(ctx, "bruno", time.Time{}) // until changed, also while offline
	_ = e.a.Disconnect(ctx, "bruno", "c1")
	e.clk.Add(24 * time.Hour)
	_ = e.a.Tick(ctx)
	if got := status(t, e.b, "bruno").Status; got != DND {
		t.Fatalf("indefinite dnd: %s", got)
	}
	_ = e.a.ClearDND(ctx, "bruno")
	if got := status(t, e.b, "bruno").Status; got != Offline {
		t.Fatalf("cleared dnd while offline: %s", got)
	}
	if err := e.a.SetDND(ctx, "bruno", e.clk.Now().Add(-time.Minute)); err == nil {
		t.Fatal("dnd ending in the past accepted")
	}
}

func TestSweeperLease(t *testing.T) {
	e := newEnv(t)
	if held, _ := e.a.lease(ctx); !held {
		t.Fatal("a could not take a free lease")
	}
	if held, _ := e.b.lease(ctx); held {
		t.Fatal("two sweepers")
	}
	e.clk.Add(LeaseTTL / 2)
	if held, _ := e.a.lease(ctx); !held { // renewal
		t.Fatal("holder could not renew")
	}
	e.clk.Add(LeaseTTL - time.Second)
	if held, _ := e.b.lease(ctx); held {
		t.Fatal("lease taken before it expired")
	}
	e.clk.Add(2 * time.Second) // a stopped renewing
	if held, _ := e.b.lease(ctx); !held {
		t.Fatal("lease not taken over after expiry")
	}
}

func TestRedisOutageAndRecovery(t *testing.T) {
	e := newEnv(t)
	_ = e.a.Connect(ctx, "ana", "c1")
	_ = e.b.Connect(ctx, "bruno", "c1")
	_ = e.b.Tick(ctx) // b sweeps
	published := len(e.rec.changes)

	e.net.down.Store(true)
	if err := e.a.Heartbeat(ctx, "ana", "c1"); err == nil {
		t.Fatal("heartbeat succeeded during the outage")
	}
	got := status(t, e.a, "ana")
	if got.Status != Online || !got.Stale {
		t.Fatalf("lookup during the outage: %+v", got)
	}
	if got := status(t, e.a, "carla"); got.Status != Offline || !got.Stale {
		t.Fatalf("unknown user during the outage: %+v", got)
	}
	_ = e.a.Connect(ctx, "carla", "c9") // connects while Redis is down
	e.clk.Add(2 * time.Minute)
	_ = e.a.Tick(ctx)
	_ = e.b.Tick(ctx)
	if len(e.rec.changes) != published {
		t.Fatal("changes published during the outage")
	}

	// Redis returns without its data
	keys, _ := e.rdb.Keys(ctx, e.prefix+"*").Result()
	e.rdb.Del(ctx, keys...)
	e.net.down.Store(false)
	start := e.clk.Now()
	for _, tr := range []*Tracker{e.a, e.b} {
		if err := tr.Tick(ctx); err != nil {
			t.Fatal(err)
		}
	}
	e.clk.Add(SweepEvery)
	_ = e.b.Tick(ctx)
	for _, u := range []string{"ana", "bruno", "carla"} {
		if got := status(t, e.b, u); got.Stale || (got.Status != Online && got.Status != Away) {
			t.Fatalf("%s after recovery: %+v", u, got)
		}
	}
	if e.clk.Now().Sub(start) > 75*time.Second {
		t.Fatal("convergence exceeded 75s")
	}
}

func TestBatchLimit(t *testing.T) {
	e := newEnv(t)
	ids := make([]string, 201)
	for i := range ids {
		ids[i] = strconv.Itoa(i)
	}
	if _, err := e.a.Get(ctx, ids); !errors.Is(err, ErrTooMany) {
		t.Fatalf("201 ids: %v", err)
	}
	if st, err := e.a.Get(ctx, ids[:200]); err != nil || len(st) != 200 {
		t.Fatalf("200 ids: %d %v", len(st), err)
	}
}
