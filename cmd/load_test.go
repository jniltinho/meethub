//go:build load

package cmd

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"slices"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/coder/websocket"
	"github.com/redis/go-redis/v9"
	"golang.org/x/sync/errgroup"

	"github.com/jniltinho/meethub/internal/auth"
	"github.com/jniltinho/meethub/internal/identity"
	"github.com/jniltinho/meethub/internal/org"
	"github.com/jniltinho/meethub/internal/session"
	"github.com/jniltinho/meethub/internal/testenv"
)

// The scale budget of design D13 (make test-load): 1 000 connections, a
// 500-member channel with @channel, slow consumers and a Redis restart.
const (
	loadUsers      = 500 // all members of #general
	loadConnsEach  = 2   // 1 000 connections that read
	loadSlow       = 20  // extra connections that never read, of the first users
	loadFloodUsers = 40  // members of the flood channel: the slow consumers' users and as many authors
	loadRate       = 50  // posts per second (every author stays under the limit of 30 posts per 10 s)
	loadP99        = time.Second
	loadPassword   = "load test password" //nolint:gosec // throwaway install
)

// Post numbers by phase. The flood is 8 MB per connection: more than the
// kernel (4 MB by default) and the hub will buffer for a consumer that does
// not read.
const (
	loadSustained = 1                           // after the @channel post
	loadFlood     = loadSustained + 12*loadRate // 600 posts to everyone, beside 500 large ones to the flood channel
	loadRestart   = loadFlood + 500
	loadAfter     = loadRestart + 30
	loadPosts     = loadAfter + 10
)

type loadUser struct{ id, cookie, csrf string }

// recipient reports whether connection i must get post n.
func recipient(n, i int) bool {
	return n < loadFlood || n >= loadRestart || i%loadUsers < loadFloodUsers
}

// loadConn is a connection that reads; its fields belong to its reader until
// the reader ends.
type loadConn struct {
	ws      *websocket.Conn
	latency []time.Duration // per post number, request sent → event read; 0 = never delivered
	extra   int             // deliveries beyond the first
	resyncs atomic.Int32
}

type loadRun struct {
	t         *testing.T
	origin    string
	general   struct{ ID, TeamID string }
	users     []loadUser
	conns     []*loadConn
	client    *http.Client
	sent      []atomic.Int64 // UnixNano of each post's request
	hellos    atomic.Int64
	delivered atomic.Int64
	want      int64 // deliveries the phases so far must produce
	readers   sync.WaitGroup
	closing   atomic.Bool
	dropped   atomic.Int64 // reading connections the server closed
	dropCode  atomic.Int64 // close status of the last one
}

