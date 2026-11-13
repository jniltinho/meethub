// Package presence tracks whether people are online, away, offline or in do
// not disturb, shared by every replica through Redis.
//
// Per user Redis keeps a sorted set of live connections ("replica:conn")
// scored by their last heartbeat and a hash with the last activity, the
// optional dnd (0 = until changed, otherwise until that time) and the last
// published status. Every transition runs in one Lua script that also
// recomputes the status, so concurrent connects and disconnects on different
// replicas never lose an update and each change is reported exactly once.
// A sweeper on the replica holding a lease drops connections whose heartbeat
// is older than ConnTTL and publishes the resulting changes.
package presence

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"slices"
	"strconv"
	"sync"
	"time"

	"github.com/redis/go-redis/v9"
)

// Statuses.
const (
	Online  = "online"
	Away    = "away"
	Offline = "offline"
	DND     = "dnd"
)

// Timing (the spec's bounds: changes are published within ConnTTL + SweepEvery = 75 s).
const (
	ConnTTL      = 60 * time.Second // a connection is live while its heartbeat is younger
	IdleAfter    = 5 * time.Minute  // online → away without activity
	SweepEvery   = 15 * time.Second
	LeaseTTL     = 20 * time.Second
	keyPrefix    = "meethub:presence:"
	maxBatchSize = 200
)

// ErrTooMany is returned for a status lookup of more than 200 users.
var ErrTooMany = errors.New("at most 200 users per status lookup")

// Change is a published status transition.
type Change struct {
	UserID string
	Status string
}

// Status is a user's status as seen by a lookup; Stale is set when Redis is
// unavailable and the value is this replica's last known status.
type Status struct {
	UserID   string `json:"user_id"`
	Status   string `json:"status"`
	DNDUntil int64  `json:"dnd_end_time,omitempty"` // ms; 0 = until changed
	Stale    bool   `json:"stale,omitempty"`
	LastSeen int64  `json:"last_activity_at,omitempty"`
}

// Notifier receives the status changes to publish (status_change events).
type Notifier interface {
	StatusChanged(ctx context.Context, changes []Change)
}

// Tracker is one replica's view of presence.
type Tracker struct {
	rdb      *redis.Client
	prefix   string
	replica  string
	notify   Notifier
	logger   *slog.Logger
	now      func() time.Time
	mu       sync.Mutex
	local    map[string]string // live connections of this replica: conn id → user id
	known    map[string]Status // last known statuses (served stale during outages)
	degraded bool              // the last Redis operation failed
	resync   bool              // an operation failed since the last re-registration
	grace    time.Time         // no sweeping before this (other replicas re-registering)
}

// Options configures a Tracker.
type Options struct {
	Replica  string // unique per process
	Notifier Notifier
	Logger   *slog.Logger
	Now      func() time.Time // test clock; time.Now by default
	Prefix   string           // Redis key prefix; "meethub:presence:" by default
}

// New builds a Tracker.
func New(rdb *redis.Client, o Options) *Tracker {
	if o.Logger == nil {
		o.Logger = slog.New(slog.DiscardHandler)
	}
	if o.Now == nil {
		o.Now = time.Now
	}
	if o.Prefix == "" {
		o.Prefix = keyPrefix
	}
	return &Tracker{
		rdb: rdb, prefix: o.Prefix, replica: o.Replica, notify: o.Notifier, logger: o.Logger, now: o.Now,
		local: map[string]string{}, known: map[string]Status{},
	}
}

func (t *Tracker) connKey(userID string) string  { return t.prefix + "{" + userID + "}:conns" }
func (t *Tracker) stateKey(userID string) string { return t.prefix + "{" + userID + "}:state" }

// usersKey holds the users with connections or a non-offline status (swept).
func (t *Tracker) usersKey() string { return t.prefix + "users" }
func (t *Tracker) leaseKey() string { return t.prefix + "lease" }

// compute is shared by every script: it recomputes the status of KEYS[1]
// (conns) / KEYS[2] (state) at time now and stores it; returns status, changed.
const compute = `
local function compute(now, ttl, idle)
  local dnd = redis.call('HGET', KEYS[2], 'dnd')
  if dnd and dnd ~= '0' and tonumber(dnd) <= now then
    redis.call('HDEL', KEYS[2], 'dnd')
    dnd = nil
  end
  local live = redis.call('ZCOUNT', KEYS[1], now - ttl, '+inf')
  local status
  if dnd then
    status = 'dnd'
  elseif live == 0 then
    status = 'offline'
  else
    local act = tonumber(redis.call('HGET', KEYS[2], 'activity') or '0')
    if now - act < idle then status = 'online' else status = 'away' end
  end
  local prev = redis.call('HGET', KEYS[2], 'status') or 'offline'
  redis.call('HSET', KEYS[2], 'status', status)
  if status == 'offline' and redis.call('ZCARD', KEYS[1]) == 0 then
    redis.call('SREM', KEYS[3], ARGV[4])
  else
    redis.call('SADD', KEYS[3], ARGV[4])
  end
  local changed = 0
  if prev ~= status then changed = 1 end
  return {status, changed}
end
`

