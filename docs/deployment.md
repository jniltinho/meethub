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
| `make demo` | Fill the running stack with sample users, teams, channels, threads and conversations to try the chat on localhost (creates the owner `ana@example.com` on an install without users). The password is public (`DEMO_PASSWORD`): demo data only, never on a real install. |
| `make dev` | Vite dev server with hot reload on `http://localhost:5173`, in front of the stack. |
| `make build` | `dist/meethub`: the static linux/amd64 binary with the SPA embedded (never committed). |
| `make image` | The runtime image `meethub:<version>` (distroless, non-root, `/data` volume, port 8080). |
| `make check`, `make test`, `make test-db`, `make test-e2e` | Lint, unit tests, database matrix, browser tests (all in containers). |
| `make test-load` | Load test at the scale budget: 1 000 connections, `@channel` in a 500-member channel, slow consumers and a Redis restart; prints the latency and delivery thresholds it enforces. |

Without `make`: `docker build --platform linux/amd64 --target export --output dist .` produces the same binary.

## Services

- **meethub** — the server (HTTP API, web app, background workers). Health check: `/meethub healthcheck` (liveness,
  `/api/v1/health`); readiness: `GET /api/v1/ready` (database, pending migrations, Redis, file storage).
- **redis** — required: Asynq background jobs, shared rate limits (logins, messages, typing), presence and the chat
  event bus between replicas (Pub/Sub channel `meethub:events`). AOF persistence on the `redis-data` volume.
- **database** — one of: SQLite (default, `meethub-data` volume, single node only), MariaDB 11 or PostgreSQL 17.
  Migrations run at startup (`meethub server --no-migrate` to skip; `meethub migrate up|down|status` by hand).

- **file storage** — where attachments live (`[files]`): `driver = "local"` (default) writes under
  `<data_dir>/files`, on the `meethub-data` volume; `driver = "s3"` uses a bucket of any S3-compatible service
  (RustFS, MinIO, AWS S3). See [File storage](#file-storage).

## Configuration

Settings come from defaults, then `config.toml` (mounted at `/etc/meethub/config.toml`), then `MEETHUB_*` variables
(`MEETHUB_<SECTION>_<KEY>`, e.g. `MEETHUB_HTTP_PUBLIC_URL`). Every setting is documented in `config.toml.example`.

**Secrets only in the environment** (`.env`, never committed) or in files named by `<NAME>_FILE` (Docker secrets):
`MEETHUB_DATABASE_PASSWORD`, `MEETHUB_REDIS_PASSWORD`, `MEETHUB_SETUP_TOKEN`, `MEETHUB_FILES_S3_ACCESS_KEY`,
`MEETHUB_FILES_S3_SECRET_KEY`. A secret in `config.toml` is rejected.

## Production checklist

- `app.env = "production"` and an **HTTPS** `http.public_url` (required): cookies become `Secure` with the
  `__Host-` prefix and HSTS is sent.
- Put a reverse proxy in front for TLS, and list its address in `http.trusted_proxies` so `X-Forwarded-For` /
  `X-Forwarded-Proto` are honored (they are ignored from any other peer). Allow WebSocket upgrades on
  `/api/v1/websocket` (see below).
- Prefer PostgreSQL or MariaDB; SQLite is for single-node installs (never on a shared/network filesystem).
- Back up the database, the `meethub-data` volume (it holds the attachments of the `local` driver) or the bucket,
  and Redis' AOF; test restores.
- Upgrades: pull/build the new image and restart; migrations apply at startup, one process at a time (migration lock).

## File storage

Attachments are streamed to the storage chosen by `files.driver`; the database holds only their metadata.

- **`local`** (default): files under `files.local.dir` (default `<data_dir>/files`). With more than one replica the
  directory must be the same shared volume on all of them; a replica that cannot write it fails its readiness check.
- **`s3`**: a bucket of an S3-compatible service — [RustFS](https://github.com/rustfs/rustfs), MinIO or AWS S3. Create
  the bucket first; MeetHub needs to put, get and delete objects in it. The right choice for several replicas.

```toml
[files]
driver = "s3"
max_size_bytes = 52428800   # 50 MiB per file
max_per_post = 10

[files.s3]
endpoint = "rustfs:9000"    # host[:port], without a scheme
bucket = "meethub"
region = "us-east-1"
use_ssl = false             # true for an HTTPS endpoint
```

The credentials come only from the environment: `MEETHUB_FILES_S3_ACCESS_KEY` and `MEETHUB_FILES_S3_SECRET_KEY`.
While the storage is down, uploads and downloads answer `503 storage_unavailable`, `/api/v1/ready` reports `storage`
as failed, and the rest of the chat keeps working.

**Proxy.** One upload request carries up to `max_size_bytes × max_per_post` (500 MiB with the defaults) and is
streamed, so let the proxy pass it through without buffering and without a small body limit:

```nginx
location /api/v1/files {
    proxy_pass http://meethub:8080;
    proxy_set_header Host $host;
    proxy_set_header X-Forwarded-For $proxy_add_x_forwarded_for;
    proxy_set_header X-Forwarded-Proto $scheme;
    client_max_body_size 512m;      # max_size_bytes × max_per_post, plus the form's framing
    proxy_request_buffering off;    # stream to the storage instead of spooling to the proxy's disk
    proxy_read_timeout 600s;        # a slow connection sending a large file
    proxy_send_timeout 600s;
}
```

An hourly background task removes the bytes of deleted messages' files and the uploads never attached to a message
for 24 hours. Rolling back migration `00004` drops the metadata; the stored objects stay and can be removed by hand.

## Real-time chat behind a proxy

The chat keeps one WebSocket per browser tab on `GET /api/v1/websocket`. The proxy must pass the upgrade and keep
idle connections open longer than the server's 30-second ping:

```nginx
location /api/v1/websocket {
    proxy_pass http://meethub:8080;
    proxy_http_version 1.1;
    proxy_set_header Upgrade $http_upgrade;
    proxy_set_header Connection "upgrade";
    proxy_set_header Host $host;
    proxy_set_header X-Forwarded-For $proxy_add_x_forwarded_for;
    proxy_set_header X-Forwarded-Proto $scheme;
    proxy_read_timeout 120s;
    proxy_send_timeout 120s;
    proxy_buffering off;
}
```

Keep the `Host` and `Origin` of the public URL: upgrades whose `Origin` differs from `http.public_url` are refused
(`403`). Traefik and Caddy pass upgrades by default; raise their idle/read timeouts above 60 seconds.

**Replicas.** Any number of replicas can serve the chat behind a load balancer without sticky sessions: events reach
every replica through Redis, presence is shared in Redis, and open sockets are revalidated against the database every
5 seconds (a socket closes within 5 seconds of its session ending, even while Redis is down). If Redis is briefly
unavailable, each replica still delivers to its own sockets, and its clients receive `resync` and reload when the bus
returns. On shutdown (`SIGTERM`) a replica stops accepting upgrades and closes its sockets with status 1001 within
`http.shutdown_timeout`; clients reconnect to another replica with backoff and catch up through the API.

**Scale budget.** Designed for, per install (the load test of this budget is still pending): 2 000 users; 5 000 concurrent connections across replicas
(at most 10 per user); channels up to 1 000 members; 50 messages per second sustained. Per connection the server
queues at most 256 events / 1 MiB (slower clients are disconnected and resynchronize) and accepts frames up to
64 KiB and 20 actions per second. Each user may post 30 messages per 10 seconds. Beyond this budget, plan for
channel-scoped fan-out (a later change).
