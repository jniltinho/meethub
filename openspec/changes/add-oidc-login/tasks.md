# Tasks

Every Go task runs its tests with `-race` on SQLite, MariaDB and PostgreSQL (`make test-db`).

## 1. Server

- [x] 1.1 Add the `oidc` settings and `auth.password_login` with validation and `config.toml.example`; verify
  table-driven config tests
- [x] 1.2 Implement discovery, the start and callback routes (PKCE, single-use state and nonce in Redis, ID token
  verification) against a test provider served in the test (signed tokens, JWKS); verify the login, reused state,
  wrong audience, expired token and wrong nonce
- [x] 1.3 Implement matching and provisioning (verified e-mail, disabled user, auto-creation, allowed domains,
  default role, stored subject) with the migration and the audit events; verify the `authentication` and
  `audit-log` scenarios and a changed subject
- [x] 1.4 Implement `auth.password_login = false` (login route, bootstrap answer, CLI unaffected); verify the
  scenario and the environment override

## 2. Web app and docs

- [x] 2.1 Add the button and the failure message to the login page (pt-BR, es); verify Playwright against the
  test provider: both ways offered, single sign-on only, failure
- [x] 2.2 Document the provider setup and the trust model; verify the route documentation test

## 3. Validation

- [x] 3.1 Run `make check`, `make test`, `make test-db`, `make test-e2e`, `make build`, a Codex review with its
  findings fixed, and CI green (green on GitHub for the commit that holds both changes)
- [ ] 3.2 Try it once against a real provider (Keycloak in a container is enough) and record what was checked
  (not done: needs the owner — the browser tests use the throwaway provider of `frontend/tests/e2e/idp`)

## Implementation notes

- Migration `00010_oidc.sql` adds `users.oidc_subject` (unique). The libraries are `github.com/coreos/go-oidc/v3`
  and `golang.org/x/oauth2`, as the proposal says.
- The provider is discovered on first use, so a provider that is down at startup does not stop the server.
- Besides the single-use state in Redis (by its SHA-256), the state is also kept in an `HttpOnly; SameSite=Lax`
  cookie and must match at the callback: a callback started in another browser fails (login CSRF). `/start` is
  limited to 30 a minute per client address.
- `email_verified` is accepted as `true` or `"true"` (some providers send a string); a missing one is a refusal.
- A refused single sign-on redirects to `/login?sso=failed`; the reason is audited as `auth.oidc_refused` (e-mail,
  address, reason) and logged, never shown. A success records `auth.oidc_login`; an account it creates,
  `user.created` with `"via": "oidc"`. The session skips MeetHub's second factor (`add-two-factor-auth` D5).
- Browser tests: the e2e stack runs a throwaway provider (`frontend/tests/e2e/idp/server.mjs`, a certificate made at
  start, trusted by MeetHub through `SSL_CERT_FILE`); "single sign-on only" is checked by changing the bootstrap
  answer in the browser, the server side of `password_login = false` by the Go tests.
- Codex reviewed the change twice; fixed after it: every provider endpoint must be `https://` and the client
  follows redirects only to `https://` (15 s timeout); provider errors reach the log only by status and OAuth error
  code or cut to 200 bytes; an account created by single sign-on gets its subject in the same transaction; callbacks
  are rate limited like starts.
