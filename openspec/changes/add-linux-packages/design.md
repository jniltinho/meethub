# Design

## Context

The server is one static linux/amd64 binary that reads `config.toml` from its working directory (then
`/etc/meethub/config.toml`), takes secrets only from the environment, and keeps SQLite, uploads and the setup
token under `app.data_dir`. Builds are Docker-only with pinned images. The owner asked for an install under
`/opt/meethub` with a systemd service, on Ubuntu 26.04 and Debian 13, plus `.deb` and `.rpm` release files.

## Decisions

### D1. Everything under /opt/meethub

`bin/meethub`, `config.toml`, `meethub.env`, `data/`. The unit sets `WorkingDirectory=/opt/meethub`, so the
existing lookup finds `config.toml` with no flag and no code change; the shipped `config.toml` is
`config.toml.example` with `data_dir` made absolute, so the command line works from any directory too. The only
file outside is the unit: systemd loads units from its own directories, and a package manager can only track it
there. The binary sits in `bin/` because SELinux labels `/opt/*/bin` as executable.

Alternative: the FHS split (`/usr/bin`, `/etc/meethub`, `/var/lib/meethub`). Rejected: the owner asked for one
folder, and one folder is also what a backup and a manual install want.

### D2. dpkg-deb and rpmbuild, not a packaging tool

Both packages are assembled in a `debian:13-slim` stage with Debian's own `dpkg-deb` and `rpmbuild` from one
staged tree, with one set of maintainer scripts. Alternative: nfpm (one YAML for both formats). Rejected: a new
third-party build dependency for about sixty lines of control and spec files.

### D3. One set of scripts for both formats

`postinstall.sh`, `preremove.sh` and `postremove.sh` read the first argument both ways (`configure`/`remove`/
`purge` for dpkg, `1`/`2`/`0` for RPM). A first install enables and starts the service; an upgrade restarts it if
it was running; a removal stops and disables it and leaves `/opt/meethub/.removed`, so that installing the package
again (which dpkg reports like an upgrade) enables and starts it once more; `abort-remove` does the same. Without systemd (a container, a chroot) they only create the
user and the data directory.

### D4. Data outlives the package

`/opt/meethub/data` is created by the script, not shipped, and neither removal nor purge deletes it or the user:
a team's messages and files must not disappear as a side effect of `apt purge`. The documents say how to delete
them by hand.

### D5. Redis is recommended, not required

`Recommends: redis-server` (deb) and `Recommends: (redis or valkey)` (rpm): the default install brings a local
Redis, and an install that uses a Redis on another host can skip it. The unit orders itself after the local Redis
and database units when they exist.

### D6. A hardened unit

`ProtectSystem=strict` with `ReadWritePaths=/opt/meethub/data`, no capabilities, private `/tmp` and devices,
restricted address families and namespaces. A data or files directory elsewhere needs a drop-in; the unit and the
document say so.

### D7. Asset names without a version

`meethub_amd64.deb`, `meethub.x86_64.rpm`, `meethub_linux_amd64.tar.gz`: `releases/latest/download/<name>` then
stays valid for every release, which is what the documents link. The version is inside each package (`vX.Y.Z` →
`X.Y.Z`; any other build, also a commit that starts with a digit → `0.0.0+<commit>`, older than every release).

### D8. Publishing

The `Packages` workflow publishes on `v*` tags only, with the workflow token, after the container smoke test and
a real systemd start on the runner. `gh release create` uploads the files to a draft and publishes it only when
all are there; a release that already exists only receives the files it lacks, so a re-run repairs a partial
release and leaves a complete one alone.

## Risks

- The hardened unit runs under real systemd in CI only on the Ubuntu 24.04 runner. It was also started by hand
  under systemd in Debian 13 (systemd 257) and Ubuntu 26.04 (systemd 259) containers: active, ready, running as
  `meethub`, a write outside `data/` refused, a clean stop, data kept after removal. Not tried: an RPM host under
  systemd and SELinux in enforcing mode. The smoke test runs `systemd-analyze verify` on each distribution.
- MeetHub's own test suites run against Redis; AlmaLinux ships Valkey. The smoke test reaches `ready` with it,
  nothing more.
- Unsigned packages: `apt` and `dnf` install local files without a signature, and `SHA256SUMS` only guards the
  download against corruption.
