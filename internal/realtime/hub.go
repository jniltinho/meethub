package realtime

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"slices"
	"strings"
	"sync"
	"time"

	"github.com/coder/websocket"
	"github.com/oklog/ulid/v2"

	"github.com/jniltinho/meethub/internal/chat"
)

// Identity is the authenticated owner of a new connection.
type Identity struct {
	UserID          string
	OrgRole         string
	SessionID       string
	SessionLastSeen time.Time
}

// Presence records connections and activity (presence.Tracker).
type Presence interface {
	Connect(ctx context.Context, userID, connID string) error
	Disconnect(ctx context.Context, userID, connID string) error
	Heartbeat(ctx context.Context, userID, connID string) error
	Activity(ctx context.Context, userID string) error
}

// Handler answers the client actions that need the chat.
type Handler interface {
	Typing(ctx context.Context, sub chat.Subject, channelID, parentID string) error
	Statuses(ctx context.Context, sub chat.Subject, userIDs []string) (any, error)
}

// Sessions validates sessions in bulk and records socket activity.
type Sessions interface {
	// Live returns which of the session ids are still valid (not revoked, not
	// expired, user active).
	Live(ctx context.Context, sessionIDs []string, now time.Time) (map[string]bool, error)
	// Touch records activity on a session (at most once a minute is enforced by the hub).
	Touch(ctx context.Context, sessionID string, now time.Time) error
}

// Revalidation timing (spec: connections end with their session).
const (
	RevalidateEvery = 5 * time.Second
	FailClosedAfter = 15 * time.Second
	touchInterval   = time.Minute
)

// Options configures a Hub.
type Options struct {
	Version  string
	Presence Presence
	Handler  Handler
	Sessions Sessions
	Logger   *slog.Logger
	Now      func() time.Time
}

// Hub owns this replica's connections.
type Hub struct {
	opts Options

	mu       sync.RWMutex
	conns    map[string]*Conn
	byUser   map[string][]*Conn // oldest first
	stopping bool
	wg       sync.WaitGroup
	lastOK   time.Time
	recheck  chan struct{}
}

// NewHub builds a hub.
func NewHub(o Options) *Hub {
	if o.Logger == nil {
		o.Logger = slog.New(slog.DiscardHandler)
	}
	if o.Now == nil {
		o.Now = time.Now
	}
	return &Hub{opts: o, conns: map[string]*Conn{}, byUser: map[string][]*Conn{}, lastOK: o.Now(), recheck: make(chan struct{}, 1)}
}

// ErrStopping refuses upgrades during shutdown.
var ErrStopping = errors.New("server is shutting down")

// Serve upgrades the request (already authenticated, origin checked) and
// runs the connection until it closes.
func (h *Hub) Serve(w http.ResponseWriter, r *http.Request, ident Identity) error {
	h.mu.Lock()
	if h.stopping {
		h.mu.Unlock()
		return ErrStopping
	}
	h.wg.Add(1)
	h.mu.Unlock()
	defer h.wg.Done()

	ws, err := websocket.Accept(w, r, &websocket.AcceptOptions{InsecureSkipVerify: true}) // Origin checked by the caller
	if err != nil {
		return nil // Accept already answered the request
	}
	ws.SetReadLimit(MaxFrameBytes)
	// the connection outlives the request's context only until it closes
	ctx, cancel := context.WithCancel(context.WithoutCancel(r.Context()))
	defer cancel()
	c := newConn(ulid.Make().String(), ident, ws, h.opts.Now())
	hello, _ := json.Marshal(map[string]any{"connection_id": c.ID, "server_version": h.opts.Version})
	c.enqueue(item{event: "hello", data: hello, broadcast: Broadcast{UserID: c.UserID}, size: len(hello)}) // seq 0, before any event
	h.register(c)
	defer h.unregister(context.WithoutCancel(ctx))(c)
	h.presence(ctx, func(p Presence) error { return p.Connect(ctx, c.UserID, c.ID) })

	var wg sync.WaitGroup
	// closing never cancels ctx: the close handshake ends the read below
	wg.Go(func() {
		_ = c.writePump(ctx, func() { h.presence(ctx, func(p Presence) error { return p.Heartbeat(ctx, c.UserID, c.ID) }) })
	})
	h.readPump(ctx, c)
	c.Close(websocket.StatusNormalClosure, "")
	cancel()
	wg.Wait()
	return nil
}

func (h *Hub) presence(ctx context.Context, fn func(Presence) error) {
	if h.opts.Presence == nil {
		return
	}
	if err := fn(h.opts.Presence); err != nil {
		h.opts.Logger.DebugContext(ctx, "realtime: presence update failed", "error", err)
	}
}

