# Spec Delta

## Purpose

Defines how people get into MeetHub: protected first-run setup of the owner account, password login, revocable
sessions, logout, CSRF protection and brute-force limits.

## ADDED Requirements

### Requirement: Setup requires a bootstrap token
While the install is not initialized, the server SHALL require a one-time setup token for the setup endpoint, taken
from `MEETHUB_SETUP_TOKEN` or, if unset, generated at startup, logged once and written to `<data_dir>/setup-token`
(mode 0600); the token SHALL be deleted when setup completes.

#### Scenario: Setup without the token
- **GIVEN** an uninitialized install
- **WHEN** a visitor posts the setup form without a token or with a wrong one
- **THEN** the response is 403 with code `invalid_setup_token` and nothing is created

#### Scenario: Token printed on first start
- **GIVEN** an empty database and no `MEETHUB_SETUP_TOKEN`
- **WHEN** `meethub server` starts
- **THEN** the log contains the setup URL with the token once and `<data_dir>/setup-token` holds it with mode 0600

### Requirement: Atomic one-time initialization
Setup (HTTP or CLI) SHALL create the organization, the owner, the membership and the `system.initialized` audit event
in one transaction guarded by a database-enforced singleton, so at most one initialization ever succeeds.

#### Scenario: Concurrent setup requests
- **GIVEN** an uninitialized install and a valid token
- **WHEN** two setup requests arrive at the same time
- **THEN** exactly one succeeds and the other gets 409 with code `already_initialized`

#### Scenario: Setup after initialization
- **GIVEN** an initialized install
- **WHEN** anyone calls the setup endpoint, even with the old token
- **THEN** the response is 409 with code `already_initialized` and nothing is created

#### Scenario: Partial failure
- **WHEN** creating the owner fails after the organization row was written
- **THEN** the transaction rolls back and the install stays uninitialized

### Requirement: Bootstrap status
`GET /api/v1/bootstrap` SHALL return, without authentication, whether setup is required, the organization's default
theme and the supported languages, and nothing else.

#### Scenario: Fresh install
- **WHEN** `/api/v1/bootstrap` is requested on an uninitialized install
- **THEN** it returns `setup_required: true` and no organization or user data

### Requirement: Password policy and hashing
Passwords SHALL be 12 to 256 bytes and SHALL be stored only as Argon2id hashes; no API, CLI output or log SHALL contain
a hash, and concurrent hash computations SHALL be bounded.

#### Scenario: Weak password rejected
- **WHEN** a user is created with the password `short`
- **THEN** creation fails with code `weak_password`

#### Scenario: Oversized password
- **WHEN** a login submits a 10 000-byte password
- **THEN** the response is 400 with code `invalid_input` and no hash is computed

#### Scenario: Hashing saturation
- **GIVEN** all hashing slots are busy for longer than the wait limit
- **WHEN** another login arrives
- **THEN** the response is 503 with code `busy` and a `Retry-After` header

### Requirement: Login creates a new session
`POST /api/v1/auth/login` with a correct e-mail and password SHALL always create a new session (never reuse a
presented one) and set its cookie; wrong credentials, unknown e-mails and disabled accounts SHALL get the same 401
response after the same password-verification work.

#### Scenario: Successful login
- **WHEN** an active user logs in with correct credentials
- **THEN** the response is 200 with the user's profile and a new session cookie is set

#### Scenario: Unknown e-mail and wrong password look the same
- **WHEN** a login uses an unknown e-mail, and another uses a known e-mail with a wrong password
- **THEN** both get 401 with code `invalid_credentials`, the same body, and both verify a password hash

#### Scenario: Disabled user
- **GIVEN** a user whose status is `disabled`
- **WHEN** they log in with the correct password
- **THEN** the response is 401 with code `invalid_credentials`

### Requirement: Session cookie attributes
The session cookie SHALL be `HttpOnly`, `SameSite=Lax`, `Path=/`, host-only (no `Domain`); when the public URL is
HTTPS it SHALL also be `Secure` and named `__Host-meethub_session`; clearing it SHALL use the same attributes.

