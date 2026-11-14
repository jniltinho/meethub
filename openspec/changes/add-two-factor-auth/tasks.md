# Tasks

Every Go task runs its tests with `-race` on SQLite, MariaDB and PostgreSQL (`make test-db`).

## 1. Server

- [ ] 1.1 Add `MEETHUB_SECRET_KEY` (validation, redaction, example) and the AES-256-GCM helper; verify config
  tests and round trips, a wrong key and tampered data
- [ ] 1.2 Implement TOTP (generation, verification with one step of tolerance, single use) and recovery codes;
  verify against the RFC 6238 test vectors and the replay scenario
- [ ] 1.3 Add the migration and the enrollment, confirmation, turn-off and recovery-code routes; verify the
  `authentication` enrollment and turn-off scenarios
- [ ] 1.4 Add the ticket and the second login step with its limits and the admin and CLI resets, with the audit
  events; verify the remaining `authentication` and `audit-log` scenarios, including a ticket from another address

## 2. Web app and docs

- [ ] 2.1 Add Settings → Security (QR drawn by the server, secret as text, recovery codes shown once) and the code
  step of the login page (pt-BR, es); verify Playwright: enroll, log in with a code, with a recovery code, wrong code
- [ ] 2.2 Document the key, backups and resets in `docs/security.md` and `docs/deployment.md`

## 3. Validation

- [ ] 3.1 Run `make check`, `make test`, `make test-db`, `make test-e2e`, `make build`, a Codex review with its
  findings fixed, and CI green

## Workflow follow-up

- Archive with `openspec archive add-two-factor-auth` after review.