// TestLoad runs the real server in-process on SQLite against the test Redis.
// Run it without -race: it measures latency.
func TestLoad(t *testing.T) {
	redisAddr := testenv.RedisAddr(t)
	t.Chdir(t.TempDir())
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	addr := ln.Addr().String()
	_ = ln.Close()
	t.Setenv("MEETHUB_APP_DATA_DIR", t.TempDir())
	t.Setenv("MEETHUB_LOG_LEVEL", "error")
	t.Setenv("MEETHUB_HTTP_ADDR", addr)
	t.Setenv("MEETHUB_HTTP_PUBLIC_URL", "http://"+addr)
	t.Setenv("MEETHUB_REDIS_ADDR", redisAddr)

	l := &loadRun{
		t: t, origin: "http://" + addr, sent: make([]atomic.Int64, loadPosts),
		client: &http.Client{Timeout: 30 * time.Second, Transport: &http.Transport{MaxIdleConnsPerHost: 200}},
	}
	l.seed()

	c := &cli{in: strings.NewReader(""), out: io.Discard, errOut: os.Stderr}
	ctx, stop := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- c.runServer(ctx, false) }()
	// also after a fatal failure: the readers and the server end before the temporary directories go
	t.Cleanup(func() {
		l.disconnect()
		stop()
		if err := <-done; err != nil {
			t.Errorf("runServer: %v", err)
		}
	})
	l.waitFor("the server", 10*time.Second, func() bool {
		resp, err := l.client.Get(l.origin + "/api/v1/ready")
		if err != nil {
			return false
		}
		_ = resp.Body.Close()
		return resp.StatusCode == http.StatusOK
	})

	slow := l.connect()

	// 1. one @channel in the 500-member channel reaches every connection
	l.burst(0, loadSustained, 1, l.general.ID, l.users[:1], "@channel")
	l.settle("@channel", len(l.conns))
	l.report("@channel to 500 members", 0, loadSustained, true)
	for _, u := range l.users[1:26] {
		if n := l.mentions(u); n != 1 {
			t.Errorf("mention count of a member after @channel = %d, want 1", n)
		}
	}

	// 2. sustained posting to everyone while large messages flood the channel of the slow consumers
	flood := l.newChannel(l.users[:loadFloodUsers])
	var phase sync.WaitGroup
	phase.Go(func() {
		l.burst(loadSustained, loadFlood, loadRate, l.general.ID, l.users[loadFloodUsers:loadFloodUsers+100], "")
	})
	phase.Go(func() {
		l.burst(loadFlood, loadRestart, loadRate, flood, l.users[loadSlow:loadFloodUsers], strings.Repeat("x", 16000))
	})
	phase.Wait()
	l.settle("sustained posting and the flood",
		(loadFlood-loadSustained)*len(l.conns)+(loadRestart-loadFlood)*loadFloodUsers*loadConnsEach)
	l.report(fmt.Sprintf("%d posts/s to everyone", loadRate), loadSustained, loadFlood, true)
	l.report("16 KB posts beside slow consumers", loadFlood, loadRestart, true)
	closed := 0
	for _, ws := range slow {
		rctx, cancel := context.WithTimeout(ctx, 20*time.Second)
		for err = nil; err == nil; {
			_, _, err = ws.Read(rctx)
		}
		if rctx.Err() == nil {
			closed++ // the server ended it (buffer overflow or write timeout), not our deadline
		}
		cancel()
	}
	t.Logf("slow consumers closed by the server: %d of %d (threshold: all)", closed, loadSlow)
	if closed != loadSlow {
		t.Errorf("%d slow consumers were still open", loadSlow-closed)
	}

	// searches over the 1 100 messages posted so far, by 20 users (2 each: under the limit of 10 in 10 s)
	l.searches(40, l.users[loadFloodUsers:loadFloodUsers+20])

	// 3. Redis restarts under traffic: local delivery goes on, clients are told to resync afterwards
	rdb := redis.NewClient(&redis.Options{Addr: redisAddr})
	defer func() { _ = rdb.Close() }()
	_ = rdb.ShutdownNoSave(ctx).Err() // the reply is the closed connection; Compose restarts the service
	l.burst(loadRestart, loadAfter, 5, l.general.ID, l.users[loadFloodUsers:], "")
	l.waitFor("Redis to come back", 60*time.Second, func() bool { return rdb.Ping(ctx).Err() == nil })
	l.waitFor("resync on every connection", 60*time.Second, func() bool {
		return !slices.ContainsFunc(l.conns, func(c *loadConn) bool { return c.resyncs.Load() == 0 })
	})
	l.burst(loadAfter, loadPosts, 10, l.general.ID, l.users[loadFloodUsers:], "")
	l.settle("the Redis restart", (loadPosts-loadRestart)*len(l.conns))
	l.report("Redis restart under traffic", loadRestart, loadPosts, false)

	l.disconnect()
	l.exactlyOnce()
}

// disconnect closes the reading connections and waits for their readers.
func (l *loadRun) disconnect() {
	l.closing.Store(true)
	for _, c := range l.conns {
		if c != nil {
			_ = c.ws.CloseNow()
		}
	}
	l.readers.Wait()
}

