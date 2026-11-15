# Deployment

Everything builds in Docker; the host needs Docker (and `make`). The only target is **linux/amd64**. This guide
runs MeetHub in containers; to run it straight on a server, as a systemd service in `/opt/meethub`, see
[Install on Ubuntu and Debian](install-linux.md) (packages) or [Install the binary](install-binary.md) — the
configuration, the checklist and the sections on storage, proxies and meetings below apply to them too.

How the pieces fit together (the server, the database, Redis, file storage and LiveKit) is drawn in
[Architecture](architecture.md).

## Quick start (Compose)

```bash
git clone https://github.com/jniltinho/meethub && cd meethub
make up                 # creates config.toml and .env from the examples, builds the image, starts MeetHub, Redis and LiveKit
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
| `make up DB=sqlite\|mariadb\|postgres` | Start the stack (SQLite by default); MariaDB/PostgreSQL start in their own container and MeetHub waits for them. Administration → Settings is editable on this local stack (`compose.local.yml`: the changes last until the container is created again, `config.toml` of the checkout stays the base); an install opts in with `compose.settings.yml` ([Container installs](admin-settings.md#container-installs-opt-in)). |
| `make down` / `make down V=1` | Stop the stack / also delete its volumes (data loss). |
| `make logs` | Follow the server log. |
| `make demo` | Fill the running stack with sample users, teams, channels, threads and conversations to try the chat on localhost (creates the owner `ana@example.com` on an install without users). The password is public (`DEMO_PASSWORD`): demo data only, never on a real install. |
| `make dev` | Vite dev server with hot reload on `http://localhost:5173`, in front of the stack. |
| `make build` | `dist/meethub`: the static linux/amd64 binary with the SPA embedded (never committed). |
| `make image` | The runtime image `meethub:<version>` (distroless, non-root, `/data` volume, port 8080). |
| `make check`, `make test`, `make test-db`, `make test-e2e` | Lint, unit tests, database matrix, browser tests (all in containers). |
| `make test-load` | Load test at the scale budget: 1 000 connections, `@channel` in a 500-member channel, slow consumers and a Redis restart; prints the latency and delivery thresholds it enforces. |

Without `make`: `docker build --platform linux/amd64 --target export --output dist .` produces the same binary.

## All-in-one image

One container with the server, Redis and LiveKit (meetings) under supervisord, SQLite on one volume — for a single
host, with no Compose file, no `config.toml` and no build:

```bash
docker run -d --name meethub -p 8080:8080 -p 7880:7880 -p 7881:7881 -p 7882:7882/udp \
  -v meethub-data:/data --stop-timeout 55 --restart unless-stopped ghcr.io/jniltinho/meethub:main-allinone
docker logs meethub     # the first start logs the setup URL and the one-time setup token
```

That serves browsers on the same machine (`http://localhost:8080`). For other machines, tell LiveKit and the web
app the host's address (here `192.0.2.10`) and publish TURN too, for browsers behind symmetric NATs:

```bash
docker run -d --name meethub -p 8080:8080 -p 7880:7880 -p 7881:7881 -p 7882:7882/udp -p 3478:3478/udp \
  -e LIVEKIT_NODE_IP=192.0.2.10 -e MEETHUB_LIVEKIT_URL=ws://192.0.2.10:7880 \
  -e MEETHUB_HTTP_PUBLIC_URL=http://192.0.2.10:8080 \
  -v meethub-data:/data --stop-timeout 55 --restart unless-stopped ghcr.io/jniltinho/meethub:main-allinone
docker exec meethub meethub meetings check   # LiveKit answers with the key, TURN allocates
```

