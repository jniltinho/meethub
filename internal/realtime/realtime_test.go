package realtime

import (
	"context"
	"encoding/json"
	"errors"
	"net"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/coder/websocket"
	"github.com/redis/go-redis/v9"

	"github.com/jniltinho/meethub/internal/chat"
	"github.com/jniltinho/meethub/internal/testenv"
)

var ctx = context.Background()

type fakeSessions struct {
	mu   sync.Mutex
	dead map[string]bool
	fail bool
}

func (f *fakeSessions) Live(_ context.Context, ids []string, _ time.Time) (map[string]bool, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.fail {
		return nil, errors.New("database down")
	}
	out := map[string]bool{}
	for _, id := range ids {
		out[id] = !f.dead[id]
	}
	return out, nil
}

func (f *fakeSessions) Touch(context.Context, string, time.Time) error { return nil }

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

type testHub struct {
	*Hub
	srv  *httptest.Server
	sess *fakeSessions
	clk  *clock
}

// newTestHub serves the hub; the test client names its user and session in the query.
func newTestHub(t *testing.T) *testHub {
	t.Helper()
	th := &testHub{sess: &fakeSessions{dead: map[string]bool{}}, clk: &clock{t: time.Unix(1_800_000_000, 0)}}
	th.Hub = NewHub(Options{Version: "test", Sessions: th.sess, Now: th.clk.Now})
	th.srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		q := r.URL.Query()
		if err := th.Serve(w, r, Identity{UserID: q.Get("user"), SessionID: q.Get("session"), SessionLastSeen: th.clk.Now()}); err != nil {
			http.Error(w, err.Error(), http.StatusServiceUnavailable)
		}
	}))
	t.Cleanup(th.srv.Close)
	return th
}

type client struct {
	ws *websocket.Conn
	t  *testing.T
}

func (th *testHub) dial(t *testing.T, user, session string) *client {
	t.Helper()
	url := "ws" + strings.TrimPrefix(th.srv.URL, "http") + "/?user=" + user + "&session=" + session
	ws, resp, err := websocket.Dial(ctx, url, nil)
	if resp != nil && resp.Body != nil {
		_ = resp.Body.Close()
	}
	if err != nil {
		t.Fatal(err)
	}
	ws.SetReadLimit(4 << 20)
	t.Cleanup(func() { _ = ws.CloseNow() })
	c := &client{ws: ws, t: t}
	if ev := c.next(); ev["event"] != "hello" || ev["seq"] != float64(0) {
		t.Fatalf("first message: %v", ev)
	}
	waitFor(t, func() bool { return th.connsOf(user) > 0 })
	return c
}

func (th *testHub) connsOf(user string) int {
	th.mu.RLock()
	defer th.mu.RUnlock()
	return len(th.byUser[user])
}

func (c *client) next() map[string]any {
	c.t.Helper()
	rctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	_, b, err := c.ws.Read(rctx)
	if err != nil {
		c.t.Fatalf("read: %v", err)
	}
	var m map[string]any
	if err := json.Unmarshal(b, &m); err != nil {
		c.t.Fatal(err)
	}
	return m
}

// closed waits for the server to close the connection and returns its status.
func (c *client) closed() websocket.StatusCode {
	c.t.Helper()
	rctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	for {
		if _, _, err := c.ws.Read(rctx); err != nil {
			return websocket.CloseStatus(err)
		}
	}
}

func (c *client) send(action string, seq int64, data any) {
	c.t.Helper()
	b, _ := json.Marshal(map[string]any{"action": action, "seq": seq, "data": data})
	if err := c.ws.Write(ctx, websocket.MessageText, b); err != nil {
		c.t.Fatal(err)
	}
}

func waitFor(t *testing.T, cond func() bool) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for !cond() {
		if time.Now().After(deadline) {
			t.Fatal("condition not reached")
		}
		time.Sleep(5 * time.Millisecond)
	}
}

func TestEnvelopesAndActions(t *testing.T) {
	th := newTestHub(t)
	c := th.dial(t, "ana", "s1")
	c.send("ping", 1, nil)
	if r := c.next(); r["status"] != "OK" || r["seq_reply"] != float64(1) || r["data"].(map[string]any)["text"] != "pong" {
		t.Fatalf("ping: %v", r)
	}
	c.send("drop_tables", 2, nil)
	if r := c.next(); r["status"] != "FAIL" || r["error"] != "unknown_action" {
		t.Fatalf("unknown action: %v", r)
	}
	th.Deliver(chat.Event{Name: "posted", Data: map[string]any{"n": 1}, Recipients: []string{"ana"}, ChannelID: "ch1"},
		chat.Event{Name: "posted", Data: map[string]any{"n": 2}, Recipients: []string{"bob"}},
		chat.Event{Name: "posted", Data: map[string]any{"n": 3}, Recipients: []string{"ana"}})
	a, b := c.next(), c.next()
	if a["seq"] != float64(1) || b["seq"] != float64(2) || a["broadcast"].(map[string]any)["channel_id"] != "ch1" ||
		b["data"].(map[string]any)["n"] != float64(3) {
		t.Fatalf("events: %v %v", a, b)
	}
	c.send("ping", 3, nil) // still open after the unknown action
	if r := c.next(); r["status"] != "OK" {
		t.Fatalf("after unknown action: %v", r)
	}
}