// ARGV: now, ttl, idle, user id, then per script.
var (
	connectScript = redis.NewScript(compute + `
local now = tonumber(ARGV[1])
redis.call('ZADD', KEYS[1], now, ARGV[5])
local act = tonumber(redis.call('HGET', KEYS[2], 'activity') or '0')
if ARGV[6] == '1' and now > act then redis.call('HSET', KEYS[2], 'activity', now) end
return compute(now, tonumber(ARGV[2]), tonumber(ARGV[3]))`)

	disconnectScript = redis.NewScript(compute + `
redis.call('ZREM', KEYS[1], ARGV[5])
return compute(tonumber(ARGV[1]), tonumber(ARGV[2]), tonumber(ARGV[3]))`)

	activityScript = redis.NewScript(compute + `
local now = tonumber(ARGV[1])
local act = tonumber(redis.call('HGET', KEYS[2], 'activity') or '0')
if now > act then redis.call('HSET', KEYS[2], 'activity', now) end
return compute(now, tonumber(ARGV[2]), tonumber(ARGV[3]))`)

	dndScript = redis.NewScript(compute + `
if ARGV[5] == '' then redis.call('HDEL', KEYS[2], 'dnd') else redis.call('HSET', KEYS[2], 'dnd', ARGV[5]) end
return compute(tonumber(ARGV[1]), tonumber(ARGV[2]), tonumber(ARGV[3]))`)

	sweepScript = redis.NewScript(compute + `
local now = tonumber(ARGV[1])
redis.call('ZREMRANGEBYSCORE', KEYS[1], '-inf', '(' .. (now - tonumber(ARGV[2])))
return compute(now, tonumber(ARGV[2]), tonumber(ARGV[3]))`)

	// leaseScript takes or renews the sweeper lease on the tracker clock:
	// KEYS[1] lease hash; ARGV now, ttl, replica. Returns 1 when held.
	leaseScript = redis.NewScript(`
local now = tonumber(ARGV[1])
local holder = redis.call('HGET', KEYS[1], 'holder')
local exp = tonumber(redis.call('HGET', KEYS[1], 'expires') or '0')
if holder and holder ~= ARGV[3] and exp > now then return 0 end
redis.call('HSET', KEYS[1], 'holder', ARGV[3], 'expires', now + tonumber(ARGV[2]))
redis.call('PEXPIRE', KEYS[1], tonumber(ARGV[2]) * 10)
return 1`)
)

func (t *Tracker) millis() int64 { return t.now().UnixMilli() }

// run executes a transition script for userID and publishes a change.
func (t *Tracker) run(ctx context.Context, s *redis.Script, userID string, extra ...any) (string, error) {
	st, changed, err := t.eval(ctx, t.rdb, s, userID, extra...)
	if err != nil {
		return "", err
	}
	if changed {
		t.publish(ctx, []Change{{UserID: userID, Status: st}})
	}
	return st, nil
}

func (t *Tracker) eval(ctx context.Context, c redis.Scripter, s *redis.Script, userID string, extra ...any) (string, bool, error) {
	args := append([]any{t.millis(), ConnTTL.Milliseconds(), IdleAfter.Milliseconds(), userID}, extra...)
	res, err := s.Run(ctx, c, []string{t.connKey(userID), t.stateKey(userID), t.usersKey()}, args...).Slice()
	t.observe(ctx, err)
	if err != nil {
		return "", false, fmt.Errorf("presence: %w", err)
	}
	return parseResult(res)
}

func parseResult(res []any) (string, bool, error) {
	if len(res) != 2 {
		return "", false, fmt.Errorf("presence: unexpected reply %v", res)
	}
	st, _ := res[0].(string)
	n, _ := res[1].(int64)
	return st, n == 1, nil
}

// observe tracks Redis health; recovery triggers re-registration on the next Run tick.
func (t *Tracker) observe(ctx context.Context, err error) {
	t.mu.Lock()
	defer t.mu.Unlock()
	if err != nil && !t.degraded {
		t.logger.WarnContext(ctx, "presence: redis unavailable, serving stale statuses", "error", err)
	}
	t.degraded = err != nil
	if err != nil {
		t.resync = true
	}
}

