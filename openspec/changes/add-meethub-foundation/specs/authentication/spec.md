# Spec Delta

## Purpose

Defines how people get into MeetHub: first-run setup of the owner account, password login, revocable sessions,
logout, CSRF protection and brute-force limits.

## ADDED Requirements

### Requirement: First-run setup
While no user exists, the system SHALL expose a setup step that creates the organization and its `owner` account, and
SHALL disable that step permanently once a user exists.

#### Scenario: Setup on an empty install
- **GIVEN** an empty database
- **WHEN** a visitor submits the organization name, their name, e-mail and a valid password to the setup endpoint
- **THEN** the organization and the owner are created and the visitor is logged in

#### Scenario: Setup after the first user
- **GIVEN** at least one user exists
- **WHEN** anyone calls the setup endpoint
- **THEN** the response is 409 and nothing is created

### Requirement: Password policy and hashing
Passwords SHALL be at least 12 characters and SHALL be stored only as Argon2id hashes; no API or log SHALL return a
hash.

#### Scenario: Weak password rejected
- **WHEN** a user is created with the password `short`
- **THEN** creation fails with code `weak_password`

### Requirement: Login creates a session
`POST /api/v1/auth/login` with a correct e-mail and password SHALL create a server-side session and set it as an
`HttpOnly`, `SameSite=Lax` cookie (also `Secure` when cookies are configured secure); wrong credentials SHALL get the
same 401 response whether or not the e-mail exists.

#### Scenario: Successful login
- **WHEN** an active user logs in with correct credentials
- **THEN** the response is 200 with the user's profile and a session cookie is set

#### Scenario: Unknown e-mail and wrong password look the same
- **WHEN** a login uses an unknown e-mail, and another uses a known e-mail with a wrong password
- **THEN** both get 401 with code `invalid_credentials` and the same message

#### Scenario: Disabled user
- **GIVEN** a user whose status is `disabled`
- **WHEN** they log in with the correct password
- **THEN** the response is 401 with code `invalid_credentials`

### Requirement: Session lifetime and revocation
Sessions SHALL expire after the configured idle timeout and absolute lifetime, and SHALL be revocable server-side; a
password change SHALL revoke the user's other sessions.

#### Scenario: Logout revokes the session
- **WHEN** a logged-in user calls `POST /api/v1/auth/logout`
- **THEN** the session is revoked, the cookie is cleared and the old cookie gets 401 afterwards

#### Scenario: Idle expiry
- **GIVEN** a session unused for longer than the idle timeout
- **WHEN** it is used
- **THEN** the response is 401 with code `session_expired`

### Requirement: CSRF protection
Every state-changing request authenticated by cookie SHALL carry a CSRF token matching the session (double-submit
header), and requests without it SHALL be rejected with 403.

#### Scenario: Missing CSRF header
- **GIVEN** a logged-in browser session
- **WHEN** a `POST` to an authenticated endpoint omits the CSRF header
- **THEN** the response is 403 with code `csrf_failed`

### Requirement: Brute-force protection
Login attempts SHALL be rate limited per client address and per account; exceeding the limit SHALL return 429 with a
`Retry-After` header without revealing whether the account exists.

#### Scenario: Too many attempts
- **WHEN** a client makes more failed logins for one e-mail than the limit within the window
- **THEN** further attempts get 429 with `Retry-After` until the window passes

### Requirement: Current user endpoint
`GET /api/v1/me` SHALL return the logged-in user's profile, organization, role and preferences (including the theme),
or 401 without a valid session.

#### Scenario: Anonymous request
- **WHEN** `/api/v1/me` is requested without a session cookie
- **THEN** the response is 401 with code `unauthenticated`
