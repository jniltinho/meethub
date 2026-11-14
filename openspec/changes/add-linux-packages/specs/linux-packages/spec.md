## ADDED Requirements

### Requirement: Package build
`make package` SHALL build inside Docker, from the same static binary as `make build`, the files
`meethub_amd64.deb`, `meethub.x86_64.rpm`, `meethub_linux_amd64.tar.gz` and `SHA256SUMS` into `dist/packages/`.
A version `vX.Y.Z` SHALL become the package version `X.Y.Z`; any other version SHALL become `0.0.0+<version>`.

#### Scenario: Build on a clean host
- **GIVEN** a host with Docker and make but no Go, Node.js, dpkg or rpm
- **WHEN** the developer runs `make package`
- **THEN** `dist/packages/` holds the three packages and a `SHA256SUMS` that `sha256sum -c` accepts

#### Scenario: Version of a release
- **WHEN** the packages are built with `VERSION=v1.2.3`
- **THEN** the `.deb` and the `.rpm` report version `1.2.3`

### Requirement: Install layout
Each package SHALL install `/opt/meethub/bin/meethub`, `/opt/meethub/config.toml` (the example configuration with
`data_dir = "/opt/meethub/data"`), `/opt/meethub/meethub.env` (mode 0640, group `meethub`, no secret values) and
the unit `meethub.service`, and SHALL create the system user and group `meethub` and `/opt/meethub/data` (mode
0750, owned by `meethub`). The unit SHALL be the only file outside `/opt/meethub`.

#### Scenario: Install on Debian 13 and Ubuntu 26.04
- **WHEN** `apt install ./meethub_amd64.deb` runs on a clean system
- **THEN** the files exist with those owners and modes, the `meethub` user exists and a Redis server is installed

#### Scenario: Install on the RHEL family
- **WHEN** `dnf install ./meethub.x86_64.rpm` runs on a clean AlmaLinux 9 or 10
- **THEN** the same files, user and data directory exist

#### Scenario: Manual install
- **WHEN** `meethub_linux_amd64.tar.gz` is extracted in `/opt`
- **THEN** `/opt/meethub` holds `bin/meethub`, `config.toml`, `meethub.env` and `meethub.service`

### Requirement: systemd service
`meethub.service` SHALL run `/opt/meethub/bin/meethub server` as `meethub` with `/opt/meethub` as the working
directory and `/opt/meethub/meethub.env` as its environment file, restart it on failure, and let it write only
`/opt/meethub/data`. A first install SHALL enable and start the service; an upgrade SHALL restart it when it was
running; a removal SHALL stop and disable it.

#### Scenario: Running after the install
- **GIVEN** a host with systemd and a reachable Redis
- **WHEN** the package is installed
- **THEN** `meethub.service` is active and `GET /api/v1/ready` answers 200

#### Scenario: Secrets from the environment file
- **GIVEN** `MEETHUB_DATABASE_PASSWORD` set in `/opt/meethub/meethub.env`
- **WHEN** the service starts with a PostgreSQL database configured
- **THEN** it connects with that password, and the file is not readable by other users

#### Scenario: Unit accepted by systemd
- **WHEN** `systemd-analyze verify` checks the installed unit
- **THEN** it reports nothing about `meethub.service`

### Requirement: Data survives upgrades and removal
An upgrade SHALL keep `config.toml`, `meethub.env` and `/opt/meethub/data`. Removing or purging the package SHALL
never delete `/opt/meethub/data` or the `meethub` user.

#### Scenario: Removal keeps the data
- **GIVEN** an install whose database holds messages
- **WHEN** the package is removed, and purged on Debian or Ubuntu
- **THEN** the binary and the unit are gone and `/opt/meethub/data/meethub.db` is still there

#### Scenario: Upgrade keeps the configuration
- **GIVEN** an edited `/opt/meethub/config.toml`
- **WHEN** a newer package is installed
- **THEN** the edited file is still in place and the service runs the new version

### Requirement: Package smoke test
`make test-package` SHALL install the packages in clean containers of Debian 13, Ubuntu 26.04 and AlmaLinux 9 and
10, check the files, the user and the unit, start the server as the unit does until `/api/v1/ready` answers 200,
remove the package and check that the data stayed, failing with a message on the first problem.

#### Scenario: Every distribution passes
- **WHEN** `make test-package` runs on a host with Docker
- **THEN** it reports each distribution as ok and exits 0

### Requirement: Release assets
A `v*` tag SHALL publish `meethub_amd64.deb`, `meethub.x86_64.rpm`, `meethub_linux_amd64.tar.gz` and `SHA256SUMS`
as assets of that tag's GitHub release, only after the smoke test and a start under systemd passed. The asset
names SHALL carry no version.

#### Scenario: Tag published
- **WHEN** `v1.2.3` is pushed
- **THEN** the release `v1.2.3` holds the four files and
  `releases/latest/download/meethub_amd64.deb` downloads that `.deb`

#### Scenario: Failing package is not published
- **WHEN** the smoke test fails for a tag
- **THEN** no asset is uploaded

### Requirement: Installation documents
The repository SHALL document, in English, installing from the package on Ubuntu 26.04 and Debian 13
(`docs/install-linux.md`: layout, first start, configuration, upgrade, backup, removal, the `.rpm`) and
installing the binary by hand (`docs/install-binary.md`), both linked from the README.

#### Scenario: Following the package document
- **WHEN** an administrator follows `docs/install-linux.md` on a clean Ubuntu 26.04
- **THEN** MeetHub runs as `meethub.service` and the setup page opens with the token from `/opt/meethub/data`
