# Proposal

## Why

MeetHub (self-hosted team chat with integrated LiveKit meetings, inspired by the Mattermost workflow) starts from an
empty repository. Chat, realtime and meetings all depend on the same base: a single Go binary, selectable database,
sessions and roles, background jobs and a themed SPA shell. Building that foundation first, in the same layout as the
author's ad-manager project, lets every later feature land as a small, testable change.

## What Changes

- Mattermost is the functional base: the data model, REST and WebSocket contracts, roles/permissions and flows mirror
  Mattermost's, and code is ported only from its Apache-2.0 parts (`server/public`, `webapp/`) with attribution;
  meetings use LiveKit.
- New Go module at the repository root: `main.go` + Cobra command tree in `cmd/` (`server`, `migrate`, `user`,
  `version`) + `internal/` packages; the Vue SPA is built from `frontend/` into `web/dist` and embedded with `go:embed`.
- Configuration from `config.toml` with `MEETHUB_*` environment overrides; secrets only through the environment.
- One persistent database chosen by configuration: SQLite (default, single node), MariaDB or PostgreSQL, with
  versioned migrations that run on every dialect.
- Background jobs and queues with Asynq on Redis, a required service (run by the Compose stack).
- Authentication: first-run administrator setup, login/logout with revocable HTTP-only cookie sessions, CSRF
  protection, login rate limiting and Argon2id password hashing.
- Organizations and roles: one organization per install in this change, memberships with `owner`, `admin`, `member`
  and `guest` roles enforced by the server, and an audit trail of security events.
- HTTP runtime: Echo v5 REST under `/api/v1`, `/api/v1/health` and `/api/v1/ready`, security headers, SPA fallback
  routing.
- Web shell (Vue 3 + TypeScript, Pinia, Vue Router, Tailwind 4): login page and the three-column application layout
  from the mockup (navigation rail, context sidebar, content, collapsible side panel), responsive drawer on mobile,
  with placeholder views for Conversations, Files, Meetings and Activity.
- Theme system ported from ad-manager: role-token contract, one CSS file per theme, the ad-manager theme catalog plus
  a new **MeetHub** dark/light pair (the default, from the mockup), square corners, per-user persistence and a theme
  picker.
- Themed feedback UI: toasts, inline alerts and dialogs (alert, confirm, typed confirm) replacing the browser's native
  `alert()`/`confirm()` and validation bubbles, styled by every theme.
- English everywhere by default (code, API, CLI, logs, docs); the web app adds Portuguese (Brazil) and Spanish
  translations.
- Everything builds and runs in Docker: `make build` writes a static linux/amd64 `dist/meethub` (the only target), a
  container image and a Compose stack (MeetHub + Redis, SQLite by default; MariaDB and PostgreSQL options). Nothing under `dist/`
  is committed.

Out of scope (follow-up changes): channels, DMs, messages and the WebSocket hub (`add-chat-realtime`), LiveKit
meetings (`add-livekit-meetings`), file uploads, invitations by e-mail and password recovery, notifications, search,
SSO, multi-organization tenancy, Kubernetes and LiveKit Egress.

## Capabilities

### New Capabilities

- `server-cli`: the `meethub` binary's command tree (serve, migrate, create users, print version) and its exit codes.
- `configuration`: how settings are loaded (file, environment overrides, defaults, validation) and how secrets are
  supplied.
- `database`: selection of SQLite, MariaDB or PostgreSQL and versioned schema migrations that behave the same on each.
- `background-jobs`: enqueueing, retrying and scheduling background work on Redis.
- `http-runtime`: the HTTP server, API versioning, health/readiness probes, security headers and serving the embedded
  SPA.
- `authentication`: first-run setup, login, logout, sessions, CSRF and brute-force protection.
- `organizations`: the organization, memberships and role-based authorization rules.
- `audit-log`: recording and listing security-relevant events.
- `web-shell`: the SPA's login page, application layout, navigation and responsive behavior.
- `ui-themes`: the theme catalog, the theme contract every theme fulfils, the picker and per-user persistence.
- `ui-feedback`: themed toasts, inline alerts and dialogs used instead of native browser UI.
- `language`: English by default everywhere, with pt-BR and es translations for the web app.
- `build-and-deploy`: Docker-only builds of the linux/amd64 binary into `dist/`, the container image and the Compose
  stack.

### Modified Capabilities

None (new project).

## Impact

- License: MIT, with a `NOTICE` crediting code ported from Mattermost's Apache-2.0 parts; no AGPL code is copied.
- New repository `github.com/jniltinho/meethub` (public); `base/` (private specification and mockup) stays out of git.
- Go dependencies: Cobra, Echo v5, GORM with sqlite/mysql/postgres drivers, go-toml v2, Asynq, golang.org/x/crypto
  (Argon2id), ULID.
- Frontend dependencies: Vue 3, TypeScript, Vite+, Pinia, Vue Router, Tailwind 4, lucide-vue-next, Vitest and
  Playwright.
- Required runtime dependency: Redis (Asynq, shared rate limits and, later, multi-replica WebSocket fan-out).
- Developer tooling: Docker and make only on the host; Makefile (`build`, `image`, `up`, `down`, `check`, `test`,
  `test-db`, `dev`) and GitHub Actions CI running the same targets, with a SQLite/MariaDB/PostgreSQL test matrix.
