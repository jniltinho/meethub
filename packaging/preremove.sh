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
systemctl disable --now meethub-settings.path meethub-settings.service 2>/dev/null || true
systemctl disable --now meethub.service || true
