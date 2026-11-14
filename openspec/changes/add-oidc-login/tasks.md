# Tasks

Every Go task runs its tests with `-race` on SQLite, MariaDB and PostgreSQL (`make test-db`).

## 1. Server

- [ ] 1.1 Add the `oidc` settings and `auth.password_login` with validation and `config.toml.example`; verify
  table-driven config tests
- [ ] 1.2 Implement discovery, the start and callback routes (PKCE, single-use state and nonce in Redis, ID token
  verification) against a test provider served in the test (signed tokens, JWKS); verify the login, reused state,
  wrong audience, expired token and wrong nonce
- [ ] 1.3 Implement matching and provisioning (verified e-mail, disabled user, auto-creation, allowed domains,
  default role, stored subject) with the migration and the audit events; verify the `authentication` and
  `audit-log` scenarios and a changed subject
- [ ] 1.4 Implement `auth.password_login = false` (login route, bootstrap answer, CLI unaffected); verify the
  scenario and the environment override

## 2. Web app and docs

- [ ] 2.1 Add the button and the failure message to the login page (pt-BR, es); verify Playwright against the
  test provider: both ways offered, single sign-on only, failure
- [ ] 2.2 Document the provider setup and the trust model; verify the route documentation test

## 3. Validation

- [ ] 3.1 Run `make check`, `make test`, `make test-db`, `make test-e2e`, `make build`, a Codex review with its
  findings fixed, and CI green
- [ ] 3.2 Try it once against a real provider (Keycloak in a container is enough) and record what was checked

## Workflow follow-up

- Archive with `openspec archive add-oidc-login` after review.
