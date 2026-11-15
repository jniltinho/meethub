#!/bin/sh
# The settings applier under a real systemd (make test-settings-systemd): for Debian 13 and Ubuntu 26.04, a
# throwaway privileged container whose PID 1 is systemd runs packaging/settings-systemd-check.sh against the .deb in
# the packages directory. The containers are removed on exit (--rm, then docker stop). Needs only Docker on the host.
set -eu

dir=$(cd "${1:?usage: settings-systemd.sh <packages directory>}" && pwd)
here=$(cd "$(dirname "$0")" && pwd)
set -- "$dir"/meethub_*_amd64.deb
[ "$#" -eq 1 ] && [ -f "$1" ] || { echo "settings-systemd: want exactly one .deb in $dir" >&2; exit 1; }

IMAGES="
debian:13-slim@sha256:a72b4433de1cf7d6bae0c4cee74d0547f58601a643b3375f070c51600d63b6b2
ubuntu:26.04@sha256:065aa8626841f16057b78b027b464cd59c4fac36c204c72dcbf48d66f5fa8a96
"
name=""
cleanup() { [ -z "$name" ] || docker stop -t 5 "$name" >/dev/null 2>&1 || true; }
trap cleanup EXIT INT TERM

for image in $IMAGES; do
  name="meethub-settings-systemd-$$-$(echo "$image" | cut -d: -f1)"
  echo "settings-systemd: $image"
  docker run -d --rm --name "$name" --privileged --cgroupns=private --tmpfs /run --tmpfs /tmp \
    -v "$dir:/pkg:ro" -v "$here:/check:ro" "$image" sh -c 'export DEBIAN_FRONTEND=noninteractive;
      apt-get update -qq >/dev/null && apt-get install -y -qq systemd systemd-sysv curl procps >/dev/null 2>&1 &&
      exec /sbin/init' >/dev/null
  i=0
  until state=$(docker exec "$name" systemctl is-system-running 2>/dev/null) || [ "$state" = degraded ]; do
    i=$((i + 1))
    [ "$i" -le 180 ] || { echo "settings-systemd: systemd did not start in $image" >&2; exit 1; }
    sleep 1
  done
  docker exec "$name" sh /check/settings-systemd-check.sh
  docker stop -t 5 "$name" >/dev/null
  name=""
done
echo "settings-systemd: the applier works under systemd on every distribution"
