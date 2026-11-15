#!/bin/sh
# Smoke test of the all-in-one image (make test-allinone; the publish workflow runs it before pushing):
#   1. a root-owned /data is refused with a clear message
#   2. an empty named volume → healthy, /api/v1/ready passes every check, nothing runs as root
#   3. meetings: LiveKit runs, /api/v1/meetings reports them enabled (and Administration → Settings is read-only), the key files are private and
#      `meethub meetings check` passes, says so for a wrong secret and an unreachable LiveKit, and never prints the secret
#   4. recordings: the image's ffmpeg has no GPL or non-free part and composes both formats as uid 65532
#   5. the container stops cleanly, and the owner account, a Redis key and the LiveKit key and secret survive it
#      being deleted and created again
#   6. MEETHUB_SETTINGS_APPLIER=on: Administration → Settings is editable; a change is applied through a restart, kept,
#      written to /data/config/config.toml, nothing runs as root; stopped while another change waits for its
#      confirmation, the container stops cleanly and the file is back as it was
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

start() { # start [docker run options]
  docker run -d --name "$name" -v "$name:/data" "$@" "$image" >/dev/null
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

# every process of the container, LiveKit's among them, runs as uid 65532 (real and effective)
all_unprivileged() {
  uids=$(docker exec "$name" sh -c 'for f in /proc/[0-9]*/status; do awk "/^Uid:/ {print \$2; print \$3}" "$f"; done' |
    sort -u | tr '\n' ' ')
  [ "$uids" = "65532 " ] || fail "processes run as uids $uids"
}

start
[ "$(docker exec "$name" id -u)" = 65532 ] || fail "the container does not run as uid 65532"
docker exec "$name" supervisorctl status livekit | grep -q RUNNING || fail "LiveKit is not running"
all_unprivileged

# throwaway credentials of a throwaway container
password="Smoke-$(od -An -N8 -tx1 /dev/urandom | tr -d ' \n')-A1!"
printf '%s\n' "$password" | docker exec -i "$name" meethub user create --org-name Smoke --email smoke@example.com \
  --name 'Smoke Test' --role owner --password-stdin >/dev/null || fail "could not create the owner"
docker exec "$name" redis-cli set smoke:key kept >/dev/null || fail "could not write to Redis"

# meetings enabled, as a signed-in browser sees them
cookie=$(docker exec "$name" wget -S -qO /dev/null --header 'Content-Type: application/json' \
  --header 'Origin: http://localhost:8080' --post-data "{\"email\":\"smoke@example.com\",\"password\":\"$password\"}" \
  http://127.0.0.1:8080/api/v1/auth/login 2>&1 | sed -n 's/^ *[Ss]et-[Cc]ookie: \([^;]*\).*/\1/p' | head -n 1)
[ -n "$cookie" ] || fail "could not sign in"
docker exec "$name" wget -qO- --header "Cookie: $cookie" http://127.0.0.1:8080/api/v1/meetings |
  grep -q '"enabled":true' || fail "/api/v1/meetings does not report meetings enabled"
# Administration → Settings is read-only: configured by variables, no settings applier (add-admin-settings)
docker exec "$name" wget -qO- --header "Cookie: $cookie" http://127.0.0.1:8080/api/v1/admin/settings |
  grep -q '"applier":{"available":false' || fail "the settings page is not read-only in the all-in-one image"
modes=$(docker exec "$name" stat -c '%a %u' /data/livekit /data/livekit/api-key /data/livekit/api-secret \
  /data/livekit/keys.yaml | sort -u | tr '\n' ' ')
[ "$modes" = "600 65532 700 65532 " ] || fail "the LiveKit key files are not private to uid 65532: $modes"

# recordings: the image's own ffmpeg, LGPL only, composes an .m4a and a .webm as the container's user
! docker exec "$name" ffmpeg -hide_banner -buildconf | grep -Eq -- '--enable-(gpl|nonfree)' ||
  fail "the image's ffmpeg was built with GPL or non-free parts"
docker exec "$name" meethub recordings probe --mode all >/dev/null || fail "the recording probe failed in the image"

# meethub meetings check: all passing, a wrong secret, an unreachable LiveKit; the secret is never printed
secret=$(docker exec "$name" cat /data/livekit/api-secret)
check() { docker exec "$@" "$name" meethub meetings check 2>&1; }
out0=$(check) || fail "meetings check failed: $out0"
# an explicit variable replaces the generated file (no "both set" refusal)
wrong=$(od -An -tx1 -N32 /dev/urandom | tr -d ' \n')
if out=$(check -e MEETHUB_LIVEKIT_API_SECRET="$wrong"); then fail "meetings check passed with a wrong secret"; fi
echo "$out" | grep -q 'the key was refused' || fail "meetings check did not report the wrong secret: $out"
if out2=$(check -e MEETHUB_LIVEKIT_API_URL=http://127.0.0.1:1); then fail "meetings check passed with LiveKit unreachable"; fi
echo "$out2" | grep -q 'unreachable' || fail "meetings check did not report LiveKit unreachable: $out2"
out3=$(docker exec "$name" meethub meetings check --json 2>&1) || fail "meetings check --json failed: $out3"
echo "$out3" | grep -q '"ok": true' || fail "meetings check --json does not report ok: $out3"
case "$out0$out$out2$out3" in *"$secret"*) fail "meetings check printed the secret" ;; esac
echo "smoke: meetings enabled, meetings check ok"

docker stop -t 55 "$name" >/dev/null
[ "$(docker inspect -f '{{.State.ExitCode}}' "$name")" = 0 ] || fail "the container did not stop cleanly"
docker rm "$name" >/dev/null
start

docker exec "$name" meethub user list --json | grep -q smoke@example.com || fail "the database did not survive"
[ "$(docker exec "$name" redis-cli get smoke:key)" = kept ] || fail "the Redis data did not survive"
[ "$(docker exec "$name" cat /data/livekit/api-secret)" = "$secret" ] || fail "the LiveKit secret did not survive"
out=$(check) || fail "meetings check failed in the new container: $out"
all_unprivileged
echo "smoke: data kept across a new container — ok"

# opt-in: the server under the settings applier, on the same volume
docker stop -t 55 "$name" >/dev/null
docker rm "$name" >/dev/null
start -e MEETHUB_SETTINGS_APPLIER=on
# settings_change <key> <json value> <keep|leave>: an owner changes one setting from the page and keeps it, or
# leaves it waiting for the confirmation
settings_change() {
  docker exec -i -e SMOKE_PASSWORD="$password" -e SMOKE_KEY="$1" -e SMOKE_VALUE="$2" -e SMOKE_END="$3" "$name" python3 - <<'PY'
import http.cookiejar, json, os, sys, time, urllib.request

opener = urllib.request.build_opener(urllib.request.HTTPCookieProcessor(http.cookiejar.CookieJar()))
csrf = ""


def api(method, path, body=None):
    req = urllib.request.Request(
        "http://127.0.0.1:8080/api/v1" + path,
        method=method,
        data=None if body is None else json.dumps(body).encode(),
        headers={"Origin": "http://localhost:8080", "Content-Type": "application/json", "X-CSRF-Token": csrf},
    )
    with opener.open(req, timeout=10) as r:
        text = r.read()
        return json.loads(text) if text else None


def settings(what, cond, seconds):
    for _ in range(seconds):
        try:
            v = api("GET", "/admin/settings")
            if cond(v):
                return v
        except OSError:
            pass  # the server is restarting
        time.sleep(1)
    sys.exit("smoke: settings: no " + what)


api("POST", "/auth/login", {"email": "smoke@example.com", "password": os.environ["SMOKE_PASSWORD"]})
csrf = api("GET", "/me")["csrf_token"]
if not api("GET", "/admin/settings")["applier"]["available"]:
    sys.exit("smoke: settings: the page is read-only")
api("PUT", "/admin/settings", {"changes": {os.environ["SMOKE_KEY"]: json.loads(os.environ["SMOKE_VALUE"])}})
v = settings("restart with the change", lambda v: (v.get("pending") or {}).get("phase") == "confirming", 90)
if os.environ["SMOKE_END"] == "keep":
    api("POST", "/admin/settings/confirm", {"id": v["pending"]["id"]})
    settings("applied result", lambda v: not v.get("pending") and (v.get("last_result") or {}).get("outcome") == "applied", 60)
PY
}
settings_change files.max_per_post 20 keep || fail "the settings page did not apply a change under MEETHUB_SETTINGS_APPLIER=on"
docker exec "$name" grep -q '^max_per_post = 20$' /data/config/config.toml || fail "the change is not in /data/config/config.toml"
# the health check and the CLI read that file
docker exec "$name" meethub healthcheck || fail "the health check fails under the settings applier"
all_unprivileged
# stopped while a change waits for its confirmation: the previous file is back, the stop is clean and in time
settings_change log.level '"warn"' leave || fail "the second change did not reach its confirmation"
docker exec "$name" grep -q "^level = 'warn'$" /data/config/config.toml || fail "the second change is not in the file"
docker stop -t 55 "$name" >/dev/null
[ "$(docker inspect -f '{{.State.ExitCode}}' "$name")" = 0 ] || fail "the container did not stop cleanly under the settings applier"
docker rm "$name" >/dev/null
left=$(docker run --rm -v "$name:/data" "$image" cat /data/config/config.toml) || fail "could not read the file left in the volume"
case "$left" in *"level = 'warn'"*) fail "a change nobody kept survived the stop" ;; esac
case "$left" in *"max_per_post = 20"*) ;; *) fail "the kept change did not survive the stop" ;; esac
echo "smoke: MEETHUB_SETTINGS_APPLIER=on — a change applied and kept, one undone by the stop, a clean stop — ok"
