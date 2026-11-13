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
- One static binary (`CGO_ENABLED=0`) that runs with SQLite and no Redis on a laptop, and with PostgreSQL/MariaDB and
  Redis in production.
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
  jobs/                    Queue interface, Asynq and in-process implementations, task types, scheduler
  theme/                   theme registry (id, label, scheme) — source of truth for the API and the SPA test
  httpserver/              Echo setup, middleware, error mapping, routes, SPA handler
  httpserver/handlers/     one file per resource (auth, me, admin_users, audit, themes, health)
web/                       embed.go (//go:embed dist), dist/.gitkeep
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

- Drivers: `github.com/glebarez/sqlite` (pure Go, keeps `CGO_ENABLED=0` and the static binary), `gorm.io/driver/mysql`
  for MariaDB, `gorm.io/driver/postgres`.
- Migrations: `pressly/goose/v3` Provider with embedded migrations. Shared SQL is written once; where DDL differs
  (types, partial indexes, collation) a migration has per-dialect files selected by the runner. `AutoMigrate` is not
  used, because its result differs between dialects and cannot be reviewed.
- Migration lock: PostgreSQL `pg_advisory_lock`, MariaDB `GET_LOCK`, SQLite a single writer (single-node only).
- Case-insensitive unique e-mail: stored as `email_normalized` (lower-cased) with a unique index, portable on all
  three dialects.
- IDs: `oklog/ulid/v2` strings (`CHAR(26)`); times as UTC `TIMESTAMP`/`DATETIME(6)`; MariaDB `utf8mb4` +
  `utf8mb4_unicode_ci`.
- SQLite DSN defaults: WAL, `busy_timeout=5000`, `foreign_keys=on`, one writer connection.
Alternative considered: `mattn/go-sqlite3` — rejected (CGO breaks the static, Docker-simple build).

### D5. Background jobs: Asynq with an in-process fallback

`internal/jobs` defines:

```go
type Queue interface {
    Enqueue(ctx context.Context, task *asynq.Task, opts ...asynq.Option) error
}
```

Task types and handlers are registered once on an `*asynq.ServeMux`. With `redis.enabled = true` the server starts an
`asynq.Server` (workers), `asynq.Client` (enqueue) and `asynq.Scheduler` (periodic tasks). Without Redis, an
in-process queue (bounded channel + worker goroutines owned by the server's errgroup) calls the same `ServeMux` and
implements retry/backoff with the same `MaxRetry` option, and a ticker-driven scheduler enqueues periodic tasks.
Periodic tasks use `asynq.TaskID("<type>:<period-start>")` so concurrent schedulers on several replicas enqueue each
period once (Asynq rejects duplicate IDs). First task: `session:cleanup` (hourly). Payloads are JSON with IDs only.
The Asynq web UI/`asynqmon` is not embedded; the CLI may expose `meethub jobs` later.
Alternative considered: Redis always required — rejected; a laptop/SQLite install must run with zero services.

### D6. Sessions, CSRF and rate limiting

- Session token: 32 random bytes, base64url, sent in cookie `meethub_session`; only its SHA-256 is stored
  (`sessions.token_hash`), so a database leak does not yield usable sessions. Idle timeout (default 7 days, sliding,
  written at most once per minute) and absolute lifetime (default 30 days).
- CSRF: per-session random token stored with the session, exposed in a readable cookie `meethub_csrf`; the SPA sends
  it in `X-CSRF-Token`; compared in constant time. Same-origin `Origin`/`Referer` check as a second layer.
- Passwords: Argon2id (`m=64 MiB, t=3, p=2`, 16-byte salt), PHC string format so parameters can be raised later.
- Login rate limit: token buckets per client IP and per normalized e-mail; in memory without Redis, Redis-backed
  (sliding window) when enabled so replicas share counts.

### D7. Authorization

`org.Policy` exposes `Can(actor Membership, action Action, target …) error`. Handlers call it; role rules (owner-only
owner grants, last-owner guard) live in `internal/org`, never only in the SPA. Echo route groups add a coarse
`RequireRole(admin)` middleware; the policy does the fine checks.

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

- `src/style.css` holds the **role contract** (`@theme` tokens with the default values) and the primitives (`.btn`,
  `.inp`, `.card`, `.grid-t`, `.tag`, toasts, alerts, dialogs), with a global `border-radius: 0`.
- Each theme is `src/themes/<id>.css` overriding role tokens under `:root[data-theme='<id>']`; the 20 ad-manager theme
  files are copied and adapted to the contract, and `meethub.css` defines `meethub-dark` (from the mockup: deep navy
  `#0f1424`-range panels, indigo primary `#5b5ff0`-range, square buttons) and `meethub-light`.
- Chat-specific roles (rail, channel list active row, message hover, mention, unread badge, presence colors, meeting
  card) are added to the contract with defaults **derived** from base roles (`color-mix`), so a theme only overrides
  them when the derived value is wrong or fails contrast.
- `internal/theme` is the registry (id, label, scheme). `GET /api/v1/themes` serves it, and a frontend test checks that
  the registry, the CSS files and the picker list agree (as ad-manager's lockstep test).
- No flash: `public/theme-boot.js` (an external file, so the CSP needs no `unsafe-inline`) reads the last theme from
  `localStorage` and sets `data-theme` before the app bundle loads; the server preference wins after `/me`.
- Contrast: a Vitest test resolves each theme's tokens and checks WCAG AA pairs; Playwright renders a
  `/__gallery` route (dev builds only) with every toast, alert and dialog kind per theme for screenshots.

### D10. Toasts, alerts, dialogs

Ported from ad-manager's `toasts.js`, `dialogs.js`, `Toasts.vue`, `PanelDialogs.vue`, rewritten in TypeScript:
`useToast()` (`success|info|warning|error`, timers paused on hover/focus, max 5, `aria-live`), `<UiAlert>` component,
and `askConfirm`, `showAlert`, `confirmTyped` returning promises rendered by one `<DialogHost>` using `<dialog>` with
focus trap and focus return. A lint rule (`no-restricted-globals`/`no-restricted-properties` for
`alert`/`confirm`/`prompt`) enforces the no-native-UI rule; a `native.ts` port replaces validation bubbles.

### D11. Docker-only build and run

- **Dockerfile** (multi-stage, `--platform=linux/amd64`): `frontend` (node:lts-alpine, `npm ci && npm run build` →
  `web/dist`), `build` (golang:1.27-alpine, `CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -trimpath -ldflags "-s -w
  -X …buildinfo…"`), `export` (`FROM scratch`, only the binary — used with `docker build --target export --output
  dist/`), `runtime` (`gcr.io/distroless/static-debian12:nonroot`, `/data` volume, port 8080, `HEALTHCHECK` via
  `meethub healthcheck` subcommand since distroless has no curl).
- **Makefile**: `build` (export binary to `dist/meethub`), `image`, `up`/`down`/`logs` (Compose with `PROFILE=`),
  `check`, `test` (Go `-race` + Vitest in a tools container), `test-db` (Compose test profile with MariaDB and
  PostgreSQL, runs DB tests against all three, then `down -v`), `dev` (Vite dev server container proxying `/api` to the
  backend container), `clean`. Version from `git describe`.
- **Compose** (`deployments/compose/compose.yml`): `meethub` service (config.toml mounted read-only, `.env` via
  `env_file`, `meethub-data` volume); profiles `redis`, `mariadb`, `postgres`, `dev`, `test`.
- Go module and npm caches are Docker volumes/BuildKit cache mounts so containerized builds stay fast.
- `.gitignore` covers `dist/`, `web/dist/*` (keeps `.gitkeep`), `base/`, `config.toml`, `.env*` (keeps
  `.env.example`), `*.db*`, `node_modules/`, agent-local folders.

Note: `make build` therefore needs `web/dist` to exist for `go:embed`; the placeholder `.gitkeep` keeps `go vet` working
on a fresh clone, and the SPA handler serves a "frontend not built" page when `index.html` is missing.

## Risks / Trade-offs

- [Pure-Go SQLite is slower than CGO SQLite] → SQLite is for small single-node installs; PostgreSQL/MariaDB are
  documented for production; benchmarks in the later chat change.
- [In-process jobs are lost on restart] → logged at startup and documented; only idempotent housekeeping tasks in
  this change; enabling Redis makes them durable.
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
  a later change; the spec only requires the default to exist.)
