# Tasks

## 1. Repository, Docker toolchain and test infrastructure

- [x] 1.1 Finish repository hygiene: `.dockerignore` (`base/`, `.env*`, `config.toml`, `*.db*`, `dist/`,
  `node_modules/`, `.git/`) beside the existing `.gitignore`, `LICENSE`, `NOTICE`, `README.md`, `AGENTS.md`; verify
  `git ls-files` lists nothing under `base/`
- [x] 1.2 Initialize the Go module `github.com/jniltinho/meethub` (Go 1.27, dependencies pinned) with `main.go`,
  `internal/buildinfo`, `web/embed.go` (`//go:embed all:dist`) and `web/dist/.gitkeep`; verify `go vet ./...` and
  `go build` pass on a fresh clone in the build container
- [x] 1.3 Scaffold a minimal `frontend/` (Vue 3, TypeScript strict, Vite+, one placeholder view) that builds into
  `web/dist`; verify `npm run build` in a container writes `web/dist/index.html`
- [x] 1.4 Write the multi-stage `Dockerfile` (`frontend`, `build`, `export`, `runtime`; pinned images; `VERSION` arg)
  and the `Makefile` (`build`, `image`, `init`, `clean`); verify `make build` and the README's plain `docker build
  --target export --output dist .` both produce `dist/meethub`, a check in Docker reports a static ELF x86-64, and no
  file from `base/`, `.env` or `config.toml` is in any image layer
- [x] 1.5 Add the tools image (`docker/tools.Dockerfile`: Go + gcc for `-race`, golangci-lint, govulncheck, Node,
  Playwright) with `.golangci.yml` and the `check`/`test` targets; verify `make check` and `make test` pass on the
  skeleton
- [x] 1.6 Add the Compose test stack (MariaDB, PostgreSQL, Redis with health checks) and `make test-db` with
  always-run cleanup; verify an empty test passes against the three databases and Redis and that containers are
  removed after a forced failure

## 2. CLI and configuration

- [x] 2.1 Implement the Cobra root (`--config`, `--json`, exit codes) and `version`; verify `meethub --help`,
  unknown-command exit 1 and `version` without a config file with `cmd` tests
- [x] 2.2 Implement `internal/config` (defaults → TOML → `MEETHUB_*` env; secrets only from env or `_FILE`; structured
  database settings; `http.public_url`, `http.trusted_proxies`; `errors.Join` validation; redaction) and
  `config.toml.example`/`.env.example`; verify table-driven tests for every configuration scenario
- [x] 2.3 Implement `healthcheck` (wildcard → loopback, no proxy, 3 s timeout); verify exit 0 against an `httptest`
  server, exit 1 with nothing listening

## 3. Database and migrations

- [x] 3.1 Implement `internal/database` (open per dialect, SQLite default via `modernc.org/sqlite` with `_pragma` DSN, connect retry, readiness, driver error
  translation); verify the pragma read-back test on app and migration connections and duplicate/FK error tests on all
  dialects (`make test-db`)
- [x] 3.2 Add the goose provider (per-dialect migration sets, session lockers, 5-minute lock timeout) and the first
  migrations (`organizations` with singleton, `users`, `memberships`, `sessions`, `user_preferences`,
  `audit_events`, `job_runs`); verify the same-versions test, idempotent `up`, `down`, concurrent runners, lock timeout
  and the re-runnable MariaDB scenario on all dialects
- [x] 3.3 Add the `migrate` command (`up`, `down`, `status`); verify the fresh-install status scenario

## 4. HTTP runtime

- [x] 4.1 Implement `internal/httpserver` on a pinned Echo v5 (handler, error handler, slog request log, body limit,
  security headers with the exact CSP, client-address extraction with trusted proxies); verify tests for 404/405 JSON,
  400, 413, headers and forged `X-Forwarded-For`
- [x] 4.2 Add `/api/v1/health`, `/api/v1/ready` and the embedded SPA handler (index fallback rendering
  `data-theme`, immutable assets, "frontend not built"); verify readiness scenarios and deep-link reload
- [x] 4.3 Implement the `server` command (migrate unless `--no-migrate`, errgroup for HTTP and workers, graceful
  shutdown); verify graceful shutdown and invalid-config tests

## 5. Background jobs (Asynq)

- [x] 5.1 Implement `internal/jobs` (registry with `MaxAttempts`, Asynq client/server with `RetryDelayFunc`, Redis
  connect timeout, shutdown); verify attempts = 3 exactly, backoff growth, restart durability, Redis-down enqueue error
  and shutdown tests against the test Redis
- [x] 5.2 Implement the UTC period scheduler and `job_runs` claim/lease; verify two staggered schedulers,
  completion-before-second-enqueue and expired-lease takeover on all dialects
- [x] 5.3 Add `session:cleanup`; verify the 7-day purge scenario

## 6. Identity, organizations, audit and themes registry

- [ ] 6.1 Implement `internal/identity` (normalized e-mail, Argon2id PHC, 12–256-byte policy, hashing semaphore,
  dummy hash); verify hashing, weak/oversized password, saturation and duplicate-email (incl. `josé` vs `jose`) tests
- [ ] 6.2 Implement `internal/audit` (record in the caller's transaction, `(created_at, id)` cursor, filters); verify
  no-secret payloads, stable pages and rollback on audit failure
- [ ] 6.3 Implement `internal/org` (permission matrix, policy, owner-change locking, last-owner guard); verify
  table-driven tests for every role × permission and the concurrent two-owner scenario on all dialects
- [ ] 6.4 Implement `internal/theme` registry and `GET /api/v1/themes`; verify it lists 22 themes with
  `meethub-dark` as default

## 7. Authentication and APIs

- [ ] 7.1 Implement the setup token, `Initialize` service (singleton transaction, audit `system.initialized`) and
  `GET /api/v1/bootstrap`, `POST /api/v1/auth/setup`; verify missing/wrong token, concurrent setup, setup after
  initialization and partial-failure rollback
- [ ] 7.2 Implement sessions (hashed tokens, cookie attributes, idle/absolute expiry, conditional refresh,
  revocation), CSRF (synchronizer token + JSON + Origin) and the rate limiters (Redis, bounded memory fallback); verify
  every authentication scenario, including refresh-vs-revoke and cross-origin login
- [ ] 7.3 Add `POST /auth/login`, `POST /auth/logout`, `GET/PATCH /me`, `POST /me/password`,
  `GET/POST /admin/users`, `PATCH /admin/users/:id`, `GET /admin/audit`; verify `httptest` tests for uniform 401,
  403 matrix, unknown fields, disable-ends-sessions, invalid theme/language and audit events written
- [ ] 7.4 Implement the `user` command (`create` with `--org-name` initialization, `set-password` revoking sessions,
  `list --json`, `--password-stdin`); verify CLI initialization, first-user rules and CLI reset scenarios
- [ ] 7.5 Document the API in `docs/api.md` and the security model in `docs/security.md`; verify a test that every
  registered route appears in `docs/api.md`

## 8. Frontend foundation

- [ ] 8.1 Extend the scaffold (Pinia, Vue Router, Tailwind 4, lucide, Vitest, Playwright, ESLint banning
  `alert`/`confirm`/`prompt`); verify `make check` runs the frontend lint and type check
- [ ] 8.2 Implement `src/i18n` (typed `t()`, `en` default, `pt-BR` and `es` catalogs, error-code keys, Intl helpers)
  and the completeness test; verify it fails on a missing `pt-BR` or `es` key and the fallback scenario
- [ ] 8.3 Implement `src/api` (CSRF header, error-code mapping, 401 handling) and the session/preferences stores;
  verify Vitest tests with a mocked fetch

## 9. Themes

- [ ] 9.1 Port ad-manager's `style.css` contract and primitives, add chat roles derived with `color-mix`, global
  `border-radius: 0`; verify the contract token list test
- [ ] 9.2 Add `meethub.css` (spec palette) and port the 15 ad-manager theme files; verify the lockstep test
  (registry ids = CSS selectors = picker)
- [ ] 9.3 Add the browser contract and contrast tests (computed tokens, composited foreground/background matrix) and
  fix failing pairs; verify they pass for all 22 themes
- [ ] 9.4 Add `public/theme-boot.js`, the theme picker with previews and server persistence; verify Playwright tests
  for fresh browser, light preference, shared-browser logout, reload without flash and theme following the user
- [ ] 9.5 Write `docs/themes.md` (contract, adding a theme, chat roles); verify its token table matches the contract
  test's list

## 10. Toasts, alerts and dialogs

- [ ] 10.1 Implement `useToast` + `<ToastStack>` (kinds, remaining-time pause, max 5, aria-live); verify Vitest tests
  for every toast scenario
- [ ] 10.2 Implement `<UiAlert>`; verify component tests for kinds, action and dismiss
- [ ] 10.3 Implement `askConfirm`, `showAlert`, `confirmTyped`, `<DialogHost>` (FIFO queue, `cancelDialogs` on logout
  and expiry, focus trap/return, roles) and `native.ts`; verify overlapping-dialog, session-end and keyboard tests
- [ ] 10.4 Add the dev/test-only `/__gallery` route and the Playwright screenshot comparison per theme; verify
  baselines for 22 themes and a test that the production bundle has no gallery

## 11. Web shell

- [ ] 11.1 Implement setup (token field) and login pages (themed alerts, language selector, safe return route);
  verify Playwright setup → login → deep link and open-redirect scenarios
- [ ] 11.2 Implement `AppLayout` (rail, sidebar, content, side panel; permission-based Administration; drawer and
  bottom bar under 768 px) and placeholder views; verify Playwright at 1440 px and 360 px
- [ ] 11.3 Implement Settings (profile, password, appearance, language) and Administration (users with confirm and
  typed-confirm actions, audit log); verify Playwright admin flows and member 403 handling
- [ ] 11.4 Accessibility pass; verify an axe check reports no serious violations on login, layout, settings and
  gallery

## 12. Compose stack, CI and acceptance

- [ ] 12.1 Write the Compose files (`compose.yml` with `meethub` + `redis`, `compose.mariadb.yml`,
  `compose.postgres.yml`), `make up DB=…`/`down`/`logs`/`dev`, and `docs/deployment.md`; verify default (Redis healthy
  before MeetHub), `DB=mariadb`, `DB=postgres`, unknown `DB`, image health with an empty volume and liveness during a database
  outage
- [ ] 12.2 Add `make test-e2e` running the Playwright suite (flows, gallery, axe) against the Compose stack for each
  of SQLite, MariaDB and PostgreSQL; verify the source's acceptance gate — setup creates the organization, login,
  RBAC — passes on all three
- [ ] 12.3 Add GitHub Actions running `make check`, `make test`, `make test-db`, `make test-e2e` and `make build`;
  verify a green run on the repository

## Workflow follow-up

- Validate the implementation with Codex (`codex exec --sandbox read-only`), as done for this proposal.
- Archive the change with `openspec archive add-meethub-foundation` after review.
- Next changes: `add-chat-realtime` (Mattermost model: teams, channels, posts, WebSocket events),
  `add-livekit-meetings`, `add-file-uploads`, `add-invitations-and-recovery`.
