# Two-factor authentication

## Why

A password alone protects every conversation, file and recording of the organization, and passwords leak. Security
questionnaires ask for a second factor on the first page, and administrators want it at least for themselves.
Organizations with single sign-on get it from their provider (`add-oidc-login`); everyone else needs it in MeetHub.

## What Changes

- A person can add an authenticator app (TOTP) in Settings: a QR code and the secret to type, confirmed by a code,
  then ten one-time recovery codes shown once.
- With a second factor, a correct password is not enough: the login asks for a code (or a recovery code) and only
  then creates the session.
- Wrong codes are limited and count as failed logins.
- A person turns the second factor off with their password and a code. An administrator can reset another person's
  second factor (owners only by owners); every session of that person ends.
- Enrolling, turning off, resets and failed codes are audited.
- Stored authenticator secrets are encrypted with a key from the environment (`MEETHUB_SECRET_KEY`); without the
  key, two-factor is unavailable.

Not in this change: passkeys/WebAuthn, SMS or e-mail codes, forcing two-factor for a role or the whole
organization, "remember this device".

## Capabilities

### New Capabilities

None.

### Modified Capabilities

- `authentication`: enrolling, the second step at login, its limits, turning off and resets.
- `configuration`: the encryption key.
- `web-shell`: the Settings section and the code step of the login page.
- `audit-log`: two-factor events.

## Impact

- **Server**: `internal/auth` (TOTP per RFC 6238 with the standard library's HMAC, tickets in Redis, recovery
  codes hashed like passwords), a migration (`user_second_factors`), routes under `/api/v1/me/two-factor` and
  `/api/v1/auth/login/two-factor`, an admin reset route.
- **Dependencies**: a small pure-Go QR encoder to draw the enrollment code on the server (the secret never goes to
  a third-party service).
- **Web app**: Settings → Security, the login's second step; strings in three languages.
