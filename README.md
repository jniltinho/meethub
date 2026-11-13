# MeetHub

Self-hosted team chat — teams, channels, direct messages, threads and files — with meetings built in through
[LiveKit](https://livekit.io). Inspired by the Mattermost workflow, with its own flat, square-cornered look and the
theme system from [ad-manager](https://github.com/jniltinho/ad-manager).

> **Status:** foundation implemented — server, CLI, database layer (SQLite/MariaDB/PostgreSQL), background jobs,
> authentication, roles, audit log and the themed web shell (22 themes, English/Portuguese/Spanish). Chat, realtime and
> LiveKit meetings come next. Specification: [`openspec/changes/add-meethub-foundation`](openspec/changes/add-meethub-foundation/proposal.md).

## Stack

- **Backend:** Go (Cobra CLI, Echo v5 REST + WebSocket, GORM), one static binary with the SPA embedded.
- **Database:** SQLite by default (pure-Go `modernc.org/sqlite`), MariaDB or PostgreSQL — one, chosen by configuration.
- **Jobs:** [Asynq](https://github.com/hibiken/asynq) on Redis (required; the Compose stack runs it).
- **Frontend:** Vue 3 + TypeScript, Vite+, Pinia, Vue Router, Tailwind 4; English, Portuguese (Brazil) and Spanish.
- **Meetings:** LiveKit self-hosted (later change).

## Build and run

Everything builds and runs in Docker; the host needs only Docker and `make`. The only target is linux/amd64.

```bash
make build   # dist/meethub (static linux/amd64 binary; dist/ is never committed)
make up      # Compose stack: config.toml + .env (copy from config.toml.example / .env.example)
```

Without `make`, Docker alone produces the same binary:

```bash
docker build --platform linux/amd64 --target export --output dist .
```

## Documentation

- [Deployment](docs/deployment.md) — Compose stack, configuration, production checklist
- [API](docs/api.md) — `/api/v1` endpoints, errors, roles and permissions
- [Security](docs/security.md) — secrets, setup, sessions, CSRF, rate limits, headers
- [Themes](docs/themes.md) — the role contract and how to add a theme

## License

MIT
