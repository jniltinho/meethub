# Design

## Context

Greenfield repository (see proposal.md — Why). The private source specification and mockup live in `base/` (never
committed). The author's ad-manager project is the reference for layout and conventions: Go module at the repository
root (`main.go` → `cmd.Execute()`), Cobra commands in `cmd/`, domain packages in `internal/`, the SPA in `frontend/`
built into `web/dist` and embedded through `web/embed.go`, `config.toml` with environment overrides, and a theme system
of role tokens with one CSS file per theme. Requirements are in the specs of this change.

Constraints given by the author: all code, identifiers, comments, logs, API and CLI text in English; idiomatic Go;
builds only inside Docker; one target, linux/amd64; nothing under `dist/` committed; Asynq for queues and tasks;
frontend translated to pt-BR and es with English as the default.

## Goals / Non-Goals

**Goals:**
- A skeleton every later change (chat, realtime, meetings, files) extends without restructuring.
- One static binary (`CGO_ENABLED=0`) that runs with SQLite + Redis (both in the Compose stack) on a laptop, and with
  PostgreSQL/MariaDB + Redis in production.
- The ad-manager look system carried over, so the chat UI is built on themed primitives from day one.

**Non-Goals:**
- Chat data model, WebSocket hub, LiveKit, uploads (later changes; the package layout reserves their places).
- Multi-tenant organizations, SSO, e-mail delivery.
- Builds for macOS, Windows or arm64; desktop app.

## Decisions

### D0. Mattermost as the base, LiveKit for meetings

The product model is Mattermost's, so MeetHub does not reinvent it:

- **Mirror, don't invent:** entity names and fields (`Team`, `Channel` types `O`/`P`/`D`/`G`, `Post` with `root_id`,
  `ChannelMember.msg_count`/`mention_count`, `Reaction`, `Session`), REST resource shapes, WebSocket event names
  (`posted`, `post_edited`, `typing`, `status_change`, `channel_viewed`…) and the permission/role scheme
  (system → team → channel roles) follow Mattermost so its documented behavior answers design questions. Where the
  source specification (`base/`) names differ, Mattermost's names win for later chat changes; this change only
  introduces users, sessions, organization (≈ Mattermost system scope) and roles.
- **License boundary:** Mattermost's `server/public/` (model, `shared/markdown`, `shared/i18n`, `utils`),
  `server/i18n/`, `server/templates/` and `webapp/` are Apache-2.0: code may be ported (adapted to Echo/GORM/Vue),
  keeping a header `// Portions adapted from Mattermost (Apache-2.0): <path>` and an entry in `NOTICE`. Everything
  else (`server/channels/`, `server/cmd/`, `server/enterprise/`) is AGPL-3.0 or source-available: read as reference
  only, never copied or translated line by line, so MeetHub stays MIT. The Mattermost name and logo are not used in
  the product (trademark).
- **Roles mapping for this change:** Mattermost `system_admin` ≈ `owner`/`admin`, `system_user` ≈ `member`,
  `system_guest` ≈ `guest`; permissions are named Mattermost-style (`manage_system`, `manage_users`, `read_audits`) so
  the later team/channel scheme slots in.
- **Meetings:** LiveKit self-hosted with its Go server SDK and JS client (later change `add-livekit-meetings`), not
  Mattermost Calls.

Alternatives considered: forking Mattermost (AGPL, PostgreSQL only, React — conflicts with the chosen stack) and a
design-only reference (would re-derive logic Mattermost already publishes under Apache-2.0).

### D1. Repository layout (ad-manager style)

```text
main.go                    package main → cmd.Execute()
cmd/                       Cobra: root.go, server.go, migrate.go, user.go, version.go
internal/
  buildinfo/               Version, Commit, BuildTime (set by -ldflags)
  config/                  Config struct, Load (defaults → TOML → MEETHUB_* env), Validate
  database/                Open(driver, dsn) → *gorm.DB, dialect helpers, readiness
  database/migrations/     goose migrations (Go + per-dialect SQL), lock, runner
  identity/                users, password hashing (Argon2id), user service
  auth/                    sessions, login/logout/setup, CSRF, rate limiter
  org/                     organization, memberships, roles, authorization policy
  audit/                   audit events: record, list
  jobs/                    Queue interface on Asynq, task registry, period scheduler
  theme/                   theme registry (id, label, scheme) — source of truth for the API and the SPA test
  httpserver/              Echo setup, middleware, error mapping, routes, SPA handler
  httpserver/handlers/     one file per resource (auth, me, admin_users, audit, themes, health)
web/                       embed.go (//go:embed all:dist), dist/.gitkeep
frontend/                  Vue 3 + TypeScript SPA
deployments/compose/       compose.yml, profiles
Dockerfile, Makefile, config.toml.example, .env.example
```

