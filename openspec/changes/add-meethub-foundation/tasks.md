# Tasks

## 1. Repository and Docker toolchain

- [ ] 1.1 Create `.gitignore` (`dist/`, `web/dist/*` except `.gitkeep`, `base/`, `config.toml`, `.env*` except
  `.env.example`, `*.db*`, `node_modules/`, agent-local folders), `README.md`, `LICENSE` (MIT) and `AGENTS.md` with the
  project rules (English everywhere, Docker-only builds, OpenSpec for behavior changes, Codex review for big changes);
  verify `git status` never lists `base/`
- [ ] 1.2 Initialize the Go module `github.com/jniltinho/meethub` (Go 1.27) with `main.go`, `internal/buildinfo`,
  `web/embed.go` and `web/dist/.gitkeep`; verify `go vet ./...` passes in the build container
- [ ] 1.3 Write the multi-stage `Dockerfile` (`frontend`, `build`, `export`, `runtime` targets, linux/amd64, BuildKit
  cache mounts) and the `Makefile` (`build`, `image`, `up`, `down`, `logs`, `check`, `test`, `test-db`, `dev`,
  `clean`); verify `make build` on a host without Go/Node writes a static ELF x86-64 `dist/meethub` and that
  `git status --porcelain` shows nothing under `dist/`
- [ ] 1.4 Add the tools image used by `make check`/`make test` (Go, golangci-lint with the D2 linters, govulncheck,
  Node) and `.golangci.yml`; verify `make check` passes on the skeleton

## 2. CLI and configuration

- [ ] 2.1 Implement the Cobra root (`--config`, `--json`, exit codes) and `version`; verify `meethub --help`,
  unknown-command exit 1 and `version` without a config file with `cmd` tests
- [ ] 2.2 Implement `internal/config` (defaults → TOML → `MEETHUB_*` env, `errors.Join` validation, secret redaction)
  and `config.toml.example`/`.env.example`; verify table-driven tests for precedence, every validation scenario and
  that the example file parses
- [ ] 2.3 Implement `healthcheck`; verify exit 0 against an `httptest` server and exit 1 with nothing listening

## 3. Database and migrations

- [ ] 3.1 Implement `internal/database` (Open for sqlite/mariadb/postgres, SQLite WAL/busy-timeout/foreign-keys,
  connect retry with timeout, readiness check); verify unit tests and the unreachable-database scenario
- [ ] 3.2 Add the goose migration runner with per-dialect lock and the first migrations (`organizations`, `users`,
  `memberships`, `sessions`, `audit_events`, `user_preferences`) with up and down steps; verify idempotent `migrate up`,
  `down`, `status`
- [ ] 3.3 Add the `migrate` command and the Compose `test` profile; verify `make test-db` runs migration and
  repository tests (unique normalized e-mail, emoji round trip, concurrent migrate) on SQLite, MariaDB and PostgreSQL

## 4. Background jobs (Asynq)

- [ ] 4.1 Implement `internal/jobs` (Queue interface, ServeMux registry, Asynq client/server/scheduler, in-process
  queue with retry/backoff and ticker scheduler, period-based TaskIDs); verify tests for retry exhaustion, in-process
  execution and, against Redis in `make test-db`, restart durability and one run with two schedulers
- [ ] 4.2 Add the `session:cleanup` periodic task and the startup log line stating in-process tasks are not durable;
  verify the expired-session purge test

## 5. HTTP runtime

- [ ] 5.1 Implement `internal/httpserver` (Echo v5, middleware chain from D3, JSON error mapping, body limit, slog
  request log without bodies/cookies, security headers, HSTS on HTTPS); verify handler tests for 404 JSON, 400, 413
  and headers
- [ ] 5.2 Add `/api/v1/health` and `/api/v1/ready` (database, pending migrations, Redis) and the embedded SPA handler
  (index fallback, immutable asset caching, "frontend not built" page); verify readiness scenarios and deep-link reload
- [ ] 5.3 Implement the `server` command (migrate unless `--no-migrate`, start HTTP and job workers in an errgroup,
  graceful shutdown on SIGINT/SIGTERM); verify the graceful shutdown and invalid-config tests

## 6. Identity, authentication and organizations

- [ ] 6.1 Implement `internal/identity` (users, normalized e-mail, Argon2id PHC hashing, password policy); verify
  hashing round trip, weak-password and duplicate-email tests
- [ ] 6.2 Implement `internal/org` (organization, memberships, roles, `Policy` with owner-only grants and last-owner
  guard); verify table-driven policy tests for every role and action
- [ ] 6.3 Implement `internal/auth` sessions (hashed tokens, idle/absolute expiry, revocation, revoke-others on
  password change), CSRF (double submit + Origin check) and login rate limiting (memory and Redis); verify tests for
  each authentication scenario
- [ ] 6.4 Add endpoints: `POST /auth/setup`, `POST /auth/login`, `POST /auth/logout`, `GET /me`,
  `PATCH /me` (name, preferences: theme, language), `POST /me/password`, `GET/POST/PATCH /admin/users`; verify
  `httptest` tests covering setup 409, uniform 401, 403 for members, disable-ends-sessions and invalid theme/language
