package realtime

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"sync/atomic"
	"time"

	"github.com/redis/go-redis/v9"

	"github.com/jniltinho/meethub/internal/chat"
)

// DefaultChannel is the Redis Pub/Sub channel shared by the replicas.
const DefaultChannel = "meethub:events"

// wireEvent is an event on the bus (chat.Event hides its routing fields from JSON).
type wireEvent struct {
	Name       string         `json:"name"`
	Data       map[string]any `json:"data"`
	Recipients []string       `json:"recipients"`
	ChannelID  string         `json:"channel_id,omitempty"`
	TeamID     string         `json:"team_id,omitempty"`
}

type wireMessage struct {
	Replica string      `json:"replica"`
	Events  []wireEvent `json:"events,omitempty"`
	// SessionsChanged asks every replica to revalidate now (logout, password
	// change, reset, disable): a faster path than the 5-second revalidation.
	SessionsChanged bool `json:"sessions_changed,omitempty"`
}

// Bus delivers events to local connections first and then to the other
// replicas through Redis; it implements chat.Publisher.
type Bus struct {
	rdb     *redis.Client
	hub     *Hub
	replica string
	channel string
	logger  *slog.Logger
	healthy atomic.Bool
}

// NewBus builds a bus for this replica.
func NewBus(rdb *redis.Client, hub *Hub, replica string, logger *slog.Logger) *Bus {
	if logger == nil {
		logger = slog.New(slog.DiscardHandler)
	}
	return &Bus{rdb: rdb, hub: hub, replica: replica, channel: DefaultChannel, logger: logger}
}

// Publish delivers locally (even while Redis is down) and then publishes.
func (b *Bus) Publish(ctx context.Context, events ...chat.Event) {
	b.hub.Deliver(events...)
	msg := wireMessage{Replica: b.replica, Events: make([]wireEvent, len(events))}
	for i, e := range events {
		msg.Events[i] = wireEvent{Name: e.Name, Data: e.Data, Recipients: e.Recipients, ChannelID: e.ChannelID, TeamID: e.TeamID}
	}
	b.send(ctx, msg)
}

// SessionsChanged triggers an immediate revalidation on every replica.
func (b *Bus) SessionsChanged(ctx context.Context) {
	b.hub.Recheck()
	b.send(ctx, wireMessage{Replica: b.replica, SessionsChanged: true})
}

func (b *Bus) send(ctx context.Context, msg wireMessage) {
	payload, err := json.Marshal(msg)
	if err != nil {
		b.logger.ErrorContext(ctx, "realtime: encode bus message", "error", err)
		return
	}
	if err := b.rdb.Publish(ctx, b.channel, payload).Err(); err != nil {
		b.logger.WarnContext(ctx, "realtime: publish failed; other replicas miss these events until their clients resync", "error", err)
	}
}

// Healthy reports whether the subscription is active.
func (b *Bus) Healthy() bool { return b.healthy.Load() }

// Run receives the other replicas' messages until ctx ends. When the
// subscription is lost it resubscribes with backoff, and once subscribed
// again it sends resync to every local connection (events may have been missed).
func (b *Bus) Run(ctx context.Context) error {
	backoff := 100 * time.Millisecond
	lost := false
	for ctx.Err() == nil {
		b.runOnce(ctx, &lost, &backoff)
		select {
		case <-ctx.Done():
		case <-time.After(backoff):
			backoff = min(backoff*2, 5*time.Second)
		}
	}
	return nil
}

// runOnce subscribes until the subscription fails, then records the loss.
func (b *Bus) runOnce(ctx context.Context, lost *bool, backoff *time.Duration) {
	err := b.subscribe(ctx, lost, func() { *backoff = 100 * time.Millisecond })
	b.healthy.Store(false)
	if ctx.Err() != nil {
		return // shutting down
	}
	if !*lost {
		b.logger.WarnContext(ctx, "realtime: bus subscription lost", "error", err)
	}
	*lost = true
}

// subscribe runs one subscription until it fails.
func (b *Bus) subscribe(ctx context.Context, lost *bool, onSubscribed func()) error {
	ps := b.rdb.Subscribe(ctx, b.channel)
	defer func() { _ = ps.Close() }()
	// go-redis does not abort a blocking read when ctx ends: closing does
	stop := context.AfterFunc(ctx, func() { _ = ps.Close() })
	defer stop()
	// wait for the confirmation: only then are later messages guaranteed to arrive
	if _, err := ps.Receive(ctx); err != nil {
		return err
	}
	b.healthy.Store(true)
	onSubscribed()
	if *lost {
		*lost = false
		b.logger.InfoContext(ctx, "realtime: bus subscription restored, asking clients to resync")
		b.hub.Broadcast("resync")
	}
	for {
		m, err := ps.ReceiveTimeout(ctx, 30*time.Second)
		if err != nil {
			var nerr interface{ Timeout() bool }
			if errors.As(err, &nerr) && nerr.Timeout() {
				if err := ps.Ping(ctx); err != nil {
					return err
				}
				continue
			}
			return err
		}
		switch v := m.(type) {
		case *redis.Message:
			b.receive(ctx, v.Payload)
		case *redis.Subscription, *redis.Pong:
		}
	}
}

func (b *Bus) receive(ctx context.Context, payload string) {
	var msg wireMessage
	if err := json.Unmarshal([]byte(payload), &msg); err != nil {
		b.logger.WarnContext(ctx, "realtime: bad bus message", "error", err)
		return
	}
	if msg.Replica == b.replica {
		return // already delivered locally
	}
	if msg.SessionsChanged {
		b.hub.Recheck()
	}
	events := make([]chat.Event, len(msg.Events))
	for i, e := range msg.Events {
		events[i] = chat.Event{Name: e.Name, Data: e.Data, Recipients: e.Recipients, ChannelID: e.ChannelID, TeamID: e.TeamID}
	}
	b.hub.Deliver(events...)
}