Package names are short, singular and describe what they provide (`auth`, `org`, `audit`), no `utils`/`common`.
Later changes add `internal/chat`, `internal/realtime`, `internal/meeting`, `internal/storage` beside them.

### D2. Go practices applied project-wide

- **Dependency direction:** `cmd` wires everything (composition root); domain packages do not import `httpserver`;
  handlers depend on small interfaces declared where they are consumed. No package-level mutable state except the
  Cobra command tree and `buildinfo`.
- **Context:** every I/O function takes `context.Context` first; request contexts flow into GORM (`db.WithContext`) and
  into job enqueueing; shutdown cancels a root context.
- **Errors:** wrap with `%w` and context (`fmt.Errorf("create user %s: %w", email, err)`); domain errors are sentinel
  values or typed errors (`ErrEmailTaken`, `*ValidationError`) mapped to HTTP codes in one place
  (`httpserver/errors.go`), never string-matched.
- **Logging:** `log/slog` JSON handler, request ID attribute, no secrets; one logger passed explicitly.
- **Config:** a typed struct with `toml` and `env` tags; validation returns `errors.Join` of every problem.
- **Concurrency:** goroutines are owned (started with a context, stopped through `errgroup`), no fire-and-forget.
- **Tests:** table-driven, `t.Parallel()` where safe, `httptest` for handlers, real databases instead of mocks for the
  storage layer (D4), `-race` always on in `make test`.
- **Tooling:** `gofmt`/`goimports`, `go vet`, `golangci-lint` (errcheck, staticcheck, revive, gosec, bodyclose,
  contextcheck, errorlint), `govulncheck` — all run by `make check` inside Docker.
- **Docs:** every package has a `// Package x …` comment; exported identifiers start their comment with their name.

### D3. HTTP: Echo v5

Same framework and version as ad-manager. Middleware order: request ID → recover → slog request logger → security
headers → body limit → session loader → CSRF (unsafe methods) → route-level role checks. `/api/v1/*` unknown routes
return JSON 404; everything else falls back to the embedded `index.html`. Hashed assets under `/assets/` get
`Cache-Control: public, max-age=31536000, immutable`; `index.html` gets `no-cache`.
Alternative considered: chi/net-http only — rejected to keep parity with ad-manager's handler and middleware style.

### D4. Database: GORM + goose, pure-Go SQLite

- **SQLite is the default database**, through `modernc.org/sqlite` (pure Go, driver name `sqlite`, keeps
  `CGO_ENABLED=0` and the static binary). GORM uses it via `gorm.io/driver/sqlite` with
  `sqlite.Dialector{DriverName: "sqlite", DSN: …}` — that dialector's mattn dependency compiles to a stub without CGO
  and is never used; `glebarez/sqlite` is not used (its driver also registers as `sqlite` and would clash). DSN:
  `file:<data_dir>/meethub.db?_pragma=busy_timeout(5000)&_pragma=journal_mode(WAL)&_pragma=foreign_keys(1)&_txlock=immediate`
  (modernc `_pragma` syntax, not the mattn syntax of the source spec); a test reads the pragmas back on application and
  migration connections. Verified on a spike: static ELF with CGO off, pragmas applied, SQLite 3.54.
- MariaDB through `gorm.io/driver/mysql`, PostgreSQL through `gorm.io/driver/postgres`; all versions pinned in `go.mod`.
- Migrations: `pressly/goose/v3` `Provider` built on the `*sql.DB` from `gormDB.DB()`, dialect mapped
  `sqlite→sqlite3`, `mariadb→mysql`, `postgres→postgres`. One **complete migration set per dialect**
  (`internal/database/migrations/{sqlite,mariadb,postgres}/NNNNN_name.sql`, embedded); a test asserts the three sets
  have the same version numbers and names. `AutoMigrate` is not used: its result differs between dialects and cannot be
  reviewed.
- Lock: `goose.WithSessionLocker` — PostgreSQL `lock.NewPostgresSessionLocker`; MariaDB a custom locker holding
  `GET_LOCK('meethub_migrate', timeout)` on one dedicated `*sql.Conn` until `RELEASE_LOCK`; SQLite an OS file lock on
  `<db>.migrate.lock` (single node). Lock timeout 5 minutes, then exit 1.
