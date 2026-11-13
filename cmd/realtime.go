package cmd

import (
	"context"
	"log/slog"
	"time"

	"github.com/oklog/ulid/v2"
	"github.com/redis/go-redis/v9"

	"github.com/jniltinho/meethub/internal/buildinfo"
	"github.com/jniltinho/meethub/internal/chat"
	"github.com/jniltinho/meethub/internal/database"
	"github.com/jniltinho/meethub/internal/identity"
	"github.com/jniltinho/meethub/internal/presence"
	"github.com/jniltinho/meethub/internal/ratelimit"
	"github.com/jniltinho/meethub/internal/realtime"
	"github.com/jniltinho/meethub/internal/session"
)

// Message and typing limits (specs: messages, presence).
const (
	postsPerWindow = 30
	postsWindow    = 10 * time.Second
	typingEvery    = 3 * time.Second
)

// realtimeStack is the server's live part: presence, the WebSocket hub and the bus.
type realtimeStack struct {
	tracker *presence.Tracker
	hub     *realtime.Hub
	bus     *realtime.Bus
}

// newRealtime wires presence, hub and bus to the chat service and the API.
func newRealtime(db *database.DB, rdb *redis.Client, store session.Store, chatSvc *chat.Service, logger *slog.Logger) *realtimeStack {
	replica := ulid.Make().String()
	rt := &realtimeStack{}
	rt.tracker = presence.New(rdb, presence.Options{Replica: replica, Logger: logger, Notifier: statusNotifier{chat: chatSvc, rt: rt, logger: logger}})
	rt.hub = realtime.NewHub(realtime.Options{
		Version:  buildinfo.Version,
		Presence: rt.tracker,
		Handler:  actions{chat: chatSvc, tracker: rt.tracker},
		Sessions: sessionCheck{db: db, store: store},
		Logger:   logger,
	})
	rt.bus = realtime.NewBus(rdb, rt.hub, replica, logger)

	chatSvc.Publisher = rt.bus
	chatSvc.Presence = rt.tracker
	chatSvc.Limiter = ratelimit.NewWindow(rdb, logger, "meethub:posts:", postsPerWindow, postsWindow)
	chatSvc.TypingLimiter = ratelimit.NewWindow(rdb, logger, "meethub:typing:", 1, typingEvery)
	return rt
}

// statusNotifier publishes status changes to the users allowed to see them.
type statusNotifier struct {
	chat   *chat.Service
	rt     *realtimeStack
	logger *slog.Logger
}

func (n statusNotifier) StatusChanged(ctx context.Context, changes []presence.Change) {
	ids := make([]string, len(changes))
	for i, c := range changes {
		ids[i] = c.UserID
	}
	watchers, err := n.chat.StatusWatchers(ctx, ids)
	if err != nil {
		n.logger.WarnContext(ctx, "presence: status audience", "error", err)
		return
	}
	events := make([]chat.Event, 0, len(changes))
	for _, c := range changes {
		events = append(events, chat.Event{
			Name: "status_change", Data: map[string]any{"user_id": c.UserID, "status": c.Status}, Recipients: watchers[c.UserID],
		})
	}
	n.rt.bus.Publish(ctx, events...)
}

// actions answers the WebSocket actions that need the chat.
type actions struct {
	chat    *chat.Service
	tracker *presence.Tracker
}

func (a actions) Typing(ctx context.Context, sub chat.Subject, channelID, parentID string) error {
	return a.chat.Typing(ctx, sub, channelID, parentID)
}

// Statuses answers get_statuses: visible users only, unknown ones omitted.
func (a actions) Statuses(ctx context.Context, sub chat.Subject, userIDs []string) (any, error) {
	if len(userIDs) > 200 {
		return nil, chat.ErrInvalidInput
	}
	visible, err := a.chat.VisibleStatuses(ctx, sub.UserID, userIDs)
	if err != nil {
		return nil, err
	}
	sts, err := a.tracker.Get(ctx, visible)
	if err != nil {
		return nil, err
	}
	out := make(map[string]string, len(sts))
	for _, s := range sts {
		out[s.UserID] = s.Status
	}
	return out, nil
}

// sessionCheck validates the hub's sessions against the database.
type sessionCheck struct {
	db    *database.DB
	store session.Store
}

func (s sessionCheck) Live(ctx context.Context, ids []string, now time.Time) (map[string]bool, error) {
	return s.store.Live(ctx, s.db.Gorm, ids, identity.StatusActive, now)
}

func (s sessionCheck) Touch(ctx context.Context, id string, now time.Time) error {
	return s.store.TouchID(ctx, s.db.Gorm, id, now)
}

// run starts the loops; they stop when loopCtx ends (after the hub shut down).
func (rt *realtimeStack) loops() []func(ctx context.Context) error {
	return []func(ctx context.Context) error{rt.hub.RunRevalidation, rt.bus.Run, rt.tracker.Run}
}
