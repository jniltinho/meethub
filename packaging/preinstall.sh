#!/bin/sh
# Before the files are replaced on an upgrade: stop the settings applier, so a change it is carrying out ends (it is
# undone) before its binary changes. Arguments: deb `install` or `upgrade [old version]`; rpm `1` (install) or `2`
# (upgrade).
set -e
case "${1:-}" in
upgrade | 2) ;;
*) exit 0 ;;
esac
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
# an older version has no applier: nothing to stop
settings_quiesce