func TestEleventhConnectionClosesOldest(t *testing.T) {
	th := newTestHub(t)
	var clients []*client
	for i := range MaxConnsPerUser + 1 {
		clients = append(clients, th.dial(t, "ana", "s"+strconv.Itoa(i)))
	}
	if code := clients[0].closed(); code != websocket.StatusPolicyViolation {
		t.Fatalf("oldest closed with %d", code)
	}
	waitFor(t, func() bool { return th.connsOf("ana") == MaxConnsPerUser })
}

func TestFrameAndActionLimits(t *testing.T) {
	th := newTestHub(t)
	big := th.dial(t, "ana", "s1")
	_ = big.ws.Write(ctx, websocket.MessageText, []byte(`{"action":"ping","data":"`+strings.Repeat("x", MaxFrameBytes)+`"}`))
	if code := big.closed(); code != websocket.StatusMessageTooBig {
		t.Fatalf("frame over 64 KiB closed with %d", code)
	}
	flood := th.dial(t, "bob", "s2")
	for i := range MaxActionsPerSec + 1 {
		b, _ := json.Marshal(map[string]any{"action": "user_activity", "seq": i})
		if flood.ws.Write(ctx, websocket.MessageText, b) != nil {
			break
		}
	}
	if code := flood.closed(); code != websocket.StatusPolicyViolation {
		t.Fatalf("flood closed with %d", code)
	}
}

func TestSlowConsumer(t *testing.T) {
	th := newTestHub(t)
	slow := th.dial(t, "slow", "s1") // never reads again
	fast := th.dial(t, "fast", "s2")
	payload := strings.Repeat("x", 60<<10)
	var got atomic.Int32
	done := make(chan struct{})
	go func() {
		defer close(done)
		for got.Load() < 400 {
			rctx, cancel := context.WithTimeout(ctx, 5*time.Second)
			_, _, err := fast.ws.Read(rctx)
			cancel()
			if err != nil {
				return
			}
			got.Add(1)
		}
	}()
	start := time.Now()
	for range 400 { // 24 MB at a pace the fast client follows easily
		th.Deliver(chat.Event{Name: "posted", Data: map[string]any{"m": payload}, Recipients: []string{"slow", "fast"}})
		time.Sleep(time.Millisecond)
	}
	<-done
	if got.Load() != 400 || time.Since(start) > 8*time.Second {
		t.Fatalf("fast client got %d events in %s", got.Load(), time.Since(start))
	}
	waitFor(t, func() bool { return th.connsOf("slow") == 0 })
	_ = slow
}

func TestRemovalDropsQueuedEvents(t *testing.T) {
	h := NewHub(Options{})
	c := newConn("c1", Identity{UserID: "diego"}, nil, time.Now())
	h.register(c)
	h.Deliver(chat.Event{Name: "posted", Data: map[string]any{}, Recipients: []string{"diego"}, ChannelID: "finance"},
		chat.Event{Name: "posted", Data: map[string]any{}, Recipients: []string{"diego"}, ChannelID: "general"},
		chat.Event{Name: "user_removed", Data: map[string]any{"user_id": "diego", "channel_id": "finance"}, Recipients: []string{"diego", "ana"}, ChannelID: "finance"})
	var names []string
	for _, it := range c.take() {
		names = append(names, it.event+"@"+it.broadcast.ChannelID)
	}
	if strings.Join(names, ",") != "posted@general,user_removed@finance" {
		t.Fatalf("queue after removal: %v", names)
	}
}

func TestRevalidationClosesEndedSessions(t *testing.T) {
	th := newTestHub(t)
	a := th.dial(t, "ana", "s-ana")
	b := th.dial(t, "bob", "s-bob")
	th.sess.mu.Lock()
	th.sess.dead["s-ana"] = true // logout, password change, reset, disable or expiry
	th.sess.mu.Unlock()
	th.Revalidate(ctx)
	if code := a.closed(); code != websocket.StatusPolicyViolation {
		t.Fatalf("ended session closed with %d", code)
	}
	b.send("ping", 1, nil)
	if r := b.next(); r["status"] != "OK" {
		t.Fatal("a live session was closed")
	}
	// fail closed: validation unavailable for 15 s
	th.sess.mu.Lock()
	th.sess.fail = true
	th.sess.mu.Unlock()
	th.clk.Add(10 * time.Second)
	th.Revalidate(ctx)
	b.send("ping", 2, nil)
	if r := b.next(); r["status"] != "OK" {
		t.Fatal("closed before 15 s of failures")
	}
	th.clk.Add(5 * time.Second)
	th.Revalidate(ctx)
	if code := b.closed(); code != websocket.StatusTryAgainLater {
		t.Fatalf("fail closed with %d", code)
	}
}

