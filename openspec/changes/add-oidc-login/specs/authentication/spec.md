# Spec Delta

## ADDED Requirements

### Requirement: OpenID Connect login
With an OpenID Connect provider configured, `GET /api/v1/auth/oidc/start` SHALL redirect to it using the
authorization code flow with PKCE, a single-use `state` and a `nonce`, and `GET /api/v1/auth/oidc/callback` SHALL
verify the ID token and create a new session exactly as a password login does.

#### Scenario: Sign in through the provider
- **GIVEN** Ana exists in MeetHub with the e-mail her provider asserts as verified
- **WHEN** she completes the provider's login
- **THEN** she lands in the app with a new session cookie

#### Scenario: Reused state
- **WHEN** a callback arrives with a `state` that was already used or is unknown
- **THEN** no session is created and the login page shows a generic failure

### Requirement: Account matching and provisioning
A single sign-on login SHALL be accepted only with a verified e-mail: an existing active user signs in, a disabled
user is refused, and an unknown e-mail is created with `oidc.default_role` only when `oidc.auto_create` is on and
its domain is allowed. After a user's first single sign-on, the provider's subject SHALL have to match.

#### Scenario: Unverified e-mail
- **WHEN** the provider asserts an e-mail with `email_verified` false
- **THEN** no session is created

#### Scenario: Auto-creation off
- **GIVEN** `oidc.auto_create = false`
- **WHEN** someone unknown to MeetHub completes the provider's login
- **THEN** no account and no session are created

#### Scenario: Allowed domain
- **GIVEN** `oidc.auto_create = true` and `oidc.allowed_domains = ["example.com"]`
- **WHEN** `new@example.com` signs in for the first time
- **THEN** an active member account is created and signed in

### Requirement: Password login can be turned off
With `auth.password_login = false`, `POST /api/v1/auth/login` SHALL answer `403 password_login_disabled` and the
bootstrap answer SHALL say that only single sign-on is offered; first-run setup and the server's CLI SHALL keep
working.

#### Scenario: Password refused
- **GIVEN** password login is off
- **WHEN** a user posts a correct e-mail and password
- **THEN** the answer is `403` with code `password_login_disabled` and no session is created