// seed creates the organization, the users (all in #general) and a session
// for each, with a cheap password hash.
func (l *loadRun) seed() {
	l.t.Helper()
	if code := run([]string{"migrate", "up"}, strings.NewReader(""), io.Discard, os.Stderr); code != 0 {
		l.t.Fatalf("migrate up: exit %d", code)
	}
	c := &cli{in: strings.NewReader(""), out: io.Discard, errOut: os.Stderr}
	err := c.withServices(l.t.Context(), func(ctx context.Context, s *services) error {
		cheap := identity.NewHasher(identity.Argon2Params{MemoryKiB: 8, Time: 1, Threads: 1}, 0, 0)
		s.auth.Hasher, s.users.Hasher = cheap, cheap
		_, owner, err := s.auth.Initialize(ctx, auth.InitInput{
			OrganizationName: "Load", DisplayName: "User 0", Email: "u0@load.test", Password: loadPassword,
		}, "load")
		if err != nil {
			return err
		}
		ids := []string{owner.ID}
		for i := 1; i < loadUsers; i++ {
			m, err := s.users.Create(ctx, org.SystemActor, org.NewUser{
				Email: fmt.Sprintf("u%d@load.test", i), DisplayName: fmt.Sprintf("User %d", i), Role: org.RoleMember, Password: loadPassword,
			})
			if err != nil {
				return err
			}
			ids = append(ids, m.ID)
		}
		for _, id := range ids {
			token, _, err := s.sessions.Create(ctx, s.api.DB.Gorm, id, "127.0.0.1", "load", time.Now())
			if err != nil {
				return err
			}
			l.users = append(l.users, loadUser{id: id, cookie: s.api.CookieName() + "=" + token, csrf: session.CSRFToken(token)})
		}
		return s.api.DB.Gorm.WithContext(ctx).Raw("SELECT id, team_id FROM channels WHERE name = 'general'").Scan(&l.general).Error
	})
	if err != nil || l.general.ID == "" {
		l.t.Fatalf("seed: %v (channel %+v)", err, l.general)
	}
}

// connect opens the reading connections and the slow ones, which read only
// hello: the kernel buffers some megabytes for them (tcp_wmem), then the
// server's writes block and its own buffer fills.
func (l *loadRun) connect() []*websocket.Conn {
	l.t.Helper()
	url := "ws" + strings.TrimPrefix(l.origin, "http") + "/api/v1/websocket"
	dial := func(u loadUser) (*websocket.Conn, error) {
		ws, resp, err := websocket.Dial(l.t.Context(), url, &websocket.DialOptions{
			HTTPHeader: http.Header{"Origin": {l.origin}, "Cookie": {u.cookie}},
		})
		if resp != nil && resp.Body != nil {
			_ = resp.Body.Close()
		}
		if err != nil {
			return nil, err
		}
		ws.SetReadLimit(4 << 20)
		return ws, nil
	}
	l.conns = make([]*loadConn, loadUsers*loadConnsEach)
	var g errgroup.Group
	g.SetLimit(50)
	for i := range l.conns {
		g.Go(func() error {
			ws, err := dial(l.users[i%loadUsers])
			if err != nil {
				return err
			}
			c := &loadConn{ws: ws, latency: make([]time.Duration, loadPosts)}
			l.conns[i] = c
			l.readers.Go(func() { l.read(c) })
			return nil
		})
	}
	if err := g.Wait(); err != nil {
		l.t.Fatalf("connect: %v", err)
	}
	slow := make([]*websocket.Conn, loadSlow)
	for i := range slow {
		ws, err := dial(l.users[i])
		if err == nil {
			_, _, err = ws.Read(l.t.Context()) // hello: the hub knows the connection
		}
		if err != nil {
			l.t.Fatalf("slow consumer: %v", err)
		}
		l.t.Cleanup(func() { _ = ws.CloseNow() })
		slow[i] = ws
	}
	l.waitFor("hello on every connection", 30*time.Second, func() bool { return l.hellos.Load() == int64(len(l.conns)) })
	l.t.Logf("connected: %d connections of %d users, plus %d slow consumers", len(l.conns), loadUsers, loadSlow)
	return slow
}

