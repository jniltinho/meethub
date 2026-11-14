#!/bin/sh
# Smoke test of the Linux packages (make test-package; the packages workflow runs it before a release). In a clean
# container of each distribution:
#   1. the package installs with the distribution's own tool, which also brings Redis (or Valkey)
#   2. the files, their owners and modes, the meethub user and the systemd unit (systemd-analyze verify) are right
#   3. the server starts as the meethub user from /opt/meethub, as the unit starts it, and /api/v1/ready passes
#      every check (database, migrations, Redis, file storage)
#      — over plain HTTP, then over its own HTTPS with a generated certificate (http.self_signed)
#   4. removing the package (and purging it, on Debian and Ubuntu) keeps /opt/meethub/data
# systemd does not run in these containers: the unit is verified, not started, and the scripts' systemctl calls are
# skipped. Needs only Docker on the host.
set -eu

dir=$(cd "${1:?usage: smoke.sh <packages directory>}" && pwd)
# exactly one package of each kind: two versions in the directory would install the wrong one
for kind in 'meethub_*_amd64.deb' 'meethub-*.x86_64.rpm'; do
  # shellcheck disable=SC2086 # the pattern must expand
  set -- "$dir"/$kind
  [ "$#" -eq 1 ] && [ -f "$1" ] || { echo "smoke: want exactly one $kind in $dir" >&2; exit 1; }
done

DEB_IMAGES="
debian:13-slim@sha256:a72b4433de1cf7d6bae0c4cee74d0547f58601a643b3375f070c51600d63b6b2
ubuntu:26.04@sha256:065aa8626841f16057b78b027b464cd59c4fac36c204c72dcbf48d66f5fa8a96
"
RPM_IMAGES="
almalinux:9@sha256:245f8aab832fe800aa42bdab5fad75ebbd8e0b4c9f24820a83c638b177857ae2
almalinux:10@sha256:5a4b41a427c9717be8f4ac2a32aa22efbae5f9124fbcb79a6765394f312acfe8
"

run() { # run <deb|rpm> <image>
  echo "smoke: $2"
  docker run --rm -i -v "$dir:/pkg:ro" "$2" sh -s "$1" <<'INSIDE'
set -eu
kind=$1
fail() {
  echo "smoke: $*" >&2
  [ ! -f /tmp/meethub.log ] || tail -n 30 /tmp/meethub.log >&2
  exit 1
}
is() { [ "$(stat -c '%U:%G %a' "$1")" = "$2" ] || fail "$1 is $(stat -c '%U:%G %a' "$1"), want $2"; }

if [ "$kind" = deb ]; then
  export DEBIAN_FRONTEND=noninteractive
  apt-get update -qq >/dev/null
  apt-get install -y -qq /pkg/meethub_*_amd64.deb systemd curl >/tmp/install.log 2>&1 ||
    { tail -n 20 /tmp/install.log >&2; fail "apt could not install the package"; }
else
  # util-linux: setpriv, which the smallest base images leave out
  dnf install -y -q /pkg/meethub-*.x86_64.rpm util-linux >/tmp/install.log 2>&1 ||
    { tail -n 20 /tmp/install.log >&2; fail "dnf could not install the package"; }
fi

is /opt/meethub/bin/meethub root:root\ 755
is /opt/meethub/config.toml root:root\ 644
is /opt/meethub/meethub.env root:meethub\ 640
is /opt/meethub/data meethub:meethub\ 750
/opt/meethub/bin/meethub version | grep -q '^meethub ' || fail "meethub version does not answer"
[ "$(id -gn meethub)" = meethub ] || fail "no meethub user in the meethub group"
systemd-analyze verify /usr/lib/systemd/system/meethub.service >/tmp/unit.log 2>&1 ||
  { cat /tmp/unit.log >&2; fail "systemd-analyze refuses the unit"; }
! grep -q meethub.service /tmp/unit.log || { cat /tmp/unit.log >&2; fail "systemd-analyze warns about the unit"; }

redis=$(command -v redis-server || command -v valkey-server) || fail "the package brought no Redis or Valkey"
"$redis" --daemonize yes --save '' --appendonly no >/dev/null

# as the unit: the meethub user, /opt/meethub as the working directory, its umask, the secrets file in the environment
cd /opt/meethub
umask 0027
set -a
. ./meethub.env
set +a
setpriv --reuid meethub --regid meethub --init-groups ./bin/meethub server >/tmp/meethub.log 2>&1 &
server=$!
i=0
until body=$(curl -fsS http://127.0.0.1:8080/api/v1/ready 2>/dev/null); do
  i=$((i + 1))
  [ "$i" -le 60 ] || fail "the server is not ready after 60 seconds"
  kill -0 "$server" 2>/dev/null || fail "the server exited"
  sleep 1
done
is /opt/meethub/data/meethub.db meethub:meethub\ 640
kill "$server"
wait "$server" || fail "the server did not stop cleanly"

# the server's own HTTPS with the certificate it generates: checked against that certificate, not merely accepted
MEETHUB_HTTP_SELF_SIGNED=true MEETHUB_HTTP_PUBLIC_URL=https://localhost:8080 \
  setpriv --reuid meethub --regid meethub --init-groups ./bin/meethub server >/tmp/meethub.log 2>&1 &
server=$!
i=0
until curl -fsS --cacert /opt/meethub/data/tls/selfsigned.crt https://localhost:8080/api/v1/ready >/dev/null 2>&1; do
  i=$((i + 1))
  [ "$i" -le 60 ] || fail "the server is not ready over HTTPS after 60 seconds"
  kill -0 "$server" 2>/dev/null || fail "the server exited with http.self_signed"
  sleep 1
done
is /opt/meethub/data/tls/selfsigned.key meethub:meethub\ 600
kill "$server"
wait "$server" || fail "the server did not stop cleanly after serving HTTPS"
cd /

if [ "$kind" = deb ]; then
  apt-get remove -y -qq meethub >/dev/null 2>&1 || fail "apt could not remove the package"
  [ -f /opt/meethub/config.toml ] || fail "the removal deleted config.toml (only a purge may)"
  apt-get purge -y -qq meethub >/dev/null 2>&1 || fail "apt could not purge the package"
  [ ! -e /opt/meethub/config.toml ] || fail "the purge left config.toml"
else
  rpm -e meethub >/dev/null || fail "rpm could not remove the package"
fi
[ ! -e /opt/meethub/bin/meethub ] || fail "the removal left the binary"
[ ! -e /usr/lib/systemd/system/meethub.service ] || fail "the removal left the unit"
[ -f /opt/meethub/data/meethub.db ] || fail "the removal deleted the data"
echo "smoke: ok ($($redis --version | cut -d' ' -f1-3)), ready $body"
INSIDE
}

for image in $DEB_IMAGES; do run deb "$image"; done
for image in $RPM_IMAGES; do run rpm "$image"; done
echo "smoke: the packages install, start and remove cleanly on every distribution"
