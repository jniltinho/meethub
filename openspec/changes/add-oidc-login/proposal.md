# Single sign-on with OpenID Connect

## Why

Organizations that evaluate MeetHub already have an identity provider — Google Workspace, Microsoft Entra ID,
Keycloak, Authentik, Okta — and do not want one more password per person, nor accounts that survive after someone
leaves the company. E-mail and password is the only login today, which rules MeetHub out for them before the first
conversation.

## What Changes

- One OpenID Connect provider can be configured (`[oidc]`: issuer, client id, the secret from the environment).
  The login page then shows a "Sign in with <name>" button beside the password form.
- The flow is the authorization code flow with PKCE, `state` and `nonce`; the callback creates the same server-side
  session as a password login.
- Accounts are matched by verified e-mail: an existing active user signs in as themselves; an unknown e-mail is
  created as a member (or guest) when `oidc.auto_create` is on and its domain is allowed, refused otherwise; a
  disabled user is refused.
- `auth.password_login = false` turns the password form off for everyone once single sign-on works; the CLI can
  still set a password and turn it back on.
- Single sign-on logins and accounts created by them are audited.

Not in this change: SAML, LDAP, several providers, mapping the provider's groups to roles or teams, SCIM, logging
out at the provider.

## Capabilities

### New Capabilities

None.

### Modified Capabilities

- `authentication`: the OpenID Connect login, account matching and provisioning, turning password login off.
- `configuration`: the `oidc` settings.
- `web-shell`: the single sign-on button on the login page.
- `audit-log`: single sign-on logins.

## Impact

- **Server**: `internal/auth` gains the provider discovery, the two routes (`GET /api/v1/auth/oidc/start`,
  `GET /api/v1/auth/oidc/callback`) and the matching rules; `internal/config` the settings; the bootstrap answer
  says whether single sign-on and password login are offered.
- **Dependencies**: `github.com/coreos/go-oidc/v3` and `golang.org/x/oauth2`.
- **Web app**: the login page; strings in three languages.
- **Docs**: provider setup (redirect URI, scopes) in `docs/deployment.md`; the trust model in `docs/security.md`.
