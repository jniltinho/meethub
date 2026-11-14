# Tasks

Every Go task runs its tests with `-race` on SQLite, MariaDB and PostgreSQL (`make test-db`).

## 1. Server

- [x] 1.1 Add `MEETHUB_SECRET_KEY` (validation, redaction, example) and the AES-256-GCM helper; verify config
  tests and round trips, a wrong key and tampered data
- [x] 1.2 Implement TOTP (generation, verification with one step of tolerance, single use) and recovery codes;
  verify against the RFC 6238 test vectors and the replay scenario
- [x] 1.3 Add the migration and the enrollment, confirmation, turn-off and recovery-code routes; verify the
  `authentication` enrollment and turn-off scenarios
- [x] 1.4 Add the ticket and the second login step with its limits and the admin and CLI resets, with the audit
  events; verify the remaining `authentication` and `audit-log` scenarios, including a ticket from another address

## 2. Web app and docs

- [x] 2.1 Add Settings → Security (QR drawn by the server, secret as text, recovery codes shown once) and the code
  step of the login page (pt-BR, es); verify Playwright: enroll, log in with a code, with a recovery code, wrong code
- [x] 2.2 Document the key, backups and resets in `docs/security.md` and `docs/deployment.md`

## 3. Validation

- [ ] 3.1 Run `make check`, `make test`, `make test-db`, `make test-e2e`, `make build`, a Codex review with its
  findings fixed, and CI green (everything but CI done locally; CI needs a push)

## Implementation notes

- Migration `00009_two_factor.sql`: `user_second_factors` (sealed secret, `confirmed_at`, `last_step`) and
  `user_recovery_codes` (one row per Argon2id hash; a used code is deleted, so single use is a conditional delete).
- Routes: `GET|POST /me/two-factor`, `POST /me/two-factor/confirm`, `POST /me/two-factor/recovery-codes` (needs a
  current code), `POST /me/two-factor/disable` (password and a current code or a recovery code: a recovery code
  works "in place of a code", D3), `DELETE /admin/users/:id/two-factor`, `POST /auth/login/two-factor`;
  CLI `meethub user reset-two-factor --email`.
- Error codes: `400 invalid_code`, `400 two_factor_expired` (unknown, used, voided ticket or another address),
  `409 two_factor_enabled`, `409 two_factor_off`, `503 two_factor_unavailable`.
- Limits: the password attempt of an enrolled person keeps its reservation in the login limiter until the code is
  right (a right password alone does not clear the e-mail's failures); five wrong codes void the ticket and record
  `auth.login_failed`. Turning off and new recovery codes run under the same limiter.
- Audit events: `user.two_factor_enabled`, `user.two_factor_disabled`, `user.two_factor_reset`,
  `user.recovery_codes_generated`, `auth.recovery_code_used`, `auth.two_factor_failed` (each wrong code, as the
  proposal's "failed codes are audited") and `auth.two_factor_ticket_voided`.
- The admin user list carries `two_factor` so the Users page offers the reset only where it applies.
- Recovery codes are hashed as `meethub-recovery-code:<code>` (domain separation, and the hasher's 12-byte
  minimum). The QR code is drawn with `rsc.io/qr` (pure Go, BSD).
- `make init` adds a random `MEETHUB_SECRET_KEY` to `.env`; the e2e stack uses a fixed throwaway key.
- Codex reviewed the change twice; its findings are fixed in the commits that follow the feature: a password-only
  session re-checks under the user's row lock that two-factor is still off; a code is checked outside the
  transaction and used once under the lock, bound to the exact factor it was checked against; turning off, new
  recovery codes, starting and confirming an enrollment re-check the live session and the current password; a
  recovery code fails closed with another key; the whole confirmation runs under the login limits; nobody resets
  their own two-factor from the administration.
