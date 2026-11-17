# Tasks

## 1. Server

- [x] 1.1 Add the chosen status to the presence state and its computation, `away` and `offline` to the route, and
  `manual` to a status; verify the presence tests (chosen away kept under activity, offline while connected,
  online back to automatic, do not disturb clearing the choice and the reverse, two replicas) and the route's test

## 2. Interface

- [x] 2.1 Add the status menu to the sidebar's foot with its strings in pt-BR and es; verify Playwright with two
  browsers (the choice seen by the other without a reload, do not disturb for 30 minutes, Escape) and the
  accessibility and contrast checks

## 3. Documentation and validation

- [x] 3.1 Update `docs/api.md`, `README.md` and `CHANGELOG.md`
- [x] 3.2 Run `make check`, `make test`, `make test-db`, `make test-e2e`, `make build` and CI green
