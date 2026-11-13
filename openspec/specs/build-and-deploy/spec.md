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
- **WHEN** `make build` finishes
- **THEN** `dist/` contains only `meethub`

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