// read records every delivery of a connection until it closes. It scans the
// bytes instead of decoding JSON: the flood alone is 8 MB per connection.
func (l *loadRun) read(c *loadConn) {
	for {
		_, b, err := c.ws.Read(context.Background())
		if err != nil {
			if !l.closing.Load() {
				l.dropCode.Store(int64(websocket.CloseStatus(err)))
				l.dropped.Add(1)
			}
			return
		}
		now := time.Now().UnixNano()
		switch {
		case bytes.HasPrefix(b, []byte(`{"event":"hello"`)):
			l.hellos.Add(1)
		case bytes.HasPrefix(b, []byte(`{"event":"resync"`)):
			c.resyncs.Add(1)
		case bytes.HasPrefix(b, []byte(`{"event":"posted"`)):
			i := bytes.LastIndex(b, []byte("load ")) // the server trims the message: no space before
			if i < 0 {
				continue
			}
			n := 0
			for i += len("load "); i < len(b) && b[i] >= '0' && b[i] <= '9' && n < loadPosts; i++ {
				n = n*10 + int(b[i]-'0')
			}
			if n >= loadPosts {
				continue
			}
			if c.latency[n] != 0 {
				c.extra++
				continue
			}
			c.latency[n] = max(time.Duration(now-l.sent[n].Load()), 1)
			l.delivered.Add(1)
		}
	}
}

