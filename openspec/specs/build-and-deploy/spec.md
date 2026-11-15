# build-and-deploy Specification

## Purpose
Defines how MeetHub is built and run: reproducible builds inside Docker producing a linux/amd64 binary in `dist/`, a
container image, and a Compose stack for development and small installs — with nothing to install on the host but
Docker.

## Requirements

### Requirement: Containerized build
`make build` SHALL build the SPA and the Go binary inside Docker containers with pinned base images, needing only
Docker (and make) on the host, and SHALL write a single static `dist/meethub` executable for linux/amd64.

#### Scenario: Build on a clean host
- **GIVEN** a host with Docker and make but no Go or Node.js
- **WHEN** the developer runs `make build`
- **THEN** `dist/meethub` exists, a check inside Docker confirms a static ELF x86-64 executable, and
  `dist/meethub version` prints the git version

#### Scenario: Build with Docker only
- **GIVEN** a fresh clone on a host with Docker and nothing else (no make, Go or Node.js)
- **WHEN** the developer runs the single `docker build --platform linux/amd64 --target export --output dist .` command
  documented in the README
- **THEN** `dist/meethub` is produced, reporting version `dev` when no version is passed

### Requirement: Only linux/amd64 is produced
The build SHALL target linux/amd64 only; no other operating system or architecture is built, packaged or released.

#### Scenario: Single artifact
- **WHEN** `make build` finishes in an empty `dist/`
- **THEN** `dist/` contains only `meethub`

#### Scenario: Packages beside the binary
- **WHEN** `make package` finishes
- **THEN** `dist/packages/` holds the `.deb`, the `.rpm` and the `.tar.gz`, each for linux/amd64 only

### Requirement: Private and generated files stay out of git and build contexts
`dist/`, the built SPA in `web/dist` (except its placeholder), binaries, `config.toml`, `.env` files, databases and the
private `base/` folder SHALL be ignored by git, and `.dockerignore` SHALL keep `base/`, secrets, databases, `dist/` and
`node_modules/` out of every Docker build context.

#### Scenario: Ignored after a build
- **WHEN** `make build` runs and then `git status --porcelain` and `git ls-files` are checked
- **THEN** nothing under `dist/`, `web/dist/` (besides `.gitkeep`), `base/` or any `*.db` is listed

#### Scenario: Private folder not in the build context
- **WHEN** an image is built
- **THEN** no file from `base/`, `.env` or `config.toml` is present in any image layer

### Requirement: Container image
`make image` SHALL build a minimal linux/amd64 image running `/meethub server` (exec form) as a non-root user, with
`/data` writable by that user, reading `/etc/meethub/config.toml` and the `MEETHUB_*` environment, exposing port 8080
and declaring an exec-form `HEALTHCHECK` using `/meethub healthcheck`.

#### Scenario: Image health
- **WHEN** the image starts with an empty named `/data` volume
- **THEN** the container becomes healthy and serves the setup page on port 8080

#### Scenario: Liveness during database outage
- **GIVEN** the configured PostgreSQL becomes unreachable after start
- **WHEN** the health check runs
- **THEN** it still passes (liveness), while `/api/v1/ready` reports 503

### Requirement: Compose stack
The repository SHALL provide a Compose stack that always runs MeetHub with Redis (persistent volume, health check),
SQLite by default and, through `make up DB=mariadb|postgres`, that database instead; `make up` SHALL create
`config.toml` and `.env` from their examples when missing and SHALL refuse unknown `DB` values.

#### Scenario: Default stack
- **GIVEN** a fresh clone
- **WHEN** the developer runs `make up`
- **THEN** `config.toml` and `.env` are created from the examples and the `meethub` and `redis` services start, using
  SQLite on a named volume, and `meethub` starts only after Redis is healthy

#### Scenario: PostgreSQL
- **WHEN** the developer runs `make up DB=postgres`
- **THEN** PostgreSQL starts, MeetHub waits for it to be healthy and then uses it instead of SQLite