func TestRecheckRunsAtOnce(t *testing.T) {
	th := newTestHub(t)
	a := th.dial(t, "ana", "s-ana")
	rctx, cancel := context.WithCancel(ctx)
	defer cancel()
	go func() { _ = th.RunRevalidation(rctx) }()
	th.sess.mu.Lock()
	th.sess.dead["s-ana"] = true
	th.sess.mu.Unlock()
	start := time.Now()
	th.Recheck()
	_ = a.closed()
	if time.Since(start) > 2*time.Second {
		t.Fatal("recheck waited for the next tick")
	}
}

func TestShutdownGoingAway(t *testing.T) {
	th := newTestHub(t)
	a := th.dial(t, "ana", "s1")
	sctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	go th.Shutdown(sctx)
	if code := a.closed(); code != websocket.StatusGoingAway {
		t.Fatalf("shutdown closed with %d", code)
	}
	url := "ws" + strings.TrimPrefix(th.srv.URL, "http") + "/?user=bob&session=s2"
	_, resp, err := websocket.Dial(ctx, url, nil)
	if resp != nil && resp.Body != nil {
		_ = resp.Body.Close()
	}
	if err == nil {
		t.Fatal("upgrade accepted during shutdown")
	}
}

// switchable simulates a Redis outage for one client: cut drops the open
// connections (as a real outage does) and refuses new ones until restored.
type switchable struct {
	down  atomic.Bool
	mu    sync.Mutex
	conns []net.Conn
}

func (s *switchable) cut() {
	s.down.Store(true)
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, c := range s.conns {
		_ = c.Close()
	}
	s.conns = nil
}

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
		s.mu.Lock()
		s.conns = append(s.conns, c)
		s.mu.Unlock()
		return flakyConn{Conn: c, s: s}, nil
	}})
}

func TestBusAcrossReplicas(t *testing.T) {
	addr := testenv.RedisAddr(t)
	channel := "test:events:" + strconv.FormatInt(time.Now().UnixNano(), 36)
	netA := &switchable{}
	ra, rb := netA.client(addr), redis.NewClient(&redis.Options{Addr: addr})
	defer func() { _ = ra.Close(); _ = rb.Close() }()
	hubA, hubB := newTestHub(t), newTestHub(t)
	busA, busB := NewBus(ra, hubA.Hub, "a", nil), NewBus(rb, hubB.Hub, "b", nil)
	busA.channel, busB.channel = channel, channel
	rctx, cancel := context.WithCancel(ctx)
	defer cancel()
	go func() { _ = busA.Run(rctx) }()
	go func() { _ = busB.Run(rctx) }()
	waitFor(t, func() bool { return busA.Healthy() && busB.Healthy() })

	ana := hubA.dial(t, "ana", "s1")     // replica A
	bruno := hubB.dial(t, "bruno", "s2") // replica B
	posted := chat.Event{Name: "posted", Data: map[string]any{"id": "p1"}, Recipients: []string{"ana", "bruno"}, ChannelID: "projects"}
	busB.Publish(ctx, posted)
	if ev := ana.next(); ev["event"] != "posted" || ev["data"].(map[string]any)["id"] != "p1" {
		t.Fatalf("ana through A: %v", ev)
	}
	if ev := bruno.next(); ev["event"] != "posted" {
		t.Fatalf("bruno locally: %v", ev)
	}

	// A loses Redis: local delivery continues, and A's clients get resync once it resubscribes
	netA.cut()
	waitFor(t, func() bool { return !busA.Healthy() })
	busA.Publish(ctx, chat.Event{Name: "posted", Data: map[string]any{"id": "local"}, Recipients: []string{"ana"}})
	if ev := ana.next(); ev["data"].(map[string]any)["id"] != "local" {
		t.Fatalf("local delivery during the outage: %v", ev)
	}
	busB.Publish(ctx, chat.Event{Name: "posted", Data: map[string]any{"id": "missed"}, Recipients: []string{"ana"}})
	netA.down.Store(false)
	ev := ana.next()
	if ev["event"] != "resync" {
		t.Fatalf("after resubscribing: %v", ev)
	}
	busB.Publish(ctx, chat.Event{Name: "posted", Data: map[string]any{"id": "p2"}, Recipients: []string{"ana"}})
	if ev := ana.next(); ev["data"].(map[string]any)["id"] != "p2" {
		t.Fatalf("after recovery: %v", ev)
	}

	// sessions_changed on B makes A revalidate at once
	go func() { _ = hubA.RunRevalidation(rctx) }()
	hubA.sess.mu.Lock()
	hubA.sess.dead["s1"] = true
	hubA.sess.mu.Unlock()
	start := time.Now()
	busB.SessionsChanged(ctx)
	_ = ana.closed()
	if time.Since(start) > 2*time.Second {
		t.Fatal("sessions_changed did not trigger an immediate revalidation")
	}
}
