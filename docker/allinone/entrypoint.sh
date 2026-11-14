#!/bin/sh
# MeetHub all-in-one (docs/deployment.md): checks the /data volume, then supervisord runs Redis and the meethub
# server. The container never runs as root (uid 65532), so /data must already belong to that user: a named volume
# takes its owner from the image, a bind-mounted host directory keeps the owner it has on the host.
#
#   docker run … <image>                    start Redis and the server
#   docker run … <image> meethub version    run one command instead (no Redis, no server)
set -eu

[ $# -eq 0 ] || exec "$@"

if ! mkdir -p /data/redis 2>/dev/null || ! [ -w /data ] || ! [ -w /data/redis ]; then
  echo "meethub: /data is not writable by uid $(id -u): use a named volume (-v meethub-data:/data)" \
    "or give the host directory to that user (chown -R $(id -u):$(id -g) <dir>)" >&2
  exit 1
fi

exec supervisord -c /etc/supervisord.conf
