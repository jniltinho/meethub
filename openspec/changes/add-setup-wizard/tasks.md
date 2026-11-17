# Tasks

Depends on `edit-ai-and-recording-settings`. Every Go task runs its tests with `-race` on SQLite, MariaDB and
PostgreSQL (`make test-db`).

## 1. Diagnosis

- [x] 1.1 Implement the diagnosis service and its two routes (setup token before setup, `manage_system` after);
  verify table-driven tests for each check and level, the token and rate limit, and that no answer carries a
  secret or an environment value

## 2. Wizard

- [x] 2.1 Rewrite `SetupView.vue` as the wizard up to the account step (stepper, hints, validation rules in
  `setupRules.ts` with unit tests, the token command per kind of install); verify `01-setup` and the
  accessibility and contrast checks in every theme, at phone width
- [x] 2.2 Add the optional steps, the review and Finish (apply with the applier, lines to copy without one, the
  AI key and connection test, reopening at `/setup` for owners); pt-BR and es strings; verify a new e2e spec
  against the fake AI server with and without the applier

## 3. Documentation and validation

- [x] 3.1 Describe the wizard step by step in `docs/install-linux.md`, `docs/install-binary.md`,
  `docs/deployment.md` and `README.md`, the routes in `docs/api.md`, and `CHANGELOG.md`
- [ ] 3.2 Run `make check`, `make test`, `make test-db`, `make test-e2e`, `make build`, `make test-package`, a
  Codex review with its findings fixed, and CI green