- MariaDB DDL is not transactional, so its migrations use one DDL statement per step and `IF [NOT] EXISTS` so a failed
  run can be repeated; PostgreSQL and SQLite migrations run in a transaction.
- E-mail: `email` (as typed) and `email_normalized` (trim → NFC → lower-case) with a unique index; on MariaDB the column
  uses `utf8mb4_bin` so comparison is byte-wise as on the other dialects. Memberships unique on `(org_id, user_id)`
  with foreign keys on all dialects.
- Initialization singleton: `organizations.singleton` column fixed to 1 with a unique index, so a second organization
  insert fails on every dialect.
- IDs: `oklog/ulid/v2` strings (`CHAR(26)`); times UTC (`TIMESTAMP(6)` / `DATETIME(6)` / SQLite text RFC 3339);
  MariaDB tables `utf8mb4` with `utf8mb4_unicode_ci` for display text only.
- Driver errors are translated to domain errors (`ErrDuplicate`, `ErrForeignKey`) per dialect, with tests.
Alternatives considered: `mattn/go-sqlite3` (CGO breaks the static, Docker-only build) and `glebarez/sqlite`
(driver-name clash with `modernc.org/sqlite`) — both rejected.

### D5. Background jobs: Asynq on Redis (required)

Redis is a required service: the Compose stack always runs it, and the server refuses to start without it (after the
connect timeout). `internal/jobs` defines a small `Queue` interface (`Enqueue(ctx, *asynq.Task, ...asynq.Option)
error`) used by callers, and a registry of task types, each declaring `MaxAttempts` and an idempotent handler,
registered on one `*asynq.ServeMux`.

- `asynq.Client` enqueues; `asynq.Server` runs the workers inside the server's errgroup with an explicit
  `RetryDelayFunc` (exponential, base 10 s, cap 10 min, ±20 % jitter) and `ShutdownTimeout` equal to the HTTP one;
  `MaxRetry(MaxAttempts-1)` because Asynq counts retries after the first run. Exhausted tasks stay archived in Redis.
- **Periodic tasks:** a small scheduler of our own (not `asynq.Scheduler`, whose options are fixed per registration)
  computes each UTC period start and enqueues an occurrence with `TaskID("<type>:<period-start>")` and
  `Retention(period)` — this only reduces duplicate enqueues. The **guarantee** comes from the handler: it first
  inserts `(task_type, period_start)` into `job_runs` (unique key, lease `claimed_until` 15 min, `completed_at`); a
  conflict with a completed or unexpired claim exits without work, an expired lease is taken over.
- First task: `session:cleanup` (hourly; deletes sessions expired or revoked more than 7 days ago). Payloads are JSON
  with IDs only. `asynqmon` is not embedded.
- Tests use a real Redis from the Compose test stack (`make test`/`make test-db`), not a mock.
Alternative considered: an in-process fallback without Redis — dropped at the author's request: one code path, durable
tasks everywhere, and Redis is cheap to run in the Compose stack.

### D6. Sessions, CSRF, cookies and rate limiting

- **Setup token:** `MEETHUB_SETUP_TOKEN`, or 32 random bytes generated on a start with an uninitialized database,
  logged once with the setup URL and written to `<data_dir>/setup-token` (0600); compared in constant time; deleted in
  the initialization transaction's commit hook. HTTP setup and `meethub user create --org-name` share one
  `Initialize` service function.
- **Session token:** 32 random bytes, base64url; only its SHA-256 is stored (`sessions.token_hash`). No server-wide
  session secret is needed (tokens are random, not signed), so none is configured. Login always issues a new session;
  password self-change revokes all sessions and issues a new one for the current browser.
- **Expiry and revocation:** idle 7 days (activity written at most once per minute with
  `UPDATE … WHERE id = ? AND revoked_at IS NULL AND expires_at > now`, so a refresh can never revive a revoked
  session), absolute 30 days. Sessions are checked in the database on every request, so revocation is immediate on
  all replicas.
- **Cookie:** `HttpOnly; SameSite=Lax; Path=/`, no `Domain`; with an HTTPS public URL also `Secure` and the name
  `__Host-meethub_session` (else `meethub_session`, development only — production requires HTTPS).
- **CSRF:** synchronizer token — a per-session random token stored with the session, returned by `/me` and login,
  sent by the SPA in `X-CSRF-Token`, compared in constant time. All unsafe methods also require
  `Content-Type: application/json` and an `Origin` equal to `http.public_url`'s origin (missing or `null` → 403);
  login and setup rely on this origin check since they have no session yet.