func (h *Hub) register(c *Conn) {
	h.mu.Lock()
	h.conns[c.ID] = c
	list := append(h.byUser[c.UserID], c)
	var oldest *Conn
	if len(list) > MaxConnsPerUser {
		oldest, list = list[0], list[1:]
	}
	h.byUser[c.UserID] = list
	h.mu.Unlock()
	if oldest != nil {
		oldest.Close(websocket.StatusPolicyViolation, "too many connections")
	}
}

func (h *Hub) unregister(ctx context.Context) func(*Conn) {
	return func(c *Conn) { h.drop(ctx, c) }
}

func (h *Hub) drop(ctx context.Context, c *Conn) {
	h.mu.Lock()
	delete(h.conns, c.ID)
	list := slices.DeleteFunc(h.byUser[c.UserID], func(x *Conn) bool { return x == c })
	if len(list) == 0 {
		delete(h.byUser, c.UserID)
	} else {
		h.byUser[c.UserID] = list
	}
	h.mu.Unlock()
	h.presence(ctx, func(p Presence) error { return p.Disconnect(ctx, c.UserID, c.ID) })
}

// readPump reads actions until the connection fails, closes or floods.
func (h *Hub) readPump(ctx context.Context, c *Conn) {
	var window []time.Time
	for {
		typ, msg, err := c.ws.Read(ctx)
		if err != nil {
			return // closed, too big (closed by the library with 1009) or the context ended
		}
		now := time.Now()
		window = append(window, now)
		for len(window) > 0 && now.Sub(window[0]) >= time.Second {
			window = window[1:]
		}
		if len(window) > MaxActionsPerSec {
			c.Close(websocket.StatusPolicyViolation, "too many actions")
		}
		select {
		case <-c.done:
			continue // closing: keep reading (discarding) until the close handshake ends the read
		default:
		}
		if typ != websocket.MessageText {
			continue
		}
		var a Action
		if err := json.Unmarshal(msg, &a); err != nil || a.Action == "" {
			c.respond(a.Seq, nil, "invalid_action")
			continue
		}
		h.handle(ctx, c, a)
	}
}

func (h *Hub) handle(ctx context.Context, c *Conn, a Action) {
	sub := chat.Subject{UserID: c.UserID, OrgRole: c.OrgRole}
	switch a.Action {
	case "ping":
		h.presence(ctx, func(p Presence) error { return p.Heartbeat(ctx, c.UserID, c.ID) })
		c.respond(a.Seq, map[string]any{"text": "pong", "version": h.opts.Version, "server_time": h.opts.Now().UnixMilli()}, "")
	case "user_activity":
		h.touch(ctx, c)
		h.presence(ctx, func(p Presence) error { return p.Activity(ctx, c.UserID) })
		c.respond(a.Seq, nil, "")
	case "user_typing":
		h.touch(ctx, c)
		var d struct {
			ChannelID string `json:"channel_id"`
			ParentID  string `json:"parent_id"`
		}
		if json.Unmarshal(a.Data, &d) != nil || d.ChannelID == "" || h.opts.Handler == nil {
			c.respond(a.Seq, nil, "invalid_input")
			return
		}
		if err := h.opts.Handler.Typing(ctx, sub, d.ChannelID, d.ParentID); err != nil {
			c.respond(a.Seq, nil, actionError(err))
			return
		}
		c.respond(a.Seq, nil, "")
	case "get_statuses":
		var d struct {
			UserIDs []string `json:"user_ids"`
		}
		if json.Unmarshal(a.Data, &d) != nil || h.opts.Handler == nil {
			c.respond(a.Seq, nil, "invalid_input")
			return
		}
		out, err := h.opts.Handler.Statuses(ctx, sub, d.UserIDs)
		if err != nil {
			c.respond(a.Seq, nil, actionError(err))
			return
		}
		c.respond(a.Seq, out, "")
	default:
		c.respond(a.Seq, nil, "unknown_action")
	}
}

func actionError(err error) string {
	switch {
	case errors.Is(err, chat.ErrNotFound):
		return "not_found"
	case errors.Is(err, chat.ErrForbidden):
		return "forbidden"
	case errors.Is(err, chat.ErrInvalidInput):
		return "invalid_input"
	default:
		return "internal_error"
	}
}

// touch counts socket activity as session activity, at most once a minute.
func (h *Hub) touch(ctx context.Context, c *Conn) {
	now := h.opts.Now()
	c.mu.Lock()
	due := now.Sub(c.lastTouch) >= touchInterval
	if due {
		c.lastTouch = now
	}
	c.mu.Unlock()
	if due && h.opts.Sessions != nil {
		if err := h.opts.Sessions.Touch(ctx, c.SessionID, now); err != nil {
			h.opts.Logger.DebugContext(ctx, "realtime: session touch failed", "error", err)
		}
	}
}

