# Deployment

Everything builds and runs in Docker; the host needs Docker (and `make`). The only target is **linux/amd64**.

## Quick start (Compose)

```bash
git clone https://github.com/jniltinho/meethub && cd meethub
make up                 # creates config.toml and .env from the examples, builds the image, starts MeetHub + Redis
make logs               # the first start logs the setup URL and the one-time setup token
```

Open `http://localhost:8080`, enter the setup token, the organization name and the owner account. Or create the
owner from the CLI instead of the page:

```bash
docker compose --env-file .env -f deployments/compose/compose.yml exec -T meethub \
  /meethub user create --org-name "Example Company Ltd" --email ana@example.com --name "Ana Silva" --role owner --password-stdin
```

| Target | What it does |
| --- | --- |
| `make up DB=sqlite\|mariadb\|postgres` | Start the stack (SQLite by default); MariaDB/PostgreSQL start in their own container and MeetHub waits for them. |
| `make down` / `make down V=1` | Stop the stack / also delete its volumes (data loss). |
| `make logs` | Follow the server log. |
| `make dev` | Vite dev server with hot reload on `http://localhost:5173`, in front of the stack. |
| `make build` | `dist/meethub`: the static linux/amd64 binary with the SPA embedded (never committed). |
| `make image` | The runtime image `meethub:<version>` (distroless, non-root, `/data` volume, port 8080). |
| `make check`, `make test`, `make test-db`, `make test-e2e` | Lint, unit tests, database matrix, browser tests (all in containers). |

Without `make`: `docker build --platform linux/amd64 --target export --output dist .` produces the same binary.

## Services

- **meethub** — the server (HTTP API, web app, background workers). Health check: `/meethub healthcheck` (liveness,
  `/api/v1/health`); readiness: `GET /api/v1/ready` (database, pending migrations, Redis).
- **redis** — required: Asynq background jobs and shared login rate limits. AOF persistence on the `redis-data` volume.
- **database** — one of: SQLite (default, `meethub-data` volume, single node only), MariaDB 11 or PostgreSQL 17.
  Migrations run at startup (`meethub server --no-migrate` to skip; `meethub migrate up|down|status` by hand).

## Configuration

Settings come from defaults, then `config.toml` (mounted at `/etc/meethub/config.toml`), then `MEETHUB_*` variables
(`MEETHUB_<SECTION>_<KEY>`, e.g. `MEETHUB_HTTP_PUBLIC_URL`). Every setting is documented in `config.toml.example`.

**Secrets only in the environment** (`.env`, never committed) or in files named by `<NAME>_FILE` (Docker secrets):
`MEETHUB_DATABASE_PASSWORD`, `MEETHUB_REDIS_PASSWORD`, `MEETHUB_SETUP_TOKEN`. A secret in `config.toml` is rejected.

## Production checklist

- `app.env = "production"` and an **HTTPS** `http.public_url` (required): cookies become `Secure` with the
  `__Host-` prefix and HSTS is sent.
- Put a reverse proxy in front for TLS, and list its address in `http.trusted_proxies` so `X-Forwarded-For` /
  `X-Forwarded-Proto` are honored (they are ignored from any other peer). Allow WebSocket upgrades on `/api/v1/ws`
  for the chat (later release).
- Prefer PostgreSQL or MariaDB; SQLite is for single-node installs (never on a shared/network filesystem).
- Back up the database, the `meethub-data` volume and Redis' AOF; test restores.
- Upgrades: pull/build the new image and restart; migrations apply at startup, one process at a time (migration lock).
