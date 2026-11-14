#!/bin/sh
# Smoke test of the all-in-one image (make test-allinone; the publish workflow runs it before pushing):
#   1. a root-owned /data is refused with a clear message
#   2. an empty named volume → healthy, /api/v1/ready passes every check, nothing runs as root
#   3. the container stops cleanly, and the owner account and a Redis key survive it being deleted and created again
# Needs only Docker on the host; the container and the volume are deleted afterwards, also on failure.
set -eu

image="${1:?usage: smoke.sh <image>}"
name="meethub-allinone-smoke-$$"

cleanup() {
  docker rm -f "$name" >/dev/null 2>&1 || true
  docker volume rm "$name" >/dev/null 2>&1 || true
}
trap cleanup EXIT
trap 'exit 1' INT TERM

fail() {
  echo "smoke: $*" >&2
  docker logs "$name" 2>&1 | tail -n 40 >&2 || true
  exit 1
}

start() {
  docker run -d --name "$name" -v "$name:/data" "$image" >/dev/null
  i=0
  while [ "$(docker inspect -f '{{.State.Health.Status}}' "$name")" != healthy ]; do
    i=$((i + 1))
    [ "$i" -le 90 ] || fail "the container is not healthy after 90 seconds"
    sleep 1
  done
  # 200 only when every check (database, migrations, Redis) passes; wget fails on 503
  body=$(docker exec "$name" wget -qO- http://127.0.0.1:8080/api/v1/ready) || fail "/api/v1/ready is not 200"
  echo "smoke: ready $body"
}

docker run -d --name "$name" --tmpfs /data:mode=0755 "$image" >/dev/null
i=0
while [ "$(docker inspect -f '{{.State.Running}}' "$name")" = true ]; do
  i=$((i + 1))
  [ "$i" -le 30 ] || fail "a /data owned by root was accepted: the container is still running"
  sleep 1
done
[ "$(docker inspect -f '{{.State.ExitCode}}' "$name")" != 0 ] || fail "exit status 0 with an unwritable /data"
docker logs "$name" 2>&1 | grep -q 'not writable' || fail "no message about the unwritable /data"
docker rm "$name" >/dev/null
echo "smoke: unwritable /data refused"

start
[ "$(docker exec "$name" id -u)" = 65532 ] || fail "the container does not run as uid 65532"

# throwaway credentials of a throwaway container
password="Smoke-$(od -An -N8 -tx1 /dev/urandom | tr -d ' \n')-A1!"
printf '%s\n' "$password" | docker exec -i "$name" meethub user create --org-name Smoke --email smoke@example.com \
  --name 'Smoke Test' --role owner --password-stdin >/dev/null || fail "could not create the owner"
docker exec "$name" redis-cli set smoke:key kept >/dev/null || fail "could not write to Redis"

docker stop -t 45 "$name" >/dev/null
[ "$(docker inspect -f '{{.State.ExitCode}}' "$name")" = 0 ] || fail "the container did not stop cleanly"
docker rm "$name" >/dev/null
start

docker exec "$name" meethub user list --json | grep -q smoke@example.com || fail "the database did not survive"
[ "$(docker exec "$name" redis-cli get smoke:key)" = kept ] || fail "the Redis data did not survive"
echo "smoke: data kept across a new container — ok"
