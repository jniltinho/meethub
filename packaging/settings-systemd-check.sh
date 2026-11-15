#!/bin/sh
# The settings applier under a real systemd (add-admin-settings, task 4.1), inside a throwaway container whose PID 1
# is systemd (packaging/settings-systemd.sh starts it): the .deb installed with its units, then through the API as an
# owner would — a change applied and kept, one not confirmed and rolled back, a stop in the middle followed by the
# boot-time run, a package upgrade during a change — and the service unable to write config.toml or the applier's
# directory. Run as root; needs the .deb in /pkg.
set -eu

fail() {
  echo "settings-systemd: $*" >&2
  journalctl -u meethub-settings.service -u meethub.service --no-pager -n 40 >&2 || true
  exit 1
}
api() { # api <method> <path> [json]
  curl -sS -b /tmp/jar -c /tmp/jar -H 'Origin: http://localhost:8080' -H 'Content-Type: application/json' \
    -H "X-CSRF-Token: ${csrf:-}" -X "$1" ${3:+--data "$3"} "http://127.0.0.1:8080/api/v1$2"
}
ready() {
  i=0
  until curl -fsS http://127.0.0.1:8080/api/v1/ready >/dev/null 2>&1; do
    i=$((i + 1))
    [ "$i" -le 90 ] || fail "the server is not ready"
    sleep 1
  done
}
# wait_for <jq condition on GET /admin/settings> <seconds>
wait_for() {
  i=0
  until api GET /admin/settings 2>/dev/null | jq -e "$1" >/dev/null 2>&1; do
    i=$((i + 1))
    [ "$i" -le "$2" ] || { api GET /admin/settings | jq -c '{pending, last_result, applier}' >&2; fail "timed out waiting for $1"; }
    sleep 1
  done
}
value() { sed -n "s/^$1 = \\([^ ]*\\).*/\\1/p" /opt/meethub/config.toml; }

export DEBIAN_FRONTEND=noninteractive
apt-get install -y -qq jq >/dev/null 2>&1
apt-get install -y -qq /pkg/meethub_*_amd64.deb >/tmp/install.log 2>&1 || { tail -n 20 /tmp/install.log; fail "install"; }
systemctl start redis-server
systemctl restart meethub.service
ready
systemctl is-enabled meethub-settings.path meethub-settings.service >/dev/null || fail "the applier's units are not enabled"
systemctl is-active meethub-settings.path >/dev/null || fail "the path unit is not active"
for u in meethub-settings.service meethub-settings.path; do
  systemd-analyze verify "/usr/lib/systemd/system/$u" || fail "systemd-analyze refuses $u"
done
echo "settings-systemd: $(systemd-analyze security meethub-settings.service 2>/dev/null | tail -n 1)"

# the owner, from the CLI as the service's user, and a browser session
cd /opt/meethub
set -a
. ./meethub.env
set +a
printf '%s\n' 'a long password!' | setpriv --reuid meethub --regid meethub --init-groups ./bin/meethub user create \
  --email ana@example.com --name 'Ana Silva' --role owner --org-name Acme --password-stdin >/dev/null
cd /
api POST /auth/login '{"email":"ana@example.com","password":"a long password!"}' >/dev/null
csrf=$(api GET /me | jq -r .csrf_token)
[ -n "$csrf" ] && [ "$csrf" != null ] || fail "no session"
wait_for '.applier.available' 30

# the service's user cannot write config.toml or anything the applier writes
for target in /opt/meethub/config.toml /opt/meethub/settings-state/state.json /opt/meethub/settings-state/x; do
  ! setpriv --reuid meethub --regid meethub --init-groups sh -c "echo x >> $target" 2>/dev/null ||
    fail "the service's user can write $target"
done

