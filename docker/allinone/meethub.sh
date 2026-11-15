#!/bin/sh
# /usr/local/bin/meethub of the all-in-one image: the server, the CLI (docker exec) and the health check get the
# LiveKit key and secret the entrypoint created in /data/livekit, unless the variable or its _FILE form is given —
# both set would be refused, so an explicit MEETHUB_LIVEKIT_API_KEY / _SECRET (an external LiveKit) still works.
[ -n "${MEETHUB_LIVEKIT_API_KEY+x}${MEETHUB_LIVEKIT_API_KEY_FILE+x}" ] ||
  export MEETHUB_LIVEKIT_API_KEY_FILE=/data/livekit/api-key
[ -n "${MEETHUB_LIVEKIT_API_SECRET+x}${MEETHUB_LIVEKIT_API_SECRET_FILE+x}" ] ||
  export MEETHUB_LIVEKIT_API_SECRET_FILE=/data/livekit/api-secret
# with the settings applier on (MEETHUB_SETTINGS_APPLIER=on), the health check and the CLI read the file the server
# runs with, unless they are given another
if [ "${MEETHUB_SETTINGS_APPLIER:-off}" = on ] && [ -f /data/config/config.toml ]; then
  case " $* " in
  *" --config"*) ;;
  *) set -- --config /data/config/config.toml "$@" ;;
  esac
fi
exec /usr/local/libexec/meethub "$@"
