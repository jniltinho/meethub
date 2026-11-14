# Tasks

## 1. Packages

- [x] 1.1 Add `packaging/`: the hardened `meethub.service`, the `meethub.env` template, the shared install and
  removal scripts, the deb control file, the rpm spec and `build.sh` (one staged tree → `.deb`, `.rpm`, `.tar.gz`,
  `SHA256SUMS`)
- [x] 1.2 Add the `package-build` and `packages` stages to the Dockerfile (pinned `debian:13-slim`) and
  `make package`; verify the contents, owners, modes, version and dependencies of the three files
- [x] 1.3 Add `packaging/smoke.sh` and `make test-package`; verify install, files, user, `systemd-analyze verify`,
  a real start to `ready`, removal and purge on Debian 13, Ubuntu 26.04 and AlmaLinux 9 and 10

## 2. Release and documents

- [x] 2.1 Add the `Packages` workflow: build and smoke test on pushes and pull requests, the `.deb` installed
  under systemd on the runner, the four files published on `v*` tags
- [x] 2.2 Write `docs/install-linux.md` and `docs/install-binary.md`; link them from the README and the
  deployment guide
- [x] 2.3 Run `make check`, `make build` and `make test-package`; review with Codex
- [ ] 2.4 Verify the `Packages` workflow on GitHub: the smoke test and the systemd start pass on a push to main
- [ ] 2.5 Push the first `v*` tag and verify the release holds the four files and the `latest/download` links
  of the documents work

## Workflow follow-up

- Archive with `openspec archive add-linux-packages` after review.
