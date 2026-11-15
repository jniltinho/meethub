# Design

## Context

Every SQLite connection uses `_txlock=immediate`, WAL and `busy_timeout=30000`. GORM begins transactions through
its `ConnPool`'s `BeginTx`; writes outside transactions go through `ExecContext`. All application writes that
matter (posts, channel changes, sessions) are transactions.

## Decisions

### D1. A gate in the connection pool, for SQLite only

`database.Open` wraps the `*sql.DB` given to GORM in a type that implements GORM's pool interfaces. `BeginTx` takes
a buffered-channel semaphore of size one (blocked senders are served first in, first out) before `BEGIN IMMEDIATE`
and releases it at `Commit` or `Rollback`; `ExecContext` outside a transaction takes it for the statement.
Waiting selects on the request's context. `QueryContext` and `QueryRowContext` pass through.

*Alternative considered:* one connection for everything (`SetMaxOpenConns(1)`) — it also serializes reads, and a
query on the pool from inside a transaction would deadlock.
*Alternative considered:* a longer busy timeout only — what we have: unfair.

### D2. What the gate does not cover

Other processes (the CLI, a second server on the same file, which is unsupported) still meet SQLite's own lock and
its busy timeout. Read-only transactions also begin immediate today and so pass the gate; acceptable, they are rare.
Inside the process, paths below GORM's pool bypass it: `db.SQL` (the migrations, which run before serving), GORM's
`Connection` and prepared statements outside a transaction (MeetHub uses neither), and writes that return rows
outside a transaction (`INSERT … RETURNING` through `QueryContext`; none in MeetHub). They meet SQLite's busy timeout
as before.

### D3. Bounding the wait, and the end of a transaction (decided while implementing)

`Settings.WriteWait` bounds the wait for the gate; 0 means `DefaultWriteWait`, 60 s (twice the busy timeout, the
most a writer could wait before), so a transaction begun on the pool from inside another fails with
`ErrWriteWait` instead of hanging forever. `dbtest` sets 15 s.

Each transaction runs on a connection of its own (`*sql.Conn`). The gate opens when the transaction has ended: after
Commit or Rollback, or, when its context ends first, after `Conn.Close` returns — which waits for database/sql's own
rollback — so the next writer never meets SQLite's lock still held by the previous one (Codex review).

## Risks / Trade-offs

- [A transaction that never ends holds every writer] → already true with SQLite's lock; request contexts bound it.
- [Code that begins a second transaction from inside one] → it deadlocks today as well (as `database is locked`
  after the timeout); the tests run with a short gate timeout that turns it into a clear error.

## Migration Plan

No schema change. Rollback is removing the wrapper.
