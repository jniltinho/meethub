# Raise the SQLite busy timeout

## Why

On SQLite a write transaction that finds the database locked waits through SQLite's busy handler, which polls for the
lock instead of queueing for it. Under a burst of writes one writer can keep missing the lock for several seconds, and
with `busy_timeout=5000` it then fails with `database is locked` although it only had to wait. This showed in CI as
`TestThousandMessagesInOrder` failing twice on loaded runners (four writers, a thousand posts, about nine seconds),
and it is the same failure a busy single-node install would show as a message that could not be sent.

## What Changes

- Every SQLite connection uses `busy_timeout=30000` instead of `5000`: a writer waits up to 30 seconds for the write
  lock before giving up.
- Nothing changes for MariaDB and PostgreSQL.

## Capabilities

### New Capabilities

None.

### Modified Capabilities

- `database`: the SQLite default connection reports `busy_timeout=30000`.

## Impact

- `internal/database/database.go` (the connection pragmas) and its test.
- A write on a saturated SQLite database now waits longer before it fails; the wait is still unfair (polling). Fair
  queueing of write transactions inside the process is the next step if posting latency on SQLite under sustained
  write load ever matters; it is not part of this change.
