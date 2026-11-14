# Tasks

## 1. Image

- [x] 1.1 Add the `allinone` target to `Dockerfile` (before `runtime`, on the Redis image pinned by digest, user
  65532, `/data` volume, port 8080, exec-form `HEALTHCHECK`) with `docker/allinone/supervisord.conf` and
  `docker/allinone/entrypoint.sh`; verify `docker build` without `--target` still produces the `runtime` image
- [x] 1.2 Add `make image-allinone` and `make test-allinone` at the end of `Makefile`; verify both appear in
  `make help`

## 2. Verification

- [x] 2.1 Write `docker/allinone/smoke.sh` (unwritable `/data` refused, healthy and ready on an empty volume as uid
  65532, owner account and Redis key kept across a new container, cleanup on failure); verify `make test-allinone`
  passes and `shellcheck -s sh` is clean
- [x] 2.2 Validate by hand the scenarios the smoke test does not cover: no process as root (`docker top`), port 6379
  refused from outside, Redis and the server restarted by the supervisor after `kill -9`, the server stopped before
  Redis on `docker stop` with exit status 0, the setup token in `docker logs`, a `chown`ed bind mount accepted

## 3. Publication and documentation

- [x] 3.1 Add `.github/workflows/docker-image-publish.yml` (build, smoke test, push to GHCR on `main` and `v*` tags
  only, actions pinned by commit, `packages: write` on the job); verify with `actionlint` and the pinned commits
  against the upstream tags
- [x] 3.2 Document the image in `docs/deployment.md` (`docker run` line, tags, port, volume, user, environment
  variables, upgrade); verify every command of the section against a running container
- [ ] 3.3 After the merge: push a `v*` tag, confirm the package appears on the repository page with the `allinone`
  tags and run the documented `docker run` line against the published image