#### Scenario: Unknown database
- **WHEN** the developer runs `make up DB=oracle`
- **THEN** make fails listing `sqlite`, `mariadb` and `postgres`

### Requirement: Containerized checks and tests
`make check`, `make test` (Go with the race detector, frontend unit tests), `make test-db` (database tests on SQLite,
MariaDB and PostgreSQL) and `make test-e2e` (browser tests against the Compose stack) SHALL run inside Docker and remove
their containers afterwards even on failure.

#### Scenario: Database matrix
- **WHEN** the developer runs `make test-db`
- **THEN** the migration, repository and setup → login → RBAC tests pass on all three dialects and the containers are
  removed afterwards

### Requirement: LiveKit in the Compose stack
The Compose stack SHALL run a LiveKit server (pinned image) next to MeetHub, configured with the same API key and
secret, sending its webhooks to MeetHub, and publishing its signalling and media ports. `make init` SHALL add a
generated LiveKit key and a random secret of 32 bytes to `.env` when missing, so no shared credential is versioned.

#### Scenario: Fresh install
- **GIVEN** a checkout without `.env`
- **WHEN** the operator runs `make up`
- **THEN** `.env` gains `MEETHUB_LIVEKIT_API_KEY` and a random `MEETHUB_LIVEKIT_API_SECRET`, and LiveKit and MeetHub
  start with meetings enabled

#### Scenario: Existing install
- **GIVEN** an `.env` from before this change
- **WHEN** the operator runs `make up`
- **THEN** the LiveKit variables are appended once and the existing values are left untouched

### Requirement: All-in-one image
`make image-allinone` SHALL build a linux/amd64 image, from base images pinned by digest, that runs the MeetHub
server and the Redis it requires in one container, with no configuration file and no other container needed. It
SHALL use SQLite, keep everything that must persist on the single `/data` volume, read the `MEETHUB_*` environment,
expose port 8080 and declare an exec-form `HEALTHCHECK` using the server's liveness probe.

#### Scenario: Single command start
- **GIVEN** a host with Docker and an empty named volume
- **WHEN** the image is started with only that volume mounted at `/data`
- **THEN** the container becomes healthy, `GET /api/v1/ready` answers 200 with the database and Redis checks
  passing, and the container's log shows the setup token

#### Scenario: Data survives a new container
- **GIVEN** a running container in which the owner account was created and a key was written to Redis
- **WHEN** the container is stopped, deleted and created again with the same volume
- **THEN** the owner account and the Redis key are still there and the container becomes ready again

#### Scenario: Default target unchanged
- **WHEN** the repository's `Dockerfile` is built without a target
- **THEN** the result is still the minimal runtime image

### Requirement: All-in-one image isolation
The all-in-one image SHALL run every process as a non-root user, SHALL keep Redis reachable only from inside the
container, and SHALL refuse to start, saying why, when that user cannot write `/data`. Secrets SHALL be accepted
only from the environment, as in every other install.

#### Scenario: Nothing runs as root
- **WHEN** the processes of a running container are listed
- **THEN** none of them runs as uid 0

#### Scenario: Redis is not reachable from outside
- **WHEN** a client outside the container connects to the container's address on port 6379
- **THEN** the connection is refused

#### Scenario: Unwritable data directory
- **GIVEN** a `/data` that the image's user cannot write (a host directory owned by root)
- **WHEN** the image is started
- **THEN** the container exits with a non-zero status and a message naming `/data`, the user id it needs and how to
  fix it, and no server starts

### Requirement: All-in-one process supervision
Inside the all-in-one container a process supervisor SHALL send the log of the server and of Redis to the
container's output, SHALL start again whichever of them exits, and on a stop request SHALL stop the server before
Redis, giving each the time to finish its shutdown.

#### Scenario: A process dies
- **WHEN** the Redis or the server process inside a running container is killed
- **THEN** the supervisor starts it again and the container becomes ready again without being restarted

#### Scenario: Graceful stop
- **WHEN** the container is stopped
- **THEN** the server finishes its shutdown, then Redis does, and the container exits with status 0

