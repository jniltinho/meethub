# Proposal

## Why

Trying MeetHub, or running it on one small host, takes a clone, `make`, a Compose file, a `config.toml` and a `.env`,
because the server needs Redis beside it. A single published image that carries both — as the author's go-guacamole
project already offers (`docker/allinone`) — turns that into one `docker run`.

## What Changes

- **All-in-one image**: a new image runs the MeetHub server and Redis in one container under a process supervisor,
  with SQLite and everything that must persist (database, uploaded files, Redis' append-only file, setup token) on
  the single `/data` volume. Nothing in it runs as root, Redis is reachable only inside the container, and the
  health check is the server's own liveness probe.
- **Published package**: a GitHub Actions workflow builds the image for linux/amd64, smoke tests it and publishes it
  to the repository's package on GHCR (`ghcr.io/jniltinho/meethub`) on pushes to `main` and on `v*` tags, with
  `-allinone` tags; pull requests only build and test it.
- **Make targets**: `make image-allinone` builds it from a checkout and `make test-allinone` smoke tests it.
- **Documentation**: `docs/deployment.md` gains the `docker run` line, the volume, the port and the environment
  variables.

Out of scope: publishing the minimal runtime image (the plain tags stay free for it), other architectures,
MariaDB/PostgreSQL or an external Redis inside this image (the Compose stack and the runtime image cover them),
TLS termination, and starting as root to change the owner of a bind-mounted directory.

## Capabilities

### New Capabilities

None.

### Modified Capabilities

- `build-and-deploy`: adds the all-in-one image, its smoke test and its publication to the repository's package.

## Impact

- **Build**: `Dockerfile` gains the `allinone` target (the default target stays `runtime`), on the Redis image
  already pinned by digest in the Compose stack; `docker/allinone/` holds the supervisor configuration, the
  entrypoint and the smoke test; `Makefile` gains two targets.
- **CI**: new `.github/workflows/docker-image-publish.yml` (needs `packages: write` for its own token; no secrets).
- **Docs**: `docs/deployment.md`.
- **Server code, API, database**: none.