- **Client address:** the TCP peer, unless it is in `http.trusted_proxies`, then the right-most untrusted
  `X-Forwarded-For` entry; `X-Forwarded-Proto` likewise only from trusted proxies.
- **Passwords:** Argon2id (`m=64 MiB, t=3, p=2`, 16-byte salt, PHC string), 12–256 bytes, hashing bounded by a
  semaphore of `GOMAXPROCS` slots (wait up to 5 s, then 503 `busy`). Unknown e-mails are checked against a fixed dummy
  hash so the work matches.
- **Rate limits:** fixed-window counters of failed logins — per client address 20/15 min, per normalized e-mail
  5/15 min, kept in Redis (`INCR` + `EXPIRE`) and shared by replicas; on Redis errors an in-memory LRU with TTL
  (bounded to 100 000 keys) applies and a warning is logged.

### D7. Authorization

`internal/org` holds the role → permission matrix of the organizations spec (Mattermost-style permission names) and a
`Policy` with `Require(ctx, actor, permission, target)`; handlers call it, and Echo groups add a coarse
`RequirePermission` middleware. Owner changes (role, status, password reset of an owner) run in a transaction that
locks the organization's owner memberships (`SELECT … FOR UPDATE` on PostgreSQL/MariaDB; SQLite `BEGIN IMMEDIATE`)
before counting active owners, so two concurrent changes cannot remove the last owner.

### D8. Frontend stack

Vue 3 + TypeScript (strict), Vite+ (`vp`, as ad-manager), Pinia, Vue Router, Tailwind 4, `lucide-vue-next` icons,
Vitest and Playwright. Structure: `src/api/` (typed fetch client adding `X-CSRF-Token` and mapping error codes),
`src/stores/` (session, preferences, ui), `src/router/` (auth guards), `src/layouts/AppLayout.vue` (rail, sidebar,
content, side panel), `src/views/`, `src/components/ui/` (Button, Input, Alert, Toast stack, Dialog host…),
`src/themes/`, `src/i18n/` (`en` source, `pt-BR.ts`, `es.ts`).
i18n: a small typed `t()` like ad-manager's, keyed by the English string; a test parses all `t('…')` calls and
fails on keys missing in either catalog. API error codes have their own keys (`error.invalid_credentials`).
Alternative considered: `vue-i18n` — rejected for parity with ad-manager and a smaller bundle; can be swapped later
because all text goes through `t()`.

### D9. Theme system ported from ad-manager

- `src/style.css` holds the **role contract** (`@theme` tokens with default values) and the primitives (`.btn`,
  `.inp`, `.card`, `.grid-t`, `.tag`, toasts, alerts, dialogs), with a global `border-radius: 0`.
- Themes are **one CSS file per family**, as in ad-manager: its 15 files (e.g. `breeze.css` holds `breeze` and
  `breeze-dark`, `classics.css` holds `classic-blue` and `classic-gray`) are copied and adapted to the contract, plus
  `meethub.css` with `meethub-dark` (palette fixed in the ui-themes spec) and `meethub-light` — 16 files, 22 ids.
- Chat-specific roles (rail, active channel row, message hover, mention, unread badge, presence, meeting card) are added
  to the contract with defaults **derived** from base roles (`color-mix`), so a theme overrides them only when needed.
- `internal/theme` is the registry (id, label, scheme). `GET /api/v1/themes` serves it; a lockstep test compares
  registry ids with the `[data-theme='…']` selectors found in `src/themes/*.css` and with the picker list.
- Contract and contrast tests run **in the browser** (Playwright): for each theme, read the computed value of every
  required token, then measure the declared foreground/background matrix on the composited colors (so gradients,
  translucency and `color-mix` are covered).
- **Precedence and no flash:** the Go SPA handler renders `index.html` with `data-theme="<org default>"`.
  `public/theme-boot.js` (external, so the CSP needs no `unsafe-inline`) runs first: if `localStorage['meethub.theme']`
  is set it applies it; otherwise, when the org default is `meethub-dark` and the browser prefers light, it applies
  `meethub-light`. After `/me` the user's preference wins and is stored in `localStorage`; logout removes it.
- Visual checks: a `/__gallery` route compiled only in dev/test builds (a test asserts it is absent from the
  production bundle) renders every toast, alert and dialog kind; Playwright compares it per theme with committed
  baselines (`toHaveScreenshot`, `maxDiffPixelRatio: 0.01`).

### D10. Toasts, alerts, dialogs

