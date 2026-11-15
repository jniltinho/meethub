# Tasks

## 1. The file and the request

- [x] 1.1 Add the allowlist to `internal/config` (key, type, group, bounds, environment variable) and the text
  edit that changes or adds keys and keeps the rest of the file; verify table-driven tests: an existing key with
  a trailing comment, a missing key, a missing table, arrays and durations, a file with no final newline, the
  comparison that refuses an edit touching anything else, every bound
- [x] 1.2 Add `internal/settings`: the request, its checks (allowlist, bounds, locks, the file's hash, the
  candidate loaded as at startup on the server's side), the mailbox rules (fixed names, no links, bounded regular
  files, the applier's own directory, consumed ids, the hourly limit), the durable writes, the state file,
  confirmation and the result; verify tests with a fake restart: applied and kept, not ready, not confirmed, a
  forged request, a replayed one, a link in the mailbox, a stale request, two at once, too many in an hour, and a
  stop injected at every step followed by the recovery

## 2. Server and applier

- [x] 2.1 Add `meethub settings-applier` (one transaction per run, the recovery first, the restart and the wait
  for a ready process started after it) and the boot id in `/api/v1/ready`; verify against a real server in a
  test: the three results, and that the server's user cannot write `config.toml`
  (done against the real server and the real unit in `packaging/settings-systemd-check.sh`, and the e2e stack's
  server under `--supervise`; the Go tests use a fake restart)
- [x] 2.2 Add `GET /api/v1/admin/settings` (keys with value, default, source, lock and bounds; read-only values
  with reasons; secrets as set or not set; whether an applier runs; the pending request and the last result),
  `PUT` (changes and restores → a request) and `POST …/confirm` for `manage_system`, rate limited and audited;
  verify: 403 for admins, the allowlist, bounds, a locked key, one request at a time, the audit events, no secret
  in any answer

## 3. Page

- [x] 3.1 Add Administration → Settings (groups, controls per type, defaults, locks, Restore default, pending
  changes, the themed confirmation, the progress, "Keep these settings" with its countdown, the result, the
  read-only values) with the pt-BR and es strings; verify the unit tests and Playwright on the three databases
  with a test applier: a change applied and kept, one not confirmed and rolled back, Restore default, a locked
  key, a refused value, an admin without access, the read-only page without an applier, accessibility and contrast
  (`tests/e2e/15-settings.spec.ts`; the test applier is the real one in its `--supervise` mode with a 20-second
  window; the read-only page with the answer's `applier` field replaced)
- [x] 3.2 Put each group, the meeting summaries setting and the read-only values in a sub-tab (the `.subtabs`
  recipe, the open tab in `?tab=`), with a count of the pending changes on each tab; verify
  `tests/e2e/15-settings.spec.ts` on the three databases and the page in a light and a dark theme
- [x] 3.3 Add the meeting recordings settings to the allowlist as a Recordings tab (`recordings.enabled`, a new
  on-or-off kind, the mode, the video format, who may record and the three limits), `recordings.retention_days`
  read-only as irreversible; tests of the kind, the bounds and the text edit; the page, the three languages,
  the documents
- [x] 3.4 Editable settings in the container images, opt-in: `settings-applier --supervise` as a documented mode
  (the configuration file in the data volume, seeded from the mounted one, a changed seed reported),
  `compose.settings.yml`, `MEETHUB_SETTINGS_APPLIER=on` in the all-in-one image with its smoke test, the local
  stack of `make up` (`compose.local.yml`), the documents and the security notes

## 4. Installs and documents

- [x] 4.1 Packages: `meethub-settings.path` and `.service` (also run at boot) with their hardening, enabled by
  the scripts, quiesced before an upgrade or a removal, in the `.deb`, the `.rpm` and the archive; verify under
  systemd on Debian 13 and Ubuntu 26.04: a change applied and kept, one rolled back, a reboot in the middle, the
  service unable to write `config.toml` or the applier's directory, the replacement working under the applier's
  sandbox, `systemd-analyze verify` on both units, the package smoke test
  (`make test-settings-systemd`; a reboot is simulated by killing the unit and starting it as the boot does; the
  `.rpm` under systemd and SELinux are not checked)
- [x] 4.2 Document the page, the request, the applier, the locks, the confirmation and the rollback, and what is
  read-only and why (`docs/`, the security notes, the install guides, the changelog); run `make check`,
  `make test`, `make test-db`, `make test-e2e`, `make test-package`, a Codex review with its findings fixed, and
  CI green (done; CI green on the commit of v0.2.0)

## Workflow follow-up

- Archive with `openspec archive add-admin-settings` after review.