echo "settings-systemd: 1. applied and kept"
before=$(sha256sum /opt/meethub/config.toml | cut -d' ' -f1)
api PUT /admin/settings '{"changes":{"files.max_size_bytes":104857600}}' | jq -e '.pending.id' >/dev/null || fail "PUT"
wait_for '.pending.phase == "confirming" and .pending.can_confirm' 90
id=$(api GET /admin/settings | jq -r .pending.id)
api POST /admin/settings/confirm "{\"id\":\"$id\"}" | jq -e .ok >/dev/null || fail "confirm"
wait_for "(.pending == null) and .last_result.id == \"$id\" and .last_result.outcome == \"applied\"" 30
[ "$(value max_size_bytes)" = 104857600 ] || fail "config.toml does not hold the change"
grep -q '^max_size_bytes = 104857600    # per file: 50 MiB$' /opt/meethub/config.toml || fail "the comment was lost"
[ "$(diff /opt/meethub/settings-state/config.toml.bak /opt/meethub/config.toml | grep -c '^[<>]')" = 2 ] ||
  fail "more than one line changed"
[ "$(sha256sum /opt/meethub/settings-state/config.toml.bak | cut -d' ' -f1)" = "$before" ] || fail "the backup"
[ "$(stat -c '%U:%G %a' /opt/meethub/config.toml)" = "root:root 644" ] || fail "config.toml is $(stat -c '%U:%G %a' /opt/meethub/config.toml)"
api GET /me | jq -e .user.id >/dev/null || fail "the owner was logged out by the restart"
api GET /bootstrap | jq -e '.files.max_size_bytes == 104857600' >/dev/null || fail "the new value is not in effect"
api GET '/admin/audit?action=settings.applied' | jq -e '.events | length == 1' >/dev/null || fail "audit"

echo "settings-systemd: 2. not confirmed: rolled back"
api PUT /admin/settings '{"changes":{"files.max_per_post":20}}' >/dev/null
wait_for '.pending.phase == "confirming"' 90
[ "$(value max_per_post)" = 20 ] || fail "not applied"
wait_for '(.pending == null) and .last_result.outcome == "rolled_back"' 200
[ "$(value max_per_post)" = 10 ] || fail "not rolled back"
ready

echo "settings-systemd: 3. stopped in the middle, then the boot-time run"
api PUT /admin/settings '{"changes":{"log.level":"warn"}}' >/dev/null
wait_for '.pending.phase == "confirming"' 90
systemctl kill -s KILL meethub-settings.service
sleep 1
[ "$(value level)" = '"warn"' ] || [ "$(value level)" = "'warn'" ] || fail "not applied before the kill"
[ ! -e /opt/meethub/data/settings/request.json ] || fail "the request is still there"
systemctl start meethub-settings.service # what the boot does
[ "$(value level)" = '"info"' ] || fail "the boot-time run did not undo the change: $(value level)"
wait_for '(.pending == null) and .last_result.outcome == "rolled_back" and (.last_result.reason | test("interrupted"))' 60

echo "settings-systemd: 4. a fourth change within the hour: refused without a restart"
pid=$(systemctl show -p MainPID --value meethub.service)
api PUT /admin/settings '{"changes":{"files.max_per_post":12}}' >/dev/null
wait_for '(.pending == null) and .last_result.outcome == "refused" and (.last_result.reason | test("too many"))' 60
[ "$(value max_per_post)" = 10 ] || fail "applied over the hourly limit"
[ "$(systemctl show -p MainPID --value meethub.service)" = "$pid" ] || fail "the refused change restarted the server"
# an hour later (the applier's count, which only root can write)
rm /opt/meethub/settings-state/consumed.json


echo "settings-systemd: 5. a package upgrade during a change"
sleep 1
api PUT /admin/settings '{"changes":{"log.format":"text"}}' >/dev/null
wait_for '.pending.phase == "confirming"' 90
apt-get install -y -qq --reinstall /pkg/meethub_*_amd64.deb >/tmp/install.log 2>&1 || { tail -n 20 /tmp/install.log; fail "reinstall"; }
[ "$(value format)" = '"json"' ] || fail "the upgrade did not undo the change: $(value format)"
systemctl is-active meethub-settings.path >/dev/null || fail "the path unit is not active after the upgrade"
ready
wait_for '(.pending == null) and .last_result.outcome == "rolled_back"' 60

echo "settings-systemd: ok"
