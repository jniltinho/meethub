#!/bin/sh
# After the files are in place: the meethub user, the data directory, and the service enabled and started
# (restarted on an upgrade). Arguments: deb `configure [old version]`; rpm `1` (install) or `2` (upgrade).
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

[ -d /run/systemd/system ] || exit 0 # no systemd here (a container, a chroot): nothing to enable
systemctl daemon-reload
if [ "$fresh" = yes ]; then
  systemctl enable --now meethub.service ||
    echo "meethub: the service did not start; see 'journalctl -u meethub' (it needs a Redis server)" >&2
else
  systemctl try-restart meethub.service || true
fi
