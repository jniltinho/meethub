# Tasks

## 1. Busy timeout

- [x] 1.1 Set `busy_timeout(30000)` in the SQLite connection pragmas and in the pragma test; verify
  `go test ./internal/database` and `TestThousandMessagesInOrder` repeated with `GOMAXPROCS=2 -race`
- [ ] 1.2 Verify CI green on `main` for the commit (the unit-test step that failed with `SQLITE_BUSY`)

## Workflow follow-up

- Archive with `openspec archive raise-sqlite-busy-timeout` after CI is green.
