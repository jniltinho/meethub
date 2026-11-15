#!/bin/sh
# MeetHub all-in-one (docs/deployment.md): checks the /data volume, creates the LiveKit key and secret on the first
# start, then supervisord runs Redis, LiveKit and the meethub server. The container never runs as root (uid 65532),
# so /data must already belong to that user: a named volume takes its owner from the image, a bind-mounted host
# directory keeps the owner it has on the host.
#
#   docker run … <image>                    start Redis, LiveKit and the server
#   docker run -e MEETHUB_SETTINGS_APPLIER=on … <image>   the same, with Administration → Settings editable
#   docker run … <image> meethub version    run one command instead (no Redis, no LiveKit, no server)
set -eu

# livekit_keys creates /data/livekit/api-key and api-secret once (/usr/local/bin/meethub hands them to the server
# and the CLI) and writes LiveKit's key file from them; everything is readable only by this user.
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

# MEETHUB_SETTINGS_APPLIER=on (opt-in, docs/admin-settings.md): Administration → Settings is editable. The settings
# applier runs the server as its child and applies the changes to /data/config/config.toml, which starts as a
# copy of the config.toml in use (/data/config.toml or the one mounted at /etc/meethub), or empty — the variables
# still win over it. The applier is this container's only user, as everything here.
ALLINONE_SERVER=server
case "${MEETHUB_SETTINGS_APPLIER:-off}" in
on) ALLINONE_SERVER="--config /data/config/config.toml settings-applier --supervise" ;;
off) ;;
*)
  echo "meethub: MEETHUB_SETTINGS_APPLIER must be on or off" >&2
  exit 1
  ;;
esac
export ALLINONE_SERVER

exec supervisord -c /etc/supervisord.conf
