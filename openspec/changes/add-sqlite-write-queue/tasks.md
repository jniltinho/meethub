# Tasks

## 1. Gate

- [ ] 1.1 Wrap the SQLite connection pool with the first-come-first-served write gate (transactions and writes
  outside them, context-aware waiting, reads untouched); verify unit tests of order, cancellation and a read during
  a write, with `-race`
- [ ] 1.2 Add a burst test that records each writer's longest wait and fails above a bound, and run
  `TestThousandMessagesInOrder` 20 times with `GOMAXPROCS=2 -race`; verify both pass and the bound fails without
  the gate
- [ ] 1.3 Run `make check`, `make test`, `make test-db`, `make test-load`, `make build` and CI green

## Workflow follow-up

- Archive with `openspec archive add-sqlite-write-queue` after review.
