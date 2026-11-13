# AGENTS.md — instructions for agents

MeetHub: Go (Cobra + Echo v5 + GORM + Asynq; SQLite by default via pure-Go `modernc.org/sqlite`, also MariaDB/PostgreSQL) at the repository root with an embedded Vue 3 + TypeScript SPA
(`frontend/` → `web/dist`). Layout and conventions follow ad-manager (github.com/jniltinho/ad-manager).

## Rules

1. **Everything in English**: code, identifiers, comments, godoc, API and SPA routes, CLI output, logs, API errors,
   docs, OpenSpec specs and commit messages. Translations exist only in the frontend catalogs
   (`frontend/src/i18n/pt-BR.ts`, `es.ts`); English is the default language.
2. **Idiomatic Go**: context first, wrapped errors (`%w`) and typed/sentinel domain errors, `log/slog`, no global
   mutable state, small consumer-side interfaces, owned goroutines (errgroup), table-driven tests with `-race`.
   `make check` (gofmt, go vet, golangci-lint, govulncheck) must pass.
3. **Docker-only builds**: `make build` writes `dist/meethub` (linux/amd64 only). Never commit `dist/`, `web/dist/*`
   (except `.gitkeep`), binaries, `config.toml`, `.env`, databases or `base/` (private source material).
4. Secrets only through the environment (`.env` outside git). Never write secrets to versioned files, logs or audit.
5. Authorization lives in the server (`internal/org`), never only in the UI.
6. Behavior change → OpenSpec change (`openspec/`), specs with testable scenarios.
7. UI: themed toasts, alerts and dialogs only — never `alert()`/`confirm()`/`prompt()`. Every theme fulfils the
   role-token contract with square corners and WCAG AA contrast.
8. Validate big changes with Codex (`codex exec --sandbox read-only …`).

## Validate

```bash
make check && make test && make test-db && make build
```

## Mattermost and LiveKit

Mattermost (github.com/mattermost/mattermost) is the functional base — do not reinvent its model, API, WebSocket
events or permissions. Port code **only** from its Apache-2.0 parts (`server/public/`, `server/i18n/`,
`server/templates/`, `webapp/`) with a `// Portions adapted from Mattermost (Apache-2.0): <path>` header and a
`NOTICE` entry. `server/channels/`, `server/cmd/` and `server/enterprise/` are AGPL/source-available: reference only,
never copied. Meetings use LiveKit (server SDK Go v2 + JS client), not Mattermost Calls.
