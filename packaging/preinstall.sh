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
# an older version has no applier: nothing to stop
systemctl stop meethub-settings.path meethub-settings.service 2>/dev/null || true
