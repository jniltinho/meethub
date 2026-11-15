# Tasks

## 1. Gate

- [x] 1.1 Wrap the SQLite connection pool with the first-come-first-served write gate (transactions and writes
  outside them, context-aware waiting, reads untouched); verify unit tests of order, cancellation and a read during
  a write, with `-race`
- [x] 1.2 Add a burst test that records each writer's longest wait and fails above a bound, and run
  `TestThousandMessagesInOrder` 20 times with `GOMAXPROCS=2 -race`; verify both pass and the bound fails without
  the gate
  - `TestBurstOfWritersIsFair` (8 writers × 40 transactions of ~2 ms): worst wait 18 ms with the gate, bound 68 ms;
    with the gate disabled the worst wait was 633–735 ms in three runs and the arrival-order test served
    `[5 2 3 4 1 0]`. `TestThousandMessagesInOrder` passed 20 runs with `GOMAXPROCS=2 -race`.
  - Writes that return rows (`INSERT … RETURNING` outside a transaction) pass through `QueryContext` and are not
    gated: none happens in the test suite (checked by making such a query panic while the suite ran); see the
    `shortcut:` note in `internal/database/writegate.go`.
- [x] 1.3 Run `make check`, `make test`, `make test-db`, `make test-load`, `make build` and CI green
  - All five pass locally; CI green on the commit of v0.2.0.

## Workflow follow-up

- Archive with `openspec archive add-sqlite-write-queue` after review.
