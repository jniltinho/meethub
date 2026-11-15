# Design

## Context

The server needs Redis (jobs, rate limits, presence, the chat event bus), so the smallest install today is the
Compose stack. The author's go-guacamole ships an all-in-one image (`docker/allinone`: supervisord, embedded Redis,
one `/data`) published to GHCR by `docker-image-publish.yml`; this change brings the same package to MeetHub,
following this repository's own conventions where the two differ.

## Decisions

### D1. A Dockerfile target on the Compose stack's Redis image

`allinone` is a target of the root `Dockerfile`, placed before `runtime` so the default target does not change, and
reuses the `frontend` and `build` stages: one binary, one cache. Its base is the Redis image already pinned by digest
in `deployments/compose` (`redis:8.2-alpine`), so Redis is the same build in both installs and is upgraded in one
place; `supervisor` comes from that Alpine release. Alternative considered: Alpine plus the `redis` package — a
different Redis from the one the tests and the Compose stack run.

### D2. Never root, unlike the reference

go-guacamole starts as root to `chown` a bind-mounted `./data` and then drops each program to uid 1000. Here the
image declares `USER 65532:65532` (the uid of the `runtime` image, so a `/data` volume moves between the two) and
supervisord itself runs unprivileged: no process can ever be root, the container works with dropped capabilities, and
there is no privilege-dropping code to get wrong. The cost is that a bind-mounted host directory must already belong
to 65532; the entrypoint checks `/data` first and exits with the exact `chown` to run, and the documentation
recommends a named volume, which takes its owner from the image.

### D3. Redis: loopback, append-only file in the volume

Redis binds `127.0.0.1` with protected mode on and no password — nothing outside the container's network namespace
can reach it — and persists like the Compose stack (`--appendonly yes`) in `/data/redis`, so queued and scheduled jobs
survive a restart. There is no switch for an external Redis: that install is the `runtime` image.

### D4. Supervision

supervisord is PID 1 (it forwards signals and reaps children), logs every program to the container's output, restarts
a program that exits, and stops the server (priority 20) before Redis (priority 10), each with 20 seconds — above the
server's default `http.shutdown_timeout` of 15 seconds; the two add up to 40 seconds, so the documented
`docker run` line passes `--stop-timeout 45` (Docker's default of 10 would cut a slow shutdown short). The server already waits for Redis (`redis.connect_timeout`), so no start ordering script is
needed; `startretries=10` covers a slow first start. `supervisorctl` works through a socket private to the user.
The health check is the existing `meethub healthcheck` (liveness); readiness stays on `/api/v1/ready`.

### D5. Smoke test as the gate

`docker/allinone/smoke.sh` (POSIX sh, Docker only) is what `make test-allinone` and the workflow run: an unwritable
`/data` is refused; an empty volume becomes healthy and ready as uid 65532; the container stops with status 0; the
owner account and a Redis key survive it being deleted and created again. Every wait is a bounded loop on a detached
container, so a broken image fails the test instead of hanging it. The workflow builds once with `load`, runs the smoke test,
then builds again from the builder's cache to push — the same layers that were tested.

### D6. Workflow and tags

Triggers follow the reference: pull requests build and test only; pushes to `main` and `v*` tags publish with the
workflow's `GITHUB_TOKEN` (`packages: write` on the job only). Tags carry an `-allinone` suffix (`allinone` for the
latest release), exactly as in go-guacamole, leaving the plain tags for the runtime image if it is published later.
Actions are pinned by commit. Release runs share one concurrency group, so two tags pushed close together publish
in order and the older one cannot finish last and take `allinone` back. The workflow does not call `ci.yml`: that file cancels in-progress runs per ref, so a
called copy and the push-triggered one would cancel each other; lint and tests keep running there in parallel, and
release tags are cut from commits of `main` that passed.

## Risks / Trade-offs

- **`allinone` follows the last release tag pushed**, not the highest version: a patch for an older line (`v1.1.5`
  after `v1.2.0`) moves it back, as in go-guacamole. Three tags pushed within one build also leave the middle run
  cancelled by GitHub's queue (re-run it).

- **Bind mounts need a `chown`** (D2) — mitigated by the entrypoint's message and the documentation.
- **A program that keeps failing leaves the container running** (supervisord gives up after its retries): the health
  check turns the container unhealthy and the log shows why; an orchestrator restart policy on health is the
  operator's choice.
- **`supervisor` is not pinned to a package revision**: Alpine removes old revisions from a release, so a pin would
  break rebuilds; the base digest fixes the Alpine release.
- **The base image declares port 6379 and a `/data` volume**: the port answers nobody (D3); the volume is the one
  this image uses.
