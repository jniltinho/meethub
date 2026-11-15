#!/bin/sh
# After the files are in place: the meethub user, the data directory, and the service enabled and started
# (restarted on an upgrade); the settings applier's directory and its units enabled (docs/admin-settings.md). Arguments: deb `configure [old version]`; rpm `1` (install) or `2` (upgrade).
set -e

fresh=no
case "${1:-}" in
configure) [ -n "${2:-}" ] || fresh=yes ;;
1 | abort-remove) fresh=yes ;; # abort-remove: dpkg gave up a removal after preremove.sh stopped the service
2) ;;
*) exit 0 ;; # deb: abort-upgrade, abort-deconfigure
esac
# installed again after a removal: dpkg still passes the old version, as on an upgrade, but preremove.sh had
# stopped and disabled the service
if [ -e /opt/meethub/.removed ]; then
  fresh=yes
  rm -f /opt/meethub/.removed
fi

getent group meethub >/dev/null || groupadd --system meethub
getent passwd meethub >/dev/null ||
  useradd --system --gid meethub --home-dir /opt/meethub --no-create-home --shell /usr/sbin/nologin \
    --comment MeetHub meethub

# the database, the uploaded files and the setup token: the service's own, never shipped and never deleted
install -d -o meethub -g meethub -m 0750 /opt/meethub/data
# the secrets: read by systemd as root and by the meethub CLI through the group, hidden from everyone else
chgrp meethub /opt/meethub/meethub.env
# the settings applier's own directory: written by root only, read by the service (set-group-id: its files are
# the meethub group's)
install -d -o root -g meethub -m 2750 /opt/meethub/settings-state

[ -d /run/systemd/system ] || exit 0 # no systemd here (a container, a chroot): nothing to enable
systemctl daemon-reload
if [ "$fresh" = yes ]; then
  systemctl enable --now meethub.service ||
    echo "meethub: the service did not start; see 'journalctl -u meethub' (it needs a Redis server)" >&2
else
  systemctl try-restart meethub.service || true
fi
# the applier runs at boot (to finish a change a stop cut short) and when the server leaves a request; a path unit
# that failed starts again here
systemctl enable meethub-settings.service meethub-settings.path >/dev/null 2>&1 || true
systemctl reset-failed meethub-settings.path meethub-settings.service >/dev/null 2>&1 || true
systemctl restart meethub-settings.path ||
  echo "meethub: the settings applier did not start; see 'systemctl status meethub-settings.path'" >&2
# one run now: it announces itself to the server (the page is read-only until it has), as it does at every boot
systemctl start --no-block meethub-settings.service || true