- [ ] 6.5 Implement the `user` command (`create`, `set-password`, `list --json`, terminal password prompt); verify
  creating the first owner from the CLI and logging in with it
- [ ] 6.6 Document the API in `docs/api.md` and security choices in `docs/security.md`; verify every route in the
  router appears in `docs/api.md` (test)

## 7. Audit log

- [ ] 7.1 Implement `internal/audit` (record inside the caller's transaction, cursor list with filters) and wire the
  events listed in the spec; verify tests for role change, failed login, no secrets in payloads and rollback on audit
  failure
- [ ] 7.2 Add `GET /api/v1/admin/audit`; verify filter, ordering, pagination and 403 for members

## 8. Frontend foundation

- [ ] 8.1 Scaffold `frontend/` (Vue 3, TypeScript strict, Vite+, Pinia, Vue Router, Tailwind 4, lucide, Vitest,
  Playwright, ESLint with `alert`/`confirm`/`prompt` banned); verify `npm run check` and `npm run build` in Docker
  write `web/dist`
- [ ] 8.2 Implement `src/i18n` (typed `t()`, `en` default, `pt-BR` and `es` catalogs, error-code keys, Intl date and
  number helpers) and the completeness test; verify the test fails on a missing key and the fallback scenario
- [ ] 8.3 Implement `src/api` (fetch client with CSRF header, error-code mapping, 401 handling) and the session and
  preferences stores; verify Vitest tests with a mocked fetch

## 9. Themes

- [ ] 9.1 Port ad-manager's `style.css` role contract and primitives, add the chat roles derived with `color-mix`,
  enforce `border-radius: 0`; verify a test that lists contract tokens
- [ ] 9.2 Create `meethub.css` (`meethub-dark` from the mockup, `meethub-light`) and port the 20 ad-manager theme files
  to the contract; add `internal/theme` registry and `GET /api/v1/themes`; verify the lockstep test (registry = CSS
  files = picker) and the missing-token test
- [ ] 9.3 Add the WCAG AA contrast test over every theme and fix failing pairs; verify it passes for all 22 themes
- [ ] 9.4 Add `public/theme-boot.js` (no-flash, CSP-safe), the theme picker with previews, server persistence and the
  login-page `prefers-color-scheme` rule; verify Playwright tests for reload without flash and theme following the
  user to a new browser context
- [ ] 9.5 Write `docs/themes.md` (contract, adding a theme, chat roles); verify its token table matches the contract
  test's list

## 10. Toasts, alerts and dialogs

- [ ] 10.1 Implement `useToast` + `<ToastStack>` (kinds, timers, hover/focus pause, max 5, aria-live); verify Vitest
  tests for every toast scenario
- [ ] 10.2 Implement `<UiAlert>` (kinds, title, action, dismiss); verify component tests
- [ ] 10.3 Implement `askConfirm`, `showAlert`, `confirmTyped` + `<DialogHost>` (focus trap, Escape cancels, focus
  return, roles and labels) and `native.ts` validation replacement; verify Vitest and Playwright keyboard tests
- [ ] 10.4 Add the dev-only `/__gallery` route and the Playwright visual run rendering every kind in every theme;
  verify screenshots are produced for all 22 themes and contrast checks pass

## 11. Web shell

- [ ] 11.1 Implement setup and login pages (themed alerts for errors, language selector, redirect back after login);
  verify Playwright setup → login → deep link flow
- [ ] 11.2 Implement `AppLayout` (64 px rail, 264 px sidebar, content, 320 px side panel; role-based Administration
  entry; drawer + bottom bar under 768 px) and placeholder views; verify Playwright at 1440 px and 360 px (no
  horizontal scroll)
- [ ] 11.3 Implement Settings (profile, password, appearance, language) and Administration (users with
  confirm/typed-confirm actions, audit log table); verify Playwright admin flows and member 403 handling
- [ ] 11.4 Accessibility pass (focus on route change, accessible names, visible focus in every theme); verify an axe
  check in Playwright reports no serious violations on login, layout, settings and gallery

## 12. Compose stack, CI and integration

- [ ] 12.1 Write `deployments/compose/compose.yml` (default SQLite; `redis`, `mariadb`, `postgres`, `dev`, `test`
  profiles; config.toml mount, `.env`, `meethub-data` volume) and `docs/deployment.md`; verify `make up`, `make up
  PROFILE=postgres` and the image health check
- [ ] 12.2 Add GitHub Actions running `make check`, `make test`, `make test-db` and `make build` on pushes and pull
  requests; verify a green run on the repository
- [ ] 12.3 End-to-end check: fresh `make up`, setup, login, theme and language change, admin creates and disables a
  user, audit shows the events; verify by a Playwright run against the Compose stack

## Workflow follow-up

- Validate the change with Codex (`codex exec --sandbox read-only`) after implementation, as for the proposal.
- Archive the change with `openspec archive add-meethub-foundation` after review.
- Next changes: `add-chat-realtime`, `add-livekit-meetings`, `add-file-uploads`, `add-invitations-and-recovery`.
