// Package realtime is the WebSocket event stream (design D5): one
// authenticated connection per browser tab carrying Mattermost-named events,
// fanned out across replicas through Redis.
package realtime

import (
	"context"
	"encoding/json"
	"errors"
	"sync"
	"time"

	"github.com/coder/websocket"
)

// Limits (spec: backpressure and limits).
const (
	MaxConnsPerUser  = 10
	MaxQueuedEvents  = 256
	MaxQueuedBytes   = 1 << 20
	MaxFrameBytes    = 64 << 10
	MaxActionsPerSec = 20
	PingEvery        = 30 * time.Second
	writeTimeout     = 10 * time.Second
)

// Broadcast tells the client the scope of an event (Mattermost shape).
type Broadcast struct {
	ChannelID string `json:"channel_id,omitempty"`
	TeamID    string `json:"team_id,omitempty"`
	UserID    string `json:"user_id,omitempty"`
}

// Envelope is a server event; Seq grows by one per connection.
type Envelope struct {
	Event     string          `json:"event"`
	Data      json.RawMessage `json:"data"`
	Broadcast Broadcast       `json:"broadcast"`
	Seq       int64           `json:"seq"`
}

// Action is a client request.
type Action struct {
	Action string          `json:"action"`
	Seq    int64           `json:"seq"`
	Data   json.RawMessage `json:"data"`
}

// Response answers an Action.
type Response struct {
	Status   string `json:"status"` // OK or FAIL
	SeqReply int64  `json:"seq_reply"`
	Data     any    `json:"data,omitempty"`
	Error    string `json:"error,omitempty"`
}

// item is a queued outgoing message: an event (seq assigned when written) or
// an encoded response.
type item struct {
	event     string
	data      json.RawMessage
	broadcast Broadcast
	response  []byte
	size      int
}

// Conn is one WebSocket connection.
type Conn struct {
	ID        string
	UserID    string
	OrgRole   string
	SessionID string
	opened    time.Time

	ws *websocket.Conn

	mu     sync.Mutex
	queue  []item
	bytes  int
	closed bool
	wake   chan struct{}
	done   chan struct{}
	seq    int64

	lastTouch time.Time // last session activity recorded (at most once a minute)
}

func newConn(id string, ident Identity, ws *websocket.Conn, now time.Time) *Conn {
	return &Conn{
		ID: id, UserID: ident.UserID, OrgRole: ident.OrgRole, SessionID: ident.SessionID, opened: now,
		ws: ws, wake: make(chan struct{}, 1), done: make(chan struct{}), lastTouch: ident.SessionLastSeen,
	}
}

// errOverflow closes a slow consumer.
var errOverflow = errors.New("outgoing buffer full")

// enqueue adds an item; a full buffer closes the connection (the client
// recovers through the API) instead of slowing anybody else.
func (c *Conn) enqueue(it item) {
	c.mu.Lock()
	if c.closed {
		c.mu.Unlock()
		return
	}
	if len(c.queue)+1 > MaxQueuedEvents || c.bytes+it.size > MaxQueuedBytes {
		c.mu.Unlock()
		c.Close(websocket.StatusPolicyViolation, errOverflow.Error())
		return
	}
	c.queue = append(c.queue, it)
	c.bytes += it.size
	c.mu.Unlock()
	select {
	case c.wake <- struct{}{}:
	default:
	}
}

// dropScope removes queued events of a channel or team the user just left
// (they committed before the removal, but are no longer useful to deliver).
func (c *Conn) dropScope(channelID, teamID string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	kept := c.queue[:0]
	c.bytes = 0
	for _, it := range c.queue {
		if it.response == nil && ((channelID != "" && it.broadcast.ChannelID == channelID) || (teamID != "" && it.broadcast.TeamID == teamID)) {
			continue
		}
		kept = append(kept, it)
		c.bytes += it.size
	}
	clear(c.queue[len(kept):])
	c.queue = kept
}

func (c *Conn) take() []item {
	c.mu.Lock()
	defer c.mu.Unlock()
	out := c.queue
	c.queue, c.bytes = nil, 0
	return out
}

// Close closes the connection once with a status and reason.
func (c *Conn) Close(code websocket.StatusCode, reason string) {
	c.mu.Lock()
	if c.closed {
		c.mu.Unlock()
		return
	}
	c.closed = true
	c.queue, c.bytes = nil, 0
	c.mu.Unlock()
	close(c.done)
	go func() { _ = c.ws.Close(code, reason) }()
}

// Done is closed when the connection is closed.
func (c *Conn) Done() <-chan struct{} { return c.done }

// writePump writes queued items and pings until the connection closes.
func (c *Conn) writePump(ctx context.Context, onPing func()) error {
	ping := time.NewTicker(PingEvery)
	defer ping.Stop()
	for {
		select {
		case <-ctx.Done():
			return nil
		case <-c.done:
			return nil
		case <-ping.C:
			pctx, cancel := context.WithTimeout(ctx, writeTimeout)
			err := c.ws.Ping(pctx)
			cancel()
			if err != nil {
				c.Close(websocket.StatusGoingAway, "ping timeout")
				return nil
			}
			onPing()
		case <-c.wake:
			for _, it := range c.take() {
				if err := c.write(ctx, it); err != nil {
					c.Close(websocket.StatusGoingAway, "write failed")
					return nil
				}
			}
		}
	}
}

func (c *Conn) write(ctx context.Context, it item) error {
	msg := it.response
	if msg == nil {
		var err error
		msg, err = json.Marshal(Envelope{Event: it.event, Data: it.data, Broadcast: it.broadcast, Seq: c.seq})
		if err != nil {
			return err
		}
		c.seq++
	}
	wctx, cancel := context.WithTimeout(ctx, writeTimeout)
	defer cancel()
	return c.ws.Write(wctx, websocket.MessageText, msg)
}

func (c *Conn) respond(seq int64, data any, errCode string) {
	r := Response{Status: "OK", SeqReply: seq, Data: data}
	if errCode != "" {
		r = Response{Status: "FAIL", SeqReply: seq, Error: errCode}
	}
	b, err := json.Marshal(r)
	if err != nil {
		return
	}
	c.enqueue(item{response: b, size: len(b)})
}
