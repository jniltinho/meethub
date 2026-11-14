#!/bin/sh
# MeetHub all-in-one (docs/deployment.md): checks the /data volume, creates the LiveKit key and secret on the first
# start, then supervisord runs Redis, LiveKit and the meethub server. The container never runs as root (uid 65532),
# so /data must already belong to that user: a named volume takes its owner from the image, a bind-mounted host
# directory keeps the owner it has on the host.
#
#   docker run … <image>                    start Redis, LiveKit and the server
#   docker run … <image> meethub version    run one command instead (no Redis, no LiveKit, no server)
set -eu

# livekit_keys creates /data/livekit/api-key and api-secret once (the server reads them through
# MEETHUB_LIVEKIT_API_*_FILE) and writes LiveKit's key file from them; everything is readable only by this user.
livekit_keys() ( # a subshell: the umask stays here
  d=/data/livekit
  umask 077
  mkdir -p "$d"
  chmod 700 "$d"
  if ! [ -s "$d/api-key" ] || ! [ -s "$d/api-secret" ]; then
    printf 'API%s\n' "$(od -An -tx1 -N8 /dev/urandom | tr -d ' \n')" >"$d/api-key.tmp"
    od -An -tx1 -N32 /dev/urandom | tr -d ' \n' >"$d/api-secret.tmp"
    mv "$d/api-secret.tmp" "$d/api-secret"
    mv "$d/api-key.tmp" "$d/api-key"
    echo "meethub: created the LiveKit key and secret in $d"
  fi
  printf '%s: %s\n' "$(cat "$d/api-key")" "$(cat "$d/api-secret")" >"$d/keys.yaml.tmp"
  mv "$d/keys.yaml.tmp" "$d/keys.yaml"
)

if [ $# -gt 0 ]; then
  livekit_keys >/dev/null 2>&1 || true # on an unwritable /data, commands that load the configuration then fail
  exec "$@"
fi

if ! mkdir -p /data/redis 2>/dev/null || ! [ -w /data ] || ! [ -w /data/redis ]; then
  echo "meethub: /data is not writable by uid $(id -u): use a named volume (-v meethub-data:/data)" \
    "or give the host directory to that user (chown -R $(id -u):$(id -g) <dir>)" >&2
  exit 1
fi
livekit_keys

# LiveKit's settings (no secret: the key file holds the pair). LIVEKIT_NODE_IP is the address browsers reach the
# media ports at; TURN over UDP may relay only to it, on LiveKit's own media ports.
LIVEKIT_CONFIG=$(cat <<EOF
port: 7880
key_file: /data/livekit/keys.yaml
rtc:
  tcp_port: 7881
  udp_port: 7882
  use_external_ip: false
turn:
  enabled: true
  udp_port: 3478
  allow_restricted_peer_cidrs: ["${LIVEKIT_NODE_IP}/32"]
webhook:
  api_key: "$(cat /data/livekit/api-key)"
  urls: ["http://127.0.0.1:8080/api/v1/meetings/webhook"]
room:
  empty_timeout: 300
EOF
)
export LIVEKIT_CONFIG

exec supervisord -c /etc/supervisord.conf