// call sends an API request as u and requires the status; out may be nil.
func (l *loadRun) call(u loadUser, method, path, body string, want int, out any) {
	req, err := http.NewRequestWithContext(l.t.Context(), method, l.origin+"/api/v1"+path, strings.NewReader(body))
	if err != nil {
		l.t.Error(err)
		return
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Origin", l.origin)
	req.Header.Set("Cookie", u.cookie)
	req.Header.Set("X-CSRF-Token", u.csrf)
	resp, err := l.client.Do(req)
	if err != nil {
		l.t.Errorf("%s %s: %v", method, path, err)
		return
	}
	defer func() { _ = resp.Body.Close() }()
	b, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != want {
		l.t.Errorf("%s %s: %d %.200s", method, path, resp.StatusCode, b)
	} else if out != nil {
		if err := json.Unmarshal(b, out); err != nil {
			l.t.Errorf("%s %s: %v", method, path, err)
		}
	}
}

// newChannel creates a private channel of the given users (the first is its admin).
func (l *loadRun) newChannel(members []loadUser) string {
	var ch struct {
		ID string `json:"id"`
	}
	l.call(members[0], http.MethodPost, "/channels",
		fmt.Sprintf(`{"team_id":%q,"display_name":"Flood","type":"P"}`, l.general.TeamID), http.StatusCreated, &ch)
	for _, u := range members[1:] {
		l.call(members[0], http.MethodPost, "/channels/"+ch.ID+"/members", fmt.Sprintf(`{"user_id":%q}`, u.id), http.StatusNoContent, nil)
	}
	return ch.ID
}

// burst posts the messages numbered [from, to) to a channel at rate per
// second, authors in rotation; each message ends in its number.
func (l *loadRun) burst(from, to, rate int, channelID string, authors []loadUser, text string) {
	var wg sync.WaitGroup
	tick := time.NewTicker(time.Second / time.Duration(rate))
	defer tick.Stop()
	for n := from; n < to; n++ {
		<-tick.C
		wg.Go(func() {
			body := fmt.Sprintf(`{"channel_id":%q,"message":"%s load %d","pending_post_id":"load-%d"}`, channelID, text, n, n)
			l.sent[n].Store(time.Now().UnixNano())
			l.call(authors[n%len(authors)], http.MethodPost, "/posts", body, http.StatusCreated, nil)
		})
	}
	wg.Wait()
}

// searches times message searches that match most of the channel (the
// costliest case of the substring scan) and enforces the latency threshold.
func (l *loadRun) searches(n int, users []loadUser) {
	l.t.Helper()
	took := make([]time.Duration, n)
	for i := range took {
		var res struct {
			Posts []struct {
				ID string `json:"id"`
			} `json:"posts"`
			HasMore bool `json:"has_more"`
		}
		start := time.Now()
		l.call(users[i%len(users)], http.MethodPost, "/posts/search", `{"terms":"load 1"}`, http.StatusOK, &res)
		took[i] = time.Since(start)
		if len(res.Posts) != 20 || !res.HasMore {
			l.t.Errorf("search %d: %d results, more %v (want a full first page)", i, len(res.Posts), res.HasMore)
		}
	}
	slices.Sort(took)
	p99 := took[int(0.99*float64(n-1))].Round(time.Millisecond)
	l.t.Logf("message search: %d searches, latency p50 %s, p99 %s, max %s (threshold p99 ≤ %s)",
		n, took[n/2].Round(time.Millisecond), p99, took[n-1].Round(time.Millisecond), loadP99)
	if p99 > loadP99 {
		l.t.Errorf("message search: p99 %s is above %s", p99, loadP99)
	}
}

// settle waits until the phase's deliveries reached the reading connections.
func (l *loadRun) settle(what string, deliveries int) {
	l.t.Helper()
	l.want += int64(deliveries)
	l.waitFor("deliveries of "+what, 60*time.Second, func() bool { return l.delivered.Load() >= l.want })
}

func (l *loadRun) waitFor(what string, limit time.Duration, ok func() bool) {
	l.t.Helper()
	for deadline := time.Now().Add(limit); !ok(); time.Sleep(20 * time.Millisecond) {
		if time.Now().After(deadline) {
			l.t.Fatalf("timed out after %s waiting for %s (%d of %d deliveries; the server closed %d reading connections, last status %d)",
				limit, what, l.delivered.Load(), l.want, l.dropped.Load(), l.dropCode.Load())
		}
	}
}

// report prints the delivery latency of posts [from, to) and, when enforce
// is set, fails above the threshold. The readers are still running, so it
// reads only slots that settle already saw filled.
func (l *loadRun) report(phase string, from, to int, enforce bool) {
	l.t.Helper()
	var all []time.Duration
	for i, c := range l.conns {
		if recipient(from, i) {
			all = append(all, c.latency[from:to]...)
		}
	}
	slices.Sort(all)
	p := func(q float64) time.Duration { return all[int(q*float64(len(all)-1))].Round(time.Millisecond) }
	threshold := "not enforced: the rate limiter waits for Redis"
	if enforce {
		threshold = "threshold p99 ≤ " + loadP99.String()
	}
	l.t.Logf("%s: %d deliveries, latency p50 %s, p95 %s, p99 %s, max %s (%s)", phase, len(all), p(0.5), p(0.95), p(0.99), p(1), threshold)
	if enforce && p(0.99) > loadP99 {
		l.t.Errorf("%s: p99 %s is above %s", phase, p(0.99), loadP99)
	}
}

// exactlyOnce checks, after the readers ended, that every post reached each
// of its recipients' connections once and nobody else's.
func (l *loadRun) exactlyOnce() {
	l.t.Helper()
	missing, extra, leaked := 0, 0, 0
	for i, c := range l.conns {
		extra += c.extra
		for n, d := range c.latency {
			switch {
			case recipient(n, i) && d == 0:
				missing++
			case !recipient(n, i) && d != 0:
				leaked++
			}
		}
	}
	l.t.Logf("exactly once: %d posts, %d deliveries, %d missing, %d duplicated, %d to non-members (threshold: 0, 0 and 0)",
		loadPosts, l.delivered.Load(), missing, extra, leaked)
	if missing != 0 || extra != 0 || leaked != 0 {
		l.t.Errorf("deliveries: %d missing, %d duplicated, %d to non-members", missing, extra, leaked)
	}
	if n := l.dropped.Load(); n != 0 {
		l.t.Errorf("the server closed %d reading connections (last status %d)", n, l.dropCode.Load())
	}
}

// mentions returns u's mention count across their teams.
func (l *loadRun) mentions(u loadUser) int {
	l.t.Helper()
	var page struct {
		Items []struct {
			MentionCount int `json:"mention_count"`
		} `json:"items"`
	}
	l.call(u, http.MethodGet, "/users/me/teams", "", http.StatusOK, &page)
	n := 0
	for _, it := range page.Items {
		n += it.MentionCount
	}
	return n
}
