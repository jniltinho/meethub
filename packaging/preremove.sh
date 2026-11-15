#!/bin/sh
# Before the files go away: stop and disable the service and the settings applier (a change it is carrying out is
# undone first), on a removal only. Arguments: deb `remove` (also
# `upgrade`, `deconfigure`…); rpm `0` (removal) or `1` (upgrade).
set -e
case "${1:-}" in
remove | 0) ;;
*) exit 0 ;;
esac
# tells postinstall.sh, should the package come back, that the service has to be enabled and started again
touch /opt/meethub/.removed 2>/dev/null || true
[ -d /run/systemd/system ] || exit 0
# stop the settings applier (a change it is carrying out is undone), then finish or undo one a kill or a stop left
# unfinished: the binary must not change under a change in progress
settings_quiesce() {
  systemctl stop meethub-settings.path meethub-settings.service 2>/dev/null || true
  [ -x /opt/meethub/bin/meethub ] && [ -e /opt/meethub/settings-state/state.json ] || return 0
  (
    cd /opt/meethub && set -a && { [ ! -f meethub.env ] || . ./meethub.env; } && set +a &&
      ./bin/meethub settings-applier --recover --config /opt/meethub/config.toml \
        --requests /opt/meethub/data/settings --state /opt/meethub/settings-state
  ) || {
    echo "meethub: a change from Administration → Settings could not be undone; see docs/admin-settings.md" >&2
    return 1
  }
}
settings_quiesce
systemctl disable meethub-settings.path meethub-settings.service 2>/dev/null || true
systemctl disable --now meethub.service || true
