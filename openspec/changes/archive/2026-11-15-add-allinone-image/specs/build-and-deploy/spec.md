## ADDED Requirements

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
