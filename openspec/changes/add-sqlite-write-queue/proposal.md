# Fair writes on SQLite

## Why

SQLite lets one writer in at a time and makes the others poll for the lock. Under a burst of writes a writer can
keep missing it: CI failed twice with `database is locked` after five seconds while four clients posted a thousand
messages. `raise-sqlite-busy-timeout` moved the limit to 30 seconds, which hides the failure but not the cause — the
wait is still a lottery, so one message can take seconds while others pass in milliseconds. SQLite is the default
database and what the all-in-one image runs.

## What Changes

- Inside one server process, write transactions on SQLite run one at a time in the order they asked: a queue in
  front of the database instead of polling inside it.
- A request that is cancelled while waiting leaves the queue.
- Reads do not wait (WAL keeps them concurrent).
- The 30-second busy timeout stays as the backstop for other processes on the same file (the CLI, a migration).
- Nothing changes for MariaDB and PostgreSQL.

## Capabilities

### New Capabilities

None.

### Modified Capabilities

- `database`: write transactions on SQLite are served in order.

## Impact

- `internal/database` only: the SQLite handle's connection pool is wrapped so that beginning a transaction, and a
  write outside one, take a process-wide first-come-first-served gate. No change to callers.
- A test that fails today in the way production would: the worst wait in a burst is bounded.
