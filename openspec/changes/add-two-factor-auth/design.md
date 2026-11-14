# Design

## Context

Login verifies the password with Argon2id under a bounded number of slots, applies per-e-mail and per-address
limits in Redis, and creates a server-side session. Password changes and resets end the person's sessions.
Secrets come only from the environment.

## Goals / Non-Goals

**Goals:** the standard authenticator-app flow, with no way around it through a half-finished login, and no secret
readable in a database dump. **Non-Goals:** passkeys, policies that force enrollment, trusted devices.

## Decisions

### D1. TOTP, standard parameters

RFC 6238: SHA-1, 6 digits, 30 seconds, a 160-bit random secret, one step of tolerance either way. A code is
accepted once: the last accepted step is stored and a code of that step or older is refused.

### D2. A ticket between the two steps

A correct password of a person with a second factor creates no session. The answer is `200 {two_factor_required:
true, ticket}`; the ticket (random, 5 minutes, single use, in Redis, bound to the user and the client address)
goes with the code to `POST /auth/login/two-factor`, which creates the session. Five wrong codes void the ticket
and count as one failed login in the existing limits.

### D3. Recovery codes

Ten codes of 10 characters, shown once, stored as Argon2id hashes like passwords; each works once in place of a
code. Generating new ones replaces the old.

### D4. Encryption at rest

`MEETHUB_SECRET_KEY` (32 random bytes, base64; also `_FILE`). Authenticator secrets are stored with AES-256-GCM
and a random nonce, the user id as associated data. Without the key the two-factor routes answer
`503 two_factor_unavailable` and Settings says so; with a wrong key, logins of enrolled people fail closed.

### D5. Single sign-on

A session created through the identity provider does not ask for MeetHub's second factor: the provider is
responsible for it.

## Risks / Trade-offs

- [A lost phone and no recovery codes] → an administrator's reset, audited, which ends the person's sessions; the
  last owner uses the server's CLI (`meethub user reset-two-factor`).
- [The key lost] → enrolled people cannot log in until an operator resets them from the CLI; documented with the
  backup instructions.

## Migration Plan

Additive migration. Nothing changes for people who do not enroll. Rollback drops the table; everyone logs in with
the password alone.
