#!/bin/sh
# Starts the Mattermost baseline (v12.0.2, the all-in-one preview image) on http://localhost:8065 with the mockup's
# sample data, as a functional reference for parity checks of the chat (design D12/D14). Nothing of Mattermost is
# copied into MeetHub from it; screenshots taken from it stay in base/ (private, not committed).
#   scripts/reference/mattermost.sh        start and seed
#   docker rm -f mattermost-ref            remove it
# Users: ana.silva (system admin), bruno.martins, carla.pereira, diego.rocha — password Meethub-demo-2026!
set -eu
cd "$(dirname "$0")"
docker rm -f mattermost-ref >/dev/null 2>&1 || true
docker run -d --name mattermost-ref -p 127.0.0.1:8065:8065 \
  -e MM_SERVICESETTINGS_ENABLELOCALMODE=true -e MM_SERVICESETTINGS_SITEURL=http://localhost:8065 \
  mattermost/mattermost-preview:12.0.2 >/dev/null
printf 'waiting for Mattermost'
until curl -sf http://127.0.0.1:8065/api/v4/system/ping >/dev/null 2>&1; do printf .; sleep 5; done
sleep 5; echo
M="docker exec mattermost-ref mmctl --local"
$M user create --email ana@example.com --username ana.silva --password 'Meethub-demo-2026!' --firstname Ana --lastname Silva --system-admin
for u in bruno.martins:Bruno:Martins carla.pereira:Carla:Pereira diego.rocha:Diego:Rocha; do
  un=${u%%:*}; rest=${u#*:}; fn=${rest%%:*}; ln=${rest#*:}
  $M user create --email "$un@example.com" --username "$un" --password 'Meethub-demo-2026!' --firstname "$fn" --lastname "$ln"
done
$M team create --name tecnologia --display-name Tecnologia
$M team users add tecnologia ana.silva bruno.martins carla.pereira diego.rocha
for c in projetos:Projetos devops:DevOps infra:Infra suporte:Suporte; do
  $M channel create --team tecnologia --name "${c%%:*}" --display-name "${c#*:}"
  $M channel users add "tecnologia:${c%%:*}" ana.silva bruno.martins carla.pereira diego.rocha
done
python3 -I mattermost_seed.py
echo "Mattermost reference: http://localhost:8065 (ana.silva / Meethub-demo-2026!)"