### Requirement: All-in-one smoke test
`make test-allinone` SHALL build the all-in-one image and verify, needing only Docker on the host, that it refuses an
unwritable `/data`, becomes healthy and ready on an empty volume as a non-root user, and keeps the database and the
Redis data across a new container; it SHALL remove its container and volume afterwards, even on failure.

#### Scenario: Smoke test passes
- **WHEN** the developer runs `make test-allinone` on a clean checkout
- **THEN** the image is built, the checks pass and no container or volume of the test remains

#### Scenario: Smoke test fails
- **GIVEN** an image whose server cannot start
- **WHEN** the smoke test runs
- **THEN** it exits with a non-zero status, prints the end of the container's log and leaves no container or volume

### Requirement: Published all-in-one package
A GitHub Actions workflow, with its actions pinned by commit, SHALL build the all-in-one image for linux/amd64 and
run its smoke test on every pull request, push to `main` and `v*` tag. Only on pushes to `main` and `v*` tags, and
only when the smoke test passed, it SHALL publish the image to the repository's package on GHCR using the workflow's
own token.

#### Scenario: Pull request
- **WHEN** a pull request is opened
- **THEN** the image is built and smoke tested and nothing is published

#### Scenario: Failing smoke test
- **WHEN** the smoke test fails on a push to `main`
- **THEN** the workflow fails and no image is published

### Requirement: All-in-one package tags
Every published tag of the all-in-one image SHALL end in `allinone`: `main-allinone` and `sha-<short>-allinone` for
a push to `main`; `X.Y.Z-allinone`, `X.Y-allinone`, `sha-<short>-allinone` and `allinone` for a release tag
`vX.Y.Z`. The image SHALL report the version it was built from.

#### Scenario: Release tag
- **WHEN** the tag `v1.2.3` is pushed
- **THEN** `ghcr.io/jniltinho/meethub` receives `1.2.3-allinone`, `1.2-allinone` and `allinone`, and
  `meethub version` in that image reports `v1.2.3`

#### Scenario: Push to main
- **WHEN** a commit is pushed to `main`
- **THEN** the package receives `main-allinone` and `sha-<short>-allinone`, and `allinone` does not move

### Requirement: ffmpeg in the images
The runtime image and the all-in-one image SHALL contain an `ffmpeg` executable built from pinned source without
GPL or non-free components, able to produce both recording formats as the container's user, and SHALL carry its
license texts, source locations and build configuration.

#### Scenario: Real recording in each image
- **WHEN** CI runs the recording probe (mix, compose with a name tile, encode) inside the runtime image and inside
  the all-in-one image as their unprivileged user
- **THEN** both produce a playable `.m4a` and a playable `.webm`

#### Scenario: No GPL components
- **WHEN** the image's `ffmpeg` prints its configuration
- **THEN** it shows neither `--enable-gpl` nor `--enable-nonfree`

### Requirement: ffmpeg with the Linux packages
The Linux packages SHALL carry no `ffmpeg` and SHALL use the distribution's: the `.deb` SHALL depend on it, the
`.rpm` SHALL recommend one without requiring it, and a package install SHALL compose both recording formats with
it, and MP4 where that `ffmpeg` has `libx264`.

#### Scenario: Installed from the .deb
- **WHEN** the `.deb` is installed on Debian or Ubuntu
- **THEN** the distribution's `ffmpeg` is installed with it and the recording probe passes for audio, WebM and MP4

#### Scenario: Installed from the .rpm
- **WHEN** the `.rpm` is installed on AlmaLinux with only its base repositories
- **THEN** it installs without an `ffmpeg`, and with EPEL's `ffmpeg-free` added the recording probe passes for
  audio and WebM

### Requirement: The recorder reaches LiveKit in the stack
In the Compose stack the MeetHub server SHALL be able to join a room's media as the recorder, and the deployment
guide SHALL state what the all-in-one image needs for the same (a LiveKit the container reaches).

#### Scenario: Recording across containers
- **GIVEN** the Compose stack with recordings on and a participant publishing audio
- **WHEN** a recording is started and stopped
- **THEN** the posted file contains that audio
