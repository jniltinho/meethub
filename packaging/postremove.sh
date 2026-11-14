#!/bin/sh
# After a removal: systemd forgets the unit. The data and the user stay — deleting a team's messages and files is
# never a side effect of removing a package. Arguments: deb `remove` or `purge`; rpm `0` (removal).
set -e
case "${1:-}" in
remove | purge | 0) ;;
*) exit 0 ;;
esac
if [ -d /run/systemd/system ]; then
  systemctl daemon-reload || true
fi
if [ -d /opt/meethub/data ]; then
  echo "meethub: /opt/meethub/data (the database and the uploaded files) and the meethub user were kept"
fi