func (t *Tracker) publish(ctx context.Context, changes []Change) {
	t.mu.Lock()
	for _, c := range changes {
		k := t.known[c.UserID]
		k.UserID, k.Status = c.UserID, c.Status
		t.known[c.UserID] = k
	}
	t.mu.Unlock()
	if t.notify != nil && len(changes) > 0 {
		t.notify.StatusChanged(ctx, changes)
	}
}

func (t *Tracker) member(connID string) string { return t.replica + ":" + connID }

// Connect registers a live connection of userID (connecting counts as activity).
// The connection stays registered locally even when Redis is down, so it is
// re-registered on recovery.
func (t *Tracker) Connect(ctx context.Context, userID, connID string) error {
	t.mu.Lock()
	t.local[connID] = userID
	t.mu.Unlock()
	_, err := t.run(ctx, connectScript, userID, t.member(connID), "1")
	return err
}

// Heartbeat refreshes a live connection (a ping); it re-adds it if missing.
func (t *Tracker) Heartbeat(ctx context.Context, userID, connID string) error {
	_, err := t.run(ctx, connectScript, userID, t.member(connID), "0")
	return err
}

// Disconnect removes a connection that closed cleanly.
func (t *Tracker) Disconnect(ctx context.Context, userID, connID string) error {
	t.mu.Lock()
	delete(t.local, connID)
	t.mu.Unlock()
	_, err := t.run(ctx, disconnectScript, userID, t.member(connID))
	return err
}

// Activity records user input or a post (online again when away).
func (t *Tracker) Activity(ctx context.Context, userID string) error {
	_, err := t.run(ctx, activityScript, userID)
	return err
}

// SetDND sets do not disturb until the given time (zero: until changed).
func (t *Tracker) SetDND(ctx context.Context, userID string, until time.Time) error {
	v := "0"
	if !until.IsZero() {
		if !until.After(t.now()) {
			return errors.New("dnd end time is in the past")
		}
		v = strconv.FormatInt(until.UnixMilli(), 10)
	}
	_, err := t.run(ctx, dndScript, userID, v)
	return err
}

// ClearDND returns the status to following activity.
func (t *Tracker) ClearDND(ctx context.Context, userID string) error {
	_, err := t.run(ctx, dndScript, userID, "")
	return err
}

// Get returns the statuses of userIDs (callers filter them by visibility
// first). During a Redis outage it returns the last known statuses, stale.
func (t *Tracker) Get(ctx context.Context, userIDs []string) ([]Status, error) {
	if len(userIDs) > maxBatchSize {
		return nil, ErrTooMany
	}
	pipe := t.rdb.Pipeline()
	states := make([]*redis.SliceCmd, len(userIDs))
	for i, id := range userIDs {
		states[i] = pipe.HMGet(ctx, t.stateKey(id), "status", "dnd", "activity")
	}
	_, err := pipe.Exec(ctx)
	t.observe(ctx, err)
	if err != nil {
		return t.stale(userIDs), nil
	}
	out := make([]Status, len(userIDs))
	t.mu.Lock()
	defer t.mu.Unlock()
	for i, id := range userIDs {
		v := states[i].Val()
		st := Status{UserID: id, Status: Offline}
		if s, ok := v[0].(string); ok && s != "" {
			st.Status = s
		}
		if d, ok := v[1].(string); ok && st.Status == DND {
			st.DNDUntil, _ = strconv.ParseInt(d, 10, 64)
		}
		if a, ok := v[2].(string); ok {
			st.LastSeen, _ = strconv.ParseInt(a, 10, 64)
		}
		out[i] = st
		t.known[id] = st
	}
	return out, nil
}

func (t *Tracker) stale(userIDs []string) []Status {
	t.mu.Lock()
	defer t.mu.Unlock()
	out := make([]Status, len(userIDs))
	for i, id := range userIDs {
		st, ok := t.known[id]
		if !ok {
			st = Status{UserID: id, Status: Offline}
		}
		st.Stale = true
		out[i] = st
	}
	return out
}

// Online reports which of userIDs are online now (for @here).
func (t *Tracker) Online(ctx context.Context, userIDs []string) (map[string]bool, error) {
	out := map[string]bool{}
	for chunk := range slices.Chunk(userIDs, maxBatchSize) {
		sts, err := t.Get(ctx, chunk)
		if err != nil {
			return nil, err
		}
		for _, s := range sts {
			if s.Status == Online {
				out[s.UserID] = true
			}
		}
	}
	return out, nil
}