#### Scenario: Production cookie
- **GIVEN** `http.public_url` is `https://chat.example.com`
- **WHEN** a user logs in
- **THEN** the cookie is `__Host-meethub_session` with `Secure; HttpOnly; SameSite=Lax; Path=/` and no `Domain`

#### Scenario: HTTPS required in production
- **GIVEN** `app.env` is `production`
- **WHEN** `http.public_url` is not an `https://` URL
- **THEN** startup fails naming `http.public_url`

### Requirement: Session lifetime and revocation
Sessions SHALL expire after the idle timeout (default 7 days) and the absolute lifetime (default 30 days); revocation
SHALL take effect on the next request on every replica, and a concurrent activity refresh SHALL never revive a revoked
or expired session.

#### Scenario: Logout revokes the session
- **WHEN** a logged-in user calls `POST /api/v1/auth/logout`
- **THEN** the session is revoked, the cookie is cleared and the old cookie gets 401 afterwards

#### Scenario: Idle expiry
- **GIVEN** a session unused for longer than the idle timeout
- **WHEN** it is used
- **THEN** the response is 401 with code `session_expired`

#### Scenario: Absolute expiry
- **GIVEN** a session used every day but created longer ago than the absolute lifetime
- **WHEN** it is used
- **THEN** the response is 401 with code `session_expired`

#### Scenario: Refresh racing a revoke
- **GIVEN** a request refreshing a session's activity while another revokes it
- **WHEN** both complete
- **THEN** the session stays revoked

### Requirement: Password changes and resets end sessions
A user changing their own password SHALL keep a new, rotated session and lose all others; an administrator or CLI
password reset SHALL revoke all of the target user's sessions.

#### Scenario: Self change
- **WHEN** a user changes their password with the current one
- **THEN** their other sessions get 401, and their current browser continues with a new session token

#### Scenario: CLI reset
- **WHEN** an operator runs `meethub user set-password` for a user with two sessions
- **THEN** both sessions get 401 on their next request

### Requirement: CSRF protection
Anonymous state-changing endpoints (setup, login) SHALL accept only `application/json` and an `Origin` equal to the
public URL's origin; authenticated state-changing requests SHALL also carry the session's CSRF token in `X-CSRF-Token`.
A missing, `null` or foreign `Origin` SHALL be rejected with 403 `csrf_failed`.

#### Scenario: Cross-origin login
- **WHEN** a page on another origin posts to `/api/v1/auth/login`
- **THEN** the response is 403 with code `csrf_failed`

#### Scenario: Missing CSRF header
- **GIVEN** a logged-in browser session
- **WHEN** a `POST` to an authenticated endpoint omits `X-CSRF-Token`
- **THEN** the response is 403 with code `csrf_failed`

#### Scenario: Token from another session
- **WHEN** a request carries a valid CSRF token that belongs to a different session
- **THEN** the response is 403 with code `csrf_failed`

### Requirement: Brute-force protection
Failed logins SHALL be limited per client address (default 20 per 15 minutes) and per normalized e-mail (default 5 per
15 minutes); over the limit, requests SHALL get 429 with `Retry-After` whether or not the account exists. Limiter
state SHALL be shared by replicas through Redis, and the in-memory fallback SHALL be bounded and expire.

#### Scenario: Too many attempts for one account
- **WHEN** a client fails 5 logins for one e-mail within 15 minutes
- **THEN** the 6th attempt for that e-mail gets 429 with `Retry-After`

#### Scenario: Spraying unknown e-mails
- **WHEN** one address tries 20 different unknown e-mails within 15 minutes
- **THEN** its 21st attempt gets 429, and limiter memory stays within its configured bound

#### Scenario: Redis unavailable
- **GIVEN** Redis is unreachable
- **WHEN** logins arrive
- **THEN** the limits are enforced per replica in memory and a warning is logged

### Requirement: Current user endpoint
`GET /api/v1/me` SHALL return the logged-in user's profile, organization, role, permissions and preferences (theme,
language), or 401 without a valid session.

#### Scenario: Anonymous request
- **WHEN** `/api/v1/me` is requested without a session cookie
- **THEN** the response is 401 with code `unauthenticated`