Ported from ad-manager's `toasts.js`, `dialogs.js`, `Toasts.vue`, `PanelDialogs.vue`, rewritten in TypeScript:
`useToast()` (`success|info|warning|error`, timers paused on hover/focus, max 5, `aria-live`), `<UiAlert>` component,
and `askConfirm`, `showAlert`, `confirmTyped` returning promises rendered by one `<DialogHost>` using `<dialog>` with
focus trap and focus return. Requests are queued FIFO (one dialog at a time); `cancelDialogs()` (from ad-manager)
runs on logout and session expiry and resolves every pending request as cancelled. Toast timers store the remaining
time when paused. A lint rule (`no-restricted-globals`/`no-restricted-properties` for
`alert`/`confirm`/`prompt`) enforces the no-native-UI rule; a `native.ts` port replaces validation bubbles.

### D11. Docker-only build and run

- **Dockerfile** (BuildKit, `--platform=linux/amd64`, base images pinned by tag and digest):
  `frontend` (node LTS, `npm ci && npm run build` → `web/dist`); `build` (golang 1.27, `CGO_ENABLED=0 GOOS=linux
  GOARCH=amd64 go build -trimpath -ldflags "-s -w -X …/buildinfo.Version=${VERSION}…"`, `VERSION` build-arg defaulting
  to `dev`); `export` (`FROM scratch`, `COPY /meethub /meethub` — the binary at the stage root so
  `--output dist` writes `dist/meethub`); `runtime` (`distroless/static-debian12:nonroot`, `/data` created in the build
  stage and copied with `--chown=65532:65532`, `ENTRYPOINT ["/meethub"]`, `CMD ["server"]`,
  `HEALTHCHECK CMD ["/meethub","healthcheck"]`).
- **healthcheck** turns a wildcard listen address (`:8080`, `0.0.0.0`) into `127.0.0.1`, ignores proxy variables,
  times out after 3 s and checks liveness only (`/api/v1/health`, no database).
- **Embed:** `web/embed.go` uses `//go:embed all:dist` so the committed `web/dist/.gitkeep` satisfies the pattern on a
  fresh clone; the SPA handler serves "frontend not built" when `dist/index.html` is absent.
- **.dockerignore:** `base/`, `.env*`, `config.toml`, `*.db*`, `dist/`, `node_modules/`, `.git/` (version comes from
  the `VERSION` build-arg that the Makefile fills from `git describe`).
- **Tools image** (`docker/tools.Dockerfile`: Go on Debian with gcc for `-race`, golangci-lint, govulncheck, Node,
  Playwright browsers) runs `check`, `test`, `test-db`, `test-e2e`; release builds stay static with CGO off.
- **Makefile:** `build`, `image`, `init` (copy `config.toml.example`/`.env.example` if missing), `up DB=…`,
  `down`, `logs`, `check`, `test`, `test-db`, `test-e2e`, `dev`, `clean`. `DB` selects the Compose override file
  (`compose.mariadb.yml`, `compose.postgres.yml`) that starts the database with a health check and sets
  `MEETHUB_DATABASE_*` on the `meethub` service. Redis is part of the base `compose.yml` (named volume, AOF on,
  `redis-cli ping` health check) and `meethub` depends on it being healthy. Unknown `DB` fails. Test targets
  use `trap`-style cleanup (`down -v` always).
- Go module and npm caches are BuildKit cache mounts and Docker volumes so containerized builds stay fast.

## Risks / Trade-offs

- [Pure-Go SQLite (modernc) is slower than CGO SQLite] → SQLite is for small single-node installs; PostgreSQL/MariaDB are
  documented for production; benchmarks in the later chat change.
- [Redis becomes a hard dependency] → it ships in the Compose stack with persistence and a health check; readiness
  reports it; rate limits degrade to per-replica memory limits during an outage instead of failing logins.
- [Porting 20 themes to a chat UI is large] → chat roles derive from base roles, so most themes need no new values;
  the contrast test pinpoints the few that do.
- [Three dialects double the migration effort] → per-dialect SQL only where needed, and `make test-db` in CI on every
  push catches drift early.
- [Echo v5 is newer than v4 and has fewer examples] → same version already proven in ad-manager.
- [Docker-only builds slow local iteration] → BuildKit cache mounts and `make dev` (hot reload) for frontend work; Go
  tests run in a long-lived tools container.

## Migration Plan

New project: no data to migrate. Rollout is `make build`/`make up`; rollback is running the previous image, with
`meethub migrate down` for schema steps (every migration has a down step tested in `make test-db`).

## Open Questions

- Organization default theme editable by admins in this change or the next? (Assumed: setting stored now, admin UI in
  a later change; no spec here depends on editing it.)
