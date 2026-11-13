# Spec Delta

## Purpose

Defines how MeetHub is built and run: reproducible builds inside Docker producing a linux/amd64 binary in `dist/`, a
container image, and a Compose stack for development and small installs.

## ADDED Requirements

### Requirement: Containerized build
`make build` SHALL build the SPA and the Go binary inside Docker containers, needing only Docker and make on the host,
and SHALL write a single static `dist/meethub` executable for linux/amd64.

#### Scenario: Build on a clean host
- **GIVEN** a host with Docker and make but no Go or Node.js
- **WHEN** the developer runs `make build`
- **THEN** `dist/meethub` exists, is an ELF x86-64 static executable and `dist/meethub version` prints the git version

#### Scenario: Build with Docker only
- **GIVEN** a fresh clone on a host with Docker and nothing else (no make, Go or Node.js)
- **WHEN** the developer runs the single `docker build … --target export --output dist .` command documented in the
  README
- **THEN** the same `dist/meethub` is produced

### Requirement: Only linux/amd64 is produced
The build SHALL target linux/amd64 only; no other operating system or architecture is built, packaged or released.

#### Scenario: Single artifact
- **WHEN** `make build` finishes
- **THEN** `dist/` contains only the linux/amd64 binary and no other platform's build

### Requirement: Build outputs are never versioned
`dist/`, the built SPA in `web/dist` (except its placeholder), binaries, `config.toml`, `.env` files, databases and the
private `base/` folder SHALL be ignored by git.

#### Scenario: Ignored after a build
- **WHEN** `make build` runs and then `git status --porcelain` is checked
- **THEN** no file under `dist/`, `web/dist/` (besides `.gitkeep`), `base/` or any `*.db` is listed

### Requirement: Container image
`make image` SHALL build a minimal linux/amd64 image that runs `meethub server` as a non-root user, reads
`/etc/meethub/config.toml` and the `MEETHUB_*` environment, stores data in a `/data` volume and exposes port 8080 with
a health check on `/api/v1/health`.

#### Scenario: Image health
- **WHEN** the image starts with `MEETHUB_SESSION_SECRET` set and an empty `/data` volume
- **THEN** the container becomes healthy and serves the setup page on port 8080

### Requirement: Compose stack
The repository SHALL provide a Compose file that starts MeetHub with SQLite by default, Redis optional through a
profile, and MariaDB or PostgreSQL through mutually exclusive profiles, reading settings from `config.toml` and an
`.env` file kept out of git (`.env.example` documents it).

#### Scenario: Default stack
- **WHEN** the developer copies `.env.example` to `.env` and runs `make up`
- **THEN** only the `meethub` service starts, using SQLite on a named volume

#### Scenario: PostgreSQL profile
- **WHEN** the developer runs `make up PROFILE=postgres`
- **THEN** PostgreSQL starts and MeetHub connects to it instead of SQLite

### Requirement: Containerized checks and tests
`make check` and `make test` SHALL run formatting, linting, Go tests (with the race detector) and frontend tests inside
Docker, and `make test-db` SHALL run the database tests against SQLite, MariaDB and PostgreSQL containers.

#### Scenario: Database matrix
- **WHEN** the developer runs `make test-db`
- **THEN** the migration and repository tests pass on all three dialects and the containers are removed afterwards