// Deliver queues events for this replica's connections of their recipients.
// A user_removed or leave_team for the recipient first drops their queued
// events of that channel or team.
func (h *Hub) Deliver(events ...chat.Event) {
	for _, ev := range events {
		data, err := json.Marshal(ev.Data)
		if err != nil {
			h.opts.Logger.Error("realtime: encode event", "event", ev.Name, "error", err)
			continue
		}
		bc := Broadcast{ChannelID: ev.ChannelID, TeamID: ev.TeamID}
		removed, _ := ev.Data["user_id"].(string)
		h.mu.RLock()
		for _, u := range ev.Recipients {
			for _, c := range h.byUser[u] {
				if u == removed {
					switch ev.Name {
					case "user_removed":
						ch, _ := ev.Data["channel_id"].(string)
						c.dropScope(ch, "")
					case "leave_team":
						t, _ := ev.Data["team_id"].(string)
						c.dropScope("", t)
					}
				}
				c.enqueue(item{event: ev.Name, data: data, broadcast: bc, size: len(data) + len(ev.Name) + 64})
			}
		}
		h.mu.RUnlock()
	}
}

// Broadcast sends one event to every connection of this replica (resync).
func (h *Hub) Broadcast(name string) {
	data := json.RawMessage("{}")
	h.mu.RLock()
	defer h.mu.RUnlock()
	for _, c := range h.conns {
		c.enqueue(item{event: name, data: data, size: 64})
	}
}

// Count returns the number of open connections.
func (h *Hub) Count() int {
	h.mu.RLock()
	defer h.mu.RUnlock()
	return len(h.conns)
}

// Recheck asks for an immediate revalidation (a session ended somewhere).
func (h *Hub) Recheck() {
	select {
	case h.recheck <- struct{}{}:
	default:
	}
}

// RunRevalidation closes connections whose session ended, every
// RevalidateEvery (or at once after Recheck), independently of Redis; when
// validation keeps failing for FailClosedAfter it closes every connection.
func (h *Hub) RunRevalidation(ctx context.Context) error {
	tick := time.NewTicker(RevalidateEvery)
	defer tick.Stop()
	for {
		select {
		case <-ctx.Done():
			return nil
		case <-tick.C:
		case <-h.recheck:
		}
		h.Revalidate(ctx)
	}
}

// Revalidate runs one revalidation round.
func (h *Hub) Revalidate(ctx context.Context) {
	if h.opts.Sessions == nil {
		return
	}
	h.mu.RLock()
	conns := make([]*Conn, 0, len(h.conns))
	ids := make([]string, 0, len(h.conns))
	for _, c := range h.conns {
		conns = append(conns, c)
		ids = append(ids, c.SessionID)
	}
	h.mu.RUnlock()
	now := h.opts.Now()
	if len(ids) == 0 {
		h.lastOK = now
		return
	}
	slices.Sort(ids)
	live, err := h.opts.Sessions.Live(ctx, slices.Compact(ids), now)
	if err != nil {
		h.opts.Logger.WarnContext(ctx, "realtime: session revalidation failed", "error", err)
		if now.Sub(h.lastOK) >= FailClosedAfter {
			for _, c := range conns {
				c.Close(websocket.StatusTryAgainLater, "session check unavailable")
			}
		}
		return
	}
	h.lastOK = now
	for _, c := range conns {
		if !live[c.SessionID] {
			c.Close(websocket.StatusPolicyViolation, "session ended")
		}
	}
}

// Shutdown stops accepting connections, closes the open ones with "going
// away" and waits for them until ctx ends.
func (h *Hub) Shutdown(ctx context.Context) {
	h.mu.Lock()
	h.stopping = true
	conns := make([]*Conn, 0, len(h.conns))
	for _, c := range h.conns {
		conns = append(conns, c)
	}
	h.mu.Unlock()
	for _, c := range conns {
		c.Close(websocket.StatusGoingAway, "server shutting down")
	}
	done := make(chan struct{})
	go func() { h.wg.Wait(); close(done) }()
	select {
	case <-done:
	case <-ctx.Done():
	}
}

// SameOrigin reports whether the request's Origin equals the public origin.
func SameOrigin(r *http.Request, publicOrigin string) bool {
	o := r.Header.Get("Origin")
	return o != "" && o != "null" && strings.EqualFold(o, publicOrigin)
}
