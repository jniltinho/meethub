# Design

## Context

Login (`internal/auth`) verifies a password, applies the brute-force limits and creates a server-side session whose
token goes in an HttpOnly cookie; users are unique by normalized e-mail (`internal/identity`). Redis is always
there. The public URL is known (`http.public_url`).

## Goals / Non-Goals

**Goals:** a standards-only integration that works with any OpenID Connect provider; no way to take over an
account through an unverified e-mail. **Non-Goals:** SAML, LDAP, group mapping, provider-side logout.

## Decisions

### D1. Authorization code with PKCE, state in Redis

`/auth/oidc/start` creates `state`, `nonce` and a PKCE verifier, stores them in Redis for 10 minutes under the
state, and redirects to the provider. `/auth/oidc/callback` takes the state (single use), exchanges the code,
verifies the ID token (issuer, audience, signature, expiry, nonce) with `go-oidc`, and reads `email` and
`email_verified`. The redirect URI is always `<public_url>/api/v1/auth/oidc/callback`.

### D2. Matching and provisioning by verified e-mail

Without `email_verified = true` the login is refused. A known active user gets a session; a disabled one is
refused with the same generic page as any failure. An unknown e-mail is created only when `oidc.auto_create` is on
and the e-mail's domain is in `oidc.allowed_domains` (empty: any), with `oidc.default_role` (`member` or `guest`),
the display name from the `name` claim and a random unusable password. The subject (`sub`) is stored on first use;
afterwards the subject must match, so a recycled e-mail at the provider cannot enter an old account.

### D3. Password login can be switched off

`auth.password_login` (default true). When false the login page shows only the single sign-on button and
`POST /auth/login` answers `403 password_login_disabled`. Setup, and `meethub user` on the server's console, are not
affected: an operator who loses the provider sets `MEETHUB_AUTH_PASSWORD_LOGIN=true` and restarts.

## Risks / Trade-offs

- [A provider that marks unverified e-mails as verified] → the administrator's choice of provider; documented.
- [Lock-out when the provider is down and passwords are off] → the environment switch above.
- [An attacker with an account at a public provider] → `allowed_domains` and `auto_create = false` by default.

## Migration Plan

Additive migration (`users.oidc_subject`). Nothing changes without `[oidc]`. Rollback: remove the settings.
