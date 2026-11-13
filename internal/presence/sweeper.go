package presence

import (
	"context"
	"time"

	"github.com/redis/go-redis/v9"
)

// Run ticks every SweepEvery until ctx ends; the owner runs it in its errgroup.
func (t *Tracker) Run(ctx context.Context) error {
	tick := time.NewTicker(SweepEvery)
	defer tick.Stop()
	for {
		select {
		case <-ctx.Done():
			return nil
		case <-tick.C:
			if err := t.Tick(ctx); err != nil {
				t.logger.DebugContext(ctx, "presence: tick failed", "error", err)
			}
		}
	}
}

// Tick runs one round: after a Redis outage it re-registers this replica's
// live connections, then sweeps when this replica holds the lease.
func (t *Tracker) Tick(ctx context.Context) error {
	t.mu.Lock()
	recovering := t.resync
	t.mu.Unlock()
	if recovering {
		if err := t.rdb.Ping(ctx).Err(); err != nil {
			t.observe(ctx, err)
			return err
		}
		if err := t.Reregister(ctx); err != nil {
			return err
		}
		t.mu.Lock()
		t.resync = false
		// every replica re-registers on its next tick; sweeping before that
		// would flip their users offline and back
		t.grace = t.now().Add(2 * SweepEvery)
		t.mu.Unlock()
		t.logger.InfoContext(ctx, "presence: redis recovered, connections re-registered")
	}
	held, err := t.lease(ctx)
	if err != nil || !held {
		return err
	}
	t.mu.Lock()
	wait := t.now().Before(t.grace)
	t.mu.Unlock()
	if wait {
		return nil
	}
	_, err = t.Sweep(ctx)
	return err
}

// Reregister refreshes every live connection of this replica (after Redis
// recovers, possibly without its data).
func (t *Tracker) Reregister(ctx context.Context) error {
	t.mu.Lock()
	conns := make(map[string]string, len(t.local))
	for c, u := range t.local {
		conns[c] = u
	}
	t.mu.Unlock()
	for c, u := range conns {
		if err := t.Heartbeat(ctx, u, c); err != nil {
			return err
		}
	}
	return nil
}

func (t *Tracker) lease(ctx context.Context) (bool, error) {
	n, err := leaseScript.Run(ctx, t.rdb, []string{t.leaseKey()}, t.millis(), LeaseTTL.Milliseconds(), t.replica).Int()
	t.observe(ctx, err)
	return n == 1, err
}

// Sweep drops connections without a heartbeat for ConnTTL, expires dnd,
// turns idle users away and publishes every resulting change in one batch.
func (t *Tracker) Sweep(ctx context.Context) ([]Change, error) {
	if err := sweepScript.Load(ctx, t.rdb).Err(); err != nil {
		t.observe(ctx, err)
		return nil, err
	}
	var changes []Change
	var cursor uint64
	for {
		ids, next, err := t.rdb.SScan(ctx, t.usersKey(), cursor, "", 200).Result()
		t.observe(ctx, err)
		if err != nil {
			return nil, err
		}
		got, err := t.sweepBatch(ctx, ids)
		if err != nil {
			return nil, err
		}
		changes = append(changes, got...)
		if cursor = next; cursor == 0 {
			break
		}
	}
	t.publish(ctx, changes)
	return changes, nil
}

func (t *Tracker) sweepBatch(ctx context.Context, ids []string) ([]Change, error) {
	if len(ids) == 0 {
		return nil, nil
	}
	now := t.millis()
	pipe := t.rdb.Pipeline()
	cmds := make([]*redis.Cmd, len(ids))
	for i, id := range ids {
		cmds[i] = sweepScript.EvalSha(ctx, pipe, []string{t.connKey(id), t.stateKey(id), t.usersKey()},
			now, ConnTTL.Milliseconds(), IdleAfter.Milliseconds(), id)
	}
	_, err := pipe.Exec(ctx)
	t.observe(ctx, err)
	if err != nil {
		return nil, err
	}
	var changes []Change
	for i, c := range cmds {
		res, err := c.Slice()
		if err != nil {
			return nil, err
		}
		st, changed, err := parseResult(res)
		if err != nil {
			return nil, err
		}
		if changed {
			changes = append(changes, Change{UserID: ids[i], Status: st})
		}
	}
	return changes, nil
}
