# Linux packages, a systemd service and release assets

## Why

MeetHub can only be installed through Docker today. A team that runs services straight on a server (Ubuntu,
Debian, the RHEL family) has a static binary but no package, no systemd unit, no documented layout and nothing to
download: there are no releases with files.

## What Changes

- `make package` builds, in Docker, `meethub_amd64.deb`, `meethub.x86_64.rpm`, `meethub_linux_amd64.tar.gz` and
  `SHA256SUMS` into `dist/packages/`. The three hold the same tree: everything under `/opt/meethub` (binary,
  `config.toml`, `meethub.env` for the secrets, `data/`) and a hardened `meethub.service`.
- The packages create the `meethub` system user, enable and start the service on a first install, restart it on
  an upgrade, and never delete `/opt/meethub/data`.
- `make test-package` installs the packages in clean Debian 13, Ubuntu 26.04 and AlmaLinux 9 and 10 containers,
  starts the server as the unit does and removes them.
- A `Packages` workflow builds and tests them on every push and pull request, installs the `.deb` on the runner
  under systemd, and publishes the four files as the assets of each `v*` tag's GitHub release.
- Two documents: installing on Ubuntu 26.04 and Debian 13 from the package, and installing the binary by hand.

Not in scope: an APT or DNF repository, signed packages, other architectures, a package that sets up PostgreSQL,
a reverse proxy or LiveKit.

## Capabilities

### New Capabilities

- `linux-packages`: what the packages contain, how they install, upgrade and remove, the systemd unit, and how
  they are published.

### Modified Capabilities

- `build-and-deploy`: "Only linux/amd64 is produced" — `make package` writes `dist/packages/` beside the binary.

## Impact

- New: `packaging/` (unit, secrets template, scripts, control and spec files, `build.sh`, `smoke.sh`),
  `.github/workflows/packages.yml`, `docs/install-linux.md`, `docs/install-binary.md`.
- Changed: `Dockerfile` (stages `package-build` and `packages`, on a pinned `debian:13-slim`), `Makefile`
  (`package`, `test-package`), `README.md`, `docs/deployment.md`.
- No change to the server, the API, the database or the existing images.
