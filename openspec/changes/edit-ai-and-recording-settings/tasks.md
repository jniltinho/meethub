# Tasks

Every Go task runs its tests with `-race` on SQLite, MariaDB and PostgreSQL (`make test-db`) unless it says
otherwise.

## 1. Keys in the database

- [x] 1.1 Move the seal to a shared package and add the migration with the two sealed columns; verify the seal's
  tests still pass, a key sealed for one column does not open as the other, and a wrong `MEETHUB_SECRET_KEY`
  reads as not set
- [x] 1.2 Resolve the AI keys at request time (environment first, then the stored key) in `internal/summaries`
  and the connection test, with the change announced on the bus; verify with the fake AI server: a key entered
  while running is used by the next request on two replicas, removal stops running summaries and reads `off`
- [x] 1.3 Add `GET` and `PUT /api/v1/admin/summaries/keys` (`manage_system`, write-only, an empty value removes,
  `409` when locked by the environment, `503 secret_key_missing`) and the `ai.key_changed` audit event; verify that no response, log
  line or audit entry carries a key

## 2. Allowlist, re-authentication, retention

- [x] 2.1 Add the keys of design D2 to `config.AdminKeys` with their bounds, empty-allowed text and the reasons
  removed from the read-only list; verify table-driven tests for every bound, the applier refusing out-of-bounds
  values, and `recordings.dir` refused under an unfinished recording
- [x] 2.2 Mark the sensitive keys and require `password` (and `code`) on their changes and on the keys routes,
  under the login rate limit, with `settings.reauth_failed`; verify the `Sensitive settings` scenarios, an owner
  without a password, and that a non-sensitive change asks for nothing
- [x] 2.3 Hold the expiry job while a change is pending and report the count of recordings a retention would
  delete; verify the `A new retention waits` scenarios, including a rollback

## 3. Interface

- [x] 3.1 Settings page: the new rows with hints, the AI service form (keys set or not set, replace, remove,
  locked by a variable, no secret key), the password and code in the confirmation dialog, the retention count;
  pt-BR and es strings; verify Playwright (15-settings, 14-summaries) with the accessibility and contrast checks

## 4. Documentation

- [x] 4.1 Rewrite "Meeting summaries" in `docs/deployment.md` (a walk-through for OpenAI and for a self-hosted
  server, which models work, every setting with bounds and where it is changed), update `docs/admin-settings.md`,
  `docs/security.md`, `docs/api.md`, `docs/install-linux.md`, `config.toml.example`, `README.md`, `CHANGELOG.md`
  and rule 4 of `AGENTS.md`; verify every documented command and value against the running stack

## 5. Validation

- [ ] 5.1 Run `make check`, `make test`, `make test-db`, `make test-e2e`, `make build`,
  `make test-settings-systemd`, a Codex review with its findings fixed, and CI green
- [x] 5.2 On the local stack against OpenAI: enter the key in the page with no variable set, change the summary
  model, summarize a recording; record what was checked in the change before archiving