Browsers allow camera and microphone only on `https://` pages or `localhost`: beyond a test on one machine, put a
TLS proxy in front of 8080 and 7880 as in [Meetings](#meetings-livekit) (`wss://` for `MEETHUB_LIVEKIT_URL`).

`main-allinone` is the latest build of `main`; releases also publish `allinone` and versioned tags (see the table).

Open `http://localhost:8080` and finish the setup, or create the owner from the CLI:

```bash
docker exec -i meethub meethub user create --org-name "Example Company Ltd" --email ana@example.com \
  --name "Ana Silva" --role owner --password-stdin
```

| | |
| --- | --- |
| **Tags** | `allinone` (latest release), `X.Y.Z-allinone`, `X.Y-allinone`, `main-allinone`, `sha-<short>-allinone`; linux/amd64. Published by `.github/workflows/docker-image-publish.yml` after its smoke test. |
| **Ports** | `8080` (HTTP: web app, API, WebSocket); LiveKit: `7880` (signalling), `7881/tcp` and `7882/udp` (media), `3478/udp` (TURN, optional). Redis listens only on the container's loopback: port 6379, declared by the Redis base image, answers nobody from outside. |
| **Volume** | `/data`: `meethub.db` (SQLite), `files/` (uploaded files), `redis/` (Redis append-only file: queued jobs), `livekit/` (the LiveKit key and secret, created on the first start, readable only by the container's user) and the first-run `setup-token`. Back up the whole volume with the container stopped. |
| **User** | Everything runs as uid/gid `65532`, never root. A named volume gets that owner by itself; a bind-mounted host directory needs `chown -R 65532:65532 <dir>` first (the container refuses to start and says so otherwise). |
| **Processes** | `redis`, `livekit` and `meethub`: `docker exec meethub supervisorctl status` / `restart meethub`; all logs go to `docker logs`. Stopping takes up to 50 seconds, hence `--stop-timeout 55`. |
| **Health** | `meethub healthcheck` (liveness, `/api/v1/health`); readiness on `GET /api/v1/ready`. |

Every setting is a `MEETHUB_<SECTION>_<KEY>` variable (`-e`), as in [Configuration](#configuration); a
`config.toml` mounted at `/etc/meethub/config.toml` (or placed in the volume as `/data/config.toml`) works too. The
image presets these:

| Variable | Value in the image | Notes |
| --- | --- | --- |
| `MEETHUB_APP_DATA_DIR` | `/data` | Keep it: the volume. |
| `MEETHUB_HTTP_ADDR` | `:8080` | Keep it and change the published port instead (`-p 9000:8080`). |
| `MEETHUB_REDIS_ADDR` | `127.0.0.1:6379` | The Redis inside the container. |
| `MEETHUB_HTTP_PUBLIC_URL` | `http://localhost:8080` (default) | Set it to the address people open; WebSocket upgrades from any other origin are refused. |
| `MEETHUB_APP_ENV` | `development` (default) | `production` requires an `https://` public URL (see the [checklist](#production-checklist)). |
| `MEETHUB_HTTP_TRUSTED_PROXIES` | empty (default) | The reverse proxy's address or network, comma-separated, e.g. `172.17.0.1`. |
| `MEETHUB_SETUP_TOKEN` | generated (default) | Secret: `-e`, `--env-file` or `MEETHUB_SETUP_TOKEN_FILE`; never in an image or a `config.toml`. |
| `MEETHUB_SECRET_KEY` | empty (default) | Secret, as above: 32 random bytes in base64 (`openssl rand -base64 32`). Without it two-factor authentication is unavailable (see [Two-factor authentication](#two-factor-authentication)). |
| `MEETHUB_FILES_DRIVER` | `local` (default) | Attachments in `/data/files`. For a bucket set `s3` and the `MEETHUB_FILES_S3_*` variables (see [File storage](#file-storage)). |
| `MEETHUB_LIVEKIT_URL` | `ws://localhost:7880` | What browsers reach LiveKit at: the host's address for other machines, `wss://` behind a TLS proxy. |
| `MEETHUB_LIVEKIT_API_URL` | `http://127.0.0.1:7880` | The LiveKit inside the container. |
| `MEETHUB_LIVEKIT_API_KEY`, `MEETHUB_LIVEKIT_API_SECRET` | from `/data/livekit/api-key`, `/data/livekit/api-secret` | Created on the first start and kept in the volume; the image's LiveKit uses the same pair. Setting the variables (or their `_FILE` forms), e.g. for an external LiveKit, replaces them. |
| `LIVEKIT_NODE_IP` | `127.0.0.1` | The address browsers reach the media ports at: set it to the host's address for other machines. |
| `MEETHUB_SETTINGS_APPLIER` | `off` (default) | `on` makes Administration → Settings editable: the changes go to `/data/config/config.toml`, with a restart, a confirmation and a rollback. Read [Container installs](admin-settings.md#container-installs-opt-in) first. |

Upgrade: `docker pull`, `docker stop meethub && docker rm meethub` and the same `docker run`; the data stays in the volume and
migrations apply at startup. `make image-allinone` builds the image from a checkout (`meethub:allinone`) and
`make test-allinone` smoke tests it. For MariaDB/PostgreSQL, several replicas or an external Redis, use the Compose
stack and the minimal `make image` instead.

## Services

- **meethub** — the server (HTTP API, web app, background workers). Health check: `/meethub healthcheck` (liveness,
  `/api/v1/health`); readiness: `GET /api/v1/ready` (database, pending migrations, Redis, file storage).
- **redis** — required: Asynq background jobs, shared rate limits (logins, messages, typing), presence and the chat
  event bus between replicas (Pub/Sub channel `meethub:events`). AOF persistence on the `redis-data` volume.
- **livekit** — meetings: the LiveKit SFU (signalling on 7880, media on 7881/tcp and 7882/udp), sending its webhooks
  to MeetHub. Optional: without LiveKit settings MeetHub runs with meetings off.
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
`MEETHUB_FILES_S3_SECRET_KEY`, `MEETHUB_LIVEKIT_API_KEY`, `MEETHUB_LIVEKIT_API_SECRET`, `MEETHUB_SMTP_PASSWORD`, `MEETHUB_SECRET_KEY`, `MEETHUB_OIDC_CLIENT_SECRET`. A secret in
`config.toml` is rejected. `make init` generates the LiveKit key and a random 32-byte secret into `.env` when they are missing; Compose
gives the same pair to LiveKit.

Administration → Settings shows every setting here read-only: a container is configured by its variables and has
no settings applier. On the Linux packages an owner can change a few simple settings from that page, written to
`config.toml` by a root applier with a confirmation and an automatic rollback ([Settings in Administration](admin-settings.md)).
A container can opt in to the same page — `compose.settings.yml` in the Compose stack,
`MEETHUB_SETTINGS_APPLIER=on` in the all-in-one image — with the configuration then kept in the data volume and
no separation between the applier and the server: [Container installs](admin-settings.md#container-installs-opt-in).

## Production checklist

- `app.env = "production"` and an **HTTPS** `http.public_url` (required): cookies become `Secure` with the
  `__Host-` prefix and HSTS is sent.
- TLS: MeetHub can serve HTTPS itself (`http.cert_file` and `http.key_file`, or `http.self_signed`; see
  [Standalone with HTTPS](standalone-https.md)), or put a reverse proxy in front and list its address in `http.trusted_proxies` so `X-Forwarded-For` /
  `X-Forwarded-Proto` are honored (they are ignored from any other peer). Allow WebSocket upgrades on
  `/api/v1/websocket` (see below).
- Prefer PostgreSQL or MariaDB; SQLite is for single-node installs (never on a shared/network filesystem). On
  SQLite the server queues its own write transactions first come first served; another process writing the same
  file (the CLI, a migration) waits up to 30 s for SQLite's lock.
- Back up the database, the `meethub-data` volume (it holds the attachments of the `local` driver) or the bucket,
  and Redis' AOF; test restores.
- Set `MEETHUB_SECRET_KEY` (two-factor authentication, see below) and keep a copy of it apart from the database
  backups: a backup restored without its key locks out everyone who uses an authenticator app.
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

Message search uses a full-text index kept in the database (SQLite FTS5, MariaDB `FULLTEXT`, PostgreSQL
`tsvector` with GIN), about the size of the message text on disk. After the upgrade that adds it (migration
`00007`), a background task (`search:backfill`, one replica at a time) indexes the existing messages in batches of
1 000, continuing after a restart; search scans the messages until it is done. On MariaDB, words shorter than
`innodb_ft_min_token_size` or among the server's full-text stop words are matched by scanning the indexed text.
Rolling back `00007` drops the index, which this version needs: roll it back only together with the previous
version of MeetHub (which scans).

A daily background task (`activity:cleanup`, once per day across replicas) removes the Activity items older than 90
days. Rolling back migration `00006` drops the Activity list and the users' "seen" marks.

## Real-time chat behind a proxy

The chat keeps one WebSocket per browser tab on `GET /api/v1/websocket`. The proxy must pass the upgrade and keep
idle connections open longer than the server's 30-second ping. A complete nginx setup, with TLS, the meetings and
TURN, is in [MeetHub behind nginx](nginx.md); the part that matters for the chat:

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

**Scale budget.** Designed for, per install (`make test-load` exercises part of it: 1 000 connections, `@channel` in a
500-member channel, slow consumers and a Redis restart): 2 000 users; 5 000 concurrent connections across replicas
(at most 10 per user); channels up to 1 000 members; 50 messages per second sustained. Per connection the server
queues at most 256 events / 1 MiB (slower clients are disconnected and resynchronize) and accepts frames up to
64 KiB and 20 actions per second. Each user may post 30 messages per 10 seconds. Beyond this budget, plan for
channel-scoped fan-out (a later change).

## Meetings (LiveKit)

MeetHub issues the access tokens (2 minutes, one room, only to members who may read the channel) and LiveKit carries
the media. Settings: `livekit.url` (what browsers reach, `wss://` in production), `livekit.api_url` (what the server
reaches, default `url` with `ws`→`http`) and the key and secret above. The Compose stack serves a browser on the same
host (`ws://localhost:7880`, `--node-ip 127.0.0.1`). For other machines and production:

- Serve LiveKit's signalling over TLS, proxied to port 7880 with WebSocket upgrades — on a path of the chat's own
  name (`wss://meethub.example.com/livekit`, see [MeetHub behind nginx](nginx.md)) or on a name of its own
  (`wss://livekit.example.com`) — and set `MEETHUB_LIVEKIT_URL` to it; the CSP of the web app allows exactly that origin.
- Set `LIVEKIT_NODE_IP` to the address browsers reach the host at and open 7881/tcp and 7882/udp.
- LiveKit must reach `POST /api/v1/meetings/webhook` (signed with the API key and secret); MeetHub also corrects the
  room lists from LiveKit's API every 3 seconds, so a missed webhook heals itself.

**TURN.** Browsers behind symmetric NATs or firewalls that block the media ports — most corporate networks — reach
LiveKit through its built-in TURN server, which the Compose stack turns on (`make init` adds its settings to `.env`):

| What | Where | Needed for |
| --- | --- | --- |
| `3478/udp` | published by the stack, open it in the firewall | TURN over UDP (symmetric NATs). Always on. |
| `turn.example.com` → the public host | DNS (A/AAAA record) | TURN over TLS: browsers reach `turns:turn.example.com:443`. |
| `LIVEKIT_TURN_DOMAIN=turn.example.com` | `.env` | Turns TURN over TLS on; empty leaves it off. |
| `443/tcp` with SNI `turn.example.com` → `livekit:5349` | the reverse proxy | The proxy ends TLS with its certificate for that name and passes the plain TCP stream to LiveKit (`external_tls`); the stack publishes 5349 on `127.0.0.1` only. |

LiveKit always announces TURN over TLS on port 443, so the proxy that serves the web app on 443 must route the TURN
name by SNI to a TCP (layer 4) stream instead of HTTP. With nginx, in the `stream` block; the PROXY protocol
keeps the browsers' addresses through the extra hop, so the `http` sites then listen on
`127.0.0.1:8443 ssl proxy_protocol` with `set_real_ip_from 127.0.0.1;` and `real_ip_header proxy_protocol;`
(otherwise every request seems to come from `127.0.0.1` and shares one login rate limit):

```nginx
stream {
    map $ssl_preread_server_name $upstream {
        turn.example.com turn_tls;
        default          https_sites;
    }
    upstream turn_tls    { server 127.0.0.1:5350; }
    upstream https_sites { server 127.0.0.1:8443; }
    server { listen 443; ssl_preread on; proxy_pass $upstream; proxy_protocol on; }

    # ends TLS for the TURN name, then plain TCP to LiveKit
    server {
        listen 127.0.0.1:5350 ssl proxy_protocol;
        ssl_certificate     /etc/letsencrypt/live/turn.example.com/fullchain.pem;
        ssl_certificate_key /etc/letsencrypt/live/turn.example.com/privkey.pem;
        proxy_pass 127.0.0.1:5349;
        proxy_timeout 1h;
    }
}
```

Traefik does the same with a TCP router (`HostSNI(\`turn.example.com\`)`, `tls: {}`, service `livekit:5349`). A
browser that may use only outgoing TCP 443 then joins through `wss://meethub.example.com/livekit` and `turns:turn.example.com:443`.
LiveKit relays only to its own media ports on `LIVEKIT_NODE_IP`, which may be a private address.

**Check.** `meethub meetings check` (in the stack: `docker compose --env-file .env -f deployments/compose/compose.yml
exec meethub /meethub meetings check`) reports whether LiveKit's room API accepts the key, the browser URL in use,
and whether a TURN allocation succeeds on each TURN server LiveKit hands browsers (it joins a throwaway room to get
them, then deletes it); it exits 0 only when everything passes and never prints the key, the secret or a token. A
TURN server on `127.0.0.1` (the default `LIVEKIT_NODE_IP`) is tried at `livekit.api_url`'s host. Run it on a
machine outside your network too (the binary with the same settings) to see what remote browsers see.

## Recordings

Meetings can be recorded in their channel (off by default). A hidden participant of the server captures each
person's voice and, in video mode, their camera and shared screen to the **capture volume**; when the recording
ends, `ffmpeg` mixes the voices into one `.m4a` (audio mode) or composes a `.webm` (video mode: the shared screen
with the cameras in a strip, or a grid of cameras, with name tiles), and the server posts it in the channel as an
attachment of whoever started it. Settings, under `[recordings]` (environment: `MEETHUB_RECORDINGS_<KEY>`):

| Setting | Default | What |
| --- | --- | --- |
| `enabled` | `false` | Needs meetings (LiveKit). On, startup runs a real two-second recording through `ffmpeg` in the configured mode and refuses to start, naming the setting, when it fails. |
| `mode` | `audio` | `audio` (`.m4a`, plays in every browser) or `video` (`.webm`, VP8 + Opus). |
| `video_format` | `webm` | `mp4` (H.264 + AAC) needs an `ffmpeg` with `libx264`: Debian's and Ubuntu's, which the `.deb` brings, has it; the images' own `ffmpeg` and EPEL's `ffmpeg-free` do not. |
| `start` | `admins` | Who may record: channel administrators (and team administrators and `manage_system` holders who are members); in direct and group conversations any member. `members`: any member. |
| `max_minutes` / `max_bytes` | `240` / 4 GiB | A recording stops at this length or when its captures reach this size. |
| `max_concurrent` | `2` | Recordings one server captures at once; beyond it, starts answer `503 recordings_busy`. |
| `retention_days` | `0` | Recordings older than this lose their file (their message says so); `0` keeps them. |
| `ffmpeg_path` | `ffmpeg` | A name looked up in `PATH`, or a path. The images carry `/usr/local/bin/ffmpeg`, built without GPL parts. The Linux packages use the distribution's: the `.deb` depends on `ffmpeg`; on AlmaLinux and RHEL install `ffmpeg-free` from EPEL, or RPM Fusion's `ffmpeg` for MP4. |
| `dir` | `<data_dir>/recordings` | The capture volume. |

Where the [settings applier](admin-settings.md) runs (the Linux packages, a container that opted in), an owner
changes these in Administration → Settings, except `retention_days`, `ffmpeg_path` and `dir`.

**The capture volume must survive restarts.** A recording in progress lives only there until it is composed: a
server that restarts with the same volume finishes it (a capture cut by a crash is posted as a partial recording);
a recording whose volume has not come back within 24 hours is marked failed and the channel is told. In the Compose
stack and the all-in-one image it is inside the `/data` volume; with the Linux packages it is
`/opt/meethub/data/recordings`, inside the unit's `ReadWritePaths` (another `dir` needs that path added in a
drop-in, like `files.local.dir`). With several servers each one composes what it captured: keep each server's
volume with it.

**Disk and CPU.** A start is admitted only when the volume's free space covers, for every recording in progress and
the new one, its capture budget (`max_bytes`) and the peak of its composition (about 1.8 Mbit/s of recording in
audio mode, 4.3 Mbit/s in video mode: 3.3 GB and 7.8 GB for 4 hours). Captures write about 50 kbit/s per voice
and 0.3–1.5 Mbit/s per camera. Composition runs one at a time per server, with two threads at a lowered priority
and a time limit of four times the recording's length; audio mode takes seconds per hour of meeting, video mode
minutes. On failure the server logs only an error class; `ffmpeg`'s own output stays beside the captures.

**Network.** The recorder joins LiveKit like a browser, at `livekit.api_url`, and needs its media. In the Compose
stack and the all-in-one image this works as shipped (LiveKit's connectivity checks reach the server's container);
for a LiveKit on another host or behind a firewall that drops them, set `rtc.advertise_internal_ip: true` in
LiveKit (it then also announces its own address, which the server must be able to route to).

**Formats and browsers.** `.m4a` (AAC) plays everywhere; `.webm` (VP8 + Opus) plays in current Chrome, Edge,
Firefox and Safari 14.1+ (older Safari: download it). Everyone may download a recording of a channel they read.

**Who is told.** The room shows "Recording" to everyone while it runs, the start and the stop are announced, and the
pre-join screen says so before anyone enters; starting and stopping are audited. This informs participants; it is
not their consent, which some jurisdictions require — whether informing is enough is your organization's call.

## Meeting summaries

An AI service the install configures can transcribe a recording and write an organized summary of what was said
(Summary, Decisions, Action items with their owners, Topics with who said what, Open questions, in the language of
the meeting), posted as a reply in the recording's thread with the full transcript attached. It needs recordings on
and **an OpenAI-compatible API**: OpenAI, a gateway, or a server of your own. Settings, under `[ai]` (environment:
`MEETHUB_AI_<KEY>`):

| Setting | Default | What |
| --- | --- | --- |
| `base_url` | `https://api.openai.com/v1` | The API's base; `POST <base>/audio/transcriptions` and `POST <base>/chat/completions` are called. `http://` only to a loopback or private address (a name is resolved at startup: all its addresses must be private); no credentials, query or fragment. Redirects are never followed. |
| key | — | Only `MEETHUB_AI_API_KEY` (or `_FILE`); a key in `config.toml` stops the server. |
| `transcription_model` / `summary_model` | — | Required: MeetHub has no built-in model names. AI is *configured* when the key and both are set. |
| `language` | `""` | The language spoken; `""` lets the services detect it. An ISO-639-1 code (`pt`, `en`, `es`) is sent to the transcription and asked of the summary. |
| `timeout` | `5m` | Per request. Large reasoning models may take minutes. |
| `max_upload_bytes` | `20000000` | Per transcription request; summary sources are cut to a quarter of it. |
| `context_tokens` / `max_output_tokens` | `32000` / `8000` | What the summary model accepts and may write. Reasoning models count their reasoning in `max_output_tokens`: with a small one they answer nothing. A long meeting is summarized in parts that fit. |
| `[ai.transcription] base_url` | `""` | Another service for speech to text, with its own key `MEETHUB_AI_TRANSCRIPTION_API_KEY`. |

**The organization decides.** Configuration says what MeetHub *can* reach; an owner turns it on in
Administration → Meeting summaries: **Off** (the default: nothing of any meeting is sent), **Manual** (who may record a
channel asks for a recording's summary from its message) or **Automatic** (every recording is summarized when it is
ready). Turning it on first runs a connection test — a generated three-second tone to the transcription endpoint,
which must answer **timed segments** (`response_format=verbose_json`), and a one-line request to the summary model;
a service that cannot do either is refused, with the reason. "Test connection" runs it on demand. Turning it off
cancels the queued summaries and stops the running ones within 10 seconds on every server; what was already sent
stays with the service. A recording keeps the setting in force when it **started**: one made while it was off can
never be summarized, and "off" always prevails.

**Who is told.** Under Automatic the room shows "Recording · AI summary" and the pre-join screen says the meeting
will be summarized by AI; under Manual the start confirmation, the announcement and the pre-join screen say an AI
summary may be written from the recording. The summary is labelled "Written by AI from the recording — check the
transcript", by the interface, never by the model.

**How.** When a recording is composed, each participant's voice is also re-encoded for speech (mono 16 kHz Opus,
24 kbit/s) and cut **in pauses** into pieces of at most five minutes, kept in the file storage (not listed, not
downloadable) for seven days. Each piece is transcribed, the timed segments are placed on the meeting's timeline
and ordered, every passage carrying the participant's display name: attribution is **by participant** — whose
microphone it was —, not by voice (two people at one microphone are one speaker). Summary tasks have their own queue
and workers (two per server, at most four AI requests at once); failures are retried with the queue's backoff and
then shown on the recording with "Try again"; a wrong key fails at once. Pieces already transcribed are not sent
again on a retry.

**What is kept, and for how long.**

| Thing | Removed when |
| --- | --- |
| Summary sources and transcriptions of pieces | the summary is posted, seven days after the recording, or the recording's message is deleted |
| Transcript file | with its message, and by the recording's retention |
| Summary reply | with the recording's message (a thread goes with its root) |
| Summary text after the recording's retention | **stays**, as the minutes of the meeting; the message says the recording and transcript are gone |

**Costs.** A hosted service charges per minute of audio and per token: every participant's speech is transcribed
(silence is not sent), and the summary of a long meeting takes several requests. Manual mode spends only on what
people ask for.

**A server of your own.** whisper.cpp's `whisper-server` (Ubuntu 26.04: package `whisper.cpp`) speaks the
transcription endpoint when started with `--inference-path /v1/audio/transcriptions`, `--convert` (it needs `ffmpeg`
to read Ogg/Opus) and `-l auto` (otherwise it transcribes everything as English), e.g.
`whisper-server -m ggml-small.bin --host 0.0.0.0 --port 8000 --inference-path /v1/audio/transcriptions --convert -l auto`;
set `[ai.transcription] base_url = "http://whisper:8000/v1"` and any non-empty `MEETHUB_AI_TRANSCRIPTION_API_KEY`
(it ignores the key and the model name). A multilingual model (`ggml-base.bin` or larger, not `*.en`) is needed for
Portuguese or Spanish; the larger, the better. Any OpenAI-compatible chat API writes the summary.
`ffmpeg` must have the `silencedetect` and `ametadata` filters (MeetHub's own build has them).

## Notification e-mails (SMTP)

With `smtp.host` set, MeetHub e-mails people about direct messages and mentions by name that are still unread after
10 minutes while they are not online — one e-mail per person every 15 minutes at most, listing up to 20 messages
(who, where, the start of the message, a link) and counting the rest. Each person can turn them off in Settings →
Notifications or with the link of any e-mail. Without `smtp.host` nothing is queued and Settings says e-mail is not
available.

```toml
[smtp]
host = "smtp.example.com"
port = 587                 # 0 = 587 (starttls), 465 (tls) or 25 (none)
username = "meethub"       # the password: MEETHUB_SMTP_PASSWORD
from = "MeetHub <meethub@example.com>"
tls = "starttls"           # starttls | tls | none (none with a password: loopback or private hosts only)

[notifications]
email_excerpt = true       # false: e-mails say only who wrote and where
```

A task (`notify:email`) runs every minute on one replica. It sends nothing while Redis (presence) is unavailable, so
nobody who may be online is e-mailed; a failed send keeps the messages for a later run. The links point at
`http.public_url`. Desktop notifications need no server setting: each person allows them in Settings →
Notifications, and the browser shows them while the app is in the background or another conversation is open.

## Two-factor authentication

People add an authenticator app (any TOTP app: Google Authenticator, Aegis, 1Password, …) in Settings → Security; their
logins then ask for a code after the password. The stored secrets are encrypted with `MEETHUB_SECRET_KEY`: 32 random
bytes in base64 (`openssl rand -base64 32`), in the environment or a file named by `MEETHUB_SECRET_KEY_FILE`, never in
`config.toml`. `make init` adds one to `.env`. Without it MeetHub starts and works, and Settings says two-factor
authentication is not available; a key of another length fails startup.

- **Back the key up** with the backup instructions, but not in the same place as the database dumps.
- **A key lost or changed**: people with two-factor on cannot log in. Reset each from the server's console:
  `meethub user reset-two-factor --email ana@example.com` (with Compose: `docker compose exec meethub /meethub user
  reset-two-factor --email …`); they log in with the password alone and enroll again.
- **A lost phone**: the person logs in with one of their recovery codes; without them, an administrator resets their
  two-factor in Administration → Users (owners only by owners), which ends all of their sessions; the last owner uses
  the command above.

## Single sign-on (OpenID Connect)

One OpenID Connect provider — Google Workspace, Microsoft Entra ID, Keycloak, Authentik, Okta, … — can sign people
in; the login page then shows "Sign in with <name>" beside the password form.

At the provider, register a confidential web client:

- **Redirect URI:** `<http.public_url>/api/v1/auth/oidc/callback`, e.g. `https://meethub.example.com/api/v1/auth/oidc/callback`.
- **Grant:** authorization code (MeetHub sends PKCE with S256). **Scopes:** `openid email profile`.
- The ID token must carry `email` and `email_verified: true` (or `"true"`); without them every login is refused.
  Some providers leave `email_verified` out by default (Microsoft Entra ID among them): check the provider's token
  settings with a test account first.

```toml
[oidc]
issuer = "https://id.example.com/realms/acme"   # https:// only; MeetHub reads <issuer>/.well-known/openid-configuration
client_id = "meethub"                          # the secret: MEETHUB_OIDC_CLIENT_SECRET
name = "Acme ID"                               # the button: "Sign in with Acme ID"
auto_create = false                            # true: an unknown, verified e-mail gets an account
allowed_domains = ["example.com"]              # e-mail domains auto_create accepts; empty = any
default_role = "member"                        # member | guest

[auth]
password_login = true                          # false: only the single sign-on button
```

People who already have a MeetHub account sign in with the provider when the e-mails match; the first single sign-on
links the account to the provider's subject. Check the provider once by signing in with a test account before turning
`password_login` off. If the provider is lost while passwords are off, set `MEETHUB_AUTH_PASSWORD_LOGIN=true` and
restart; `meethub user set-password` works on the server's console at any time. See `docs/security.md` for the trust
model.
