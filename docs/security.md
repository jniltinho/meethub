# Security model

MeetHub's foundation follows the OWASP session and CSRF guidance. Requirements are in
`openspec/changes/add-meethub-foundation/specs/` (authentication, configuration, http-runtime).

## Secrets

- Accepted only from the environment or from a file named by `<VARIABLE>_FILE` (Docker secrets):
  `MEETHUB_DATABASE_PASSWORD`, `MEETHUB_REDIS_PASSWORD`, `MEETHUB_SETUP_TOKEN`, `MEETHUB_LIVEKIT_API_KEY`,
  `MEETHUB_LIVEKIT_API_SECRET`, `MEETHUB_SMTP_PASSWORD`, `MEETHUB_SECRET_KEY`, `MEETHUB_OIDC_CLIENT_SECRET`. A secret in `config.toml` fails
  validation. Database settings are structured fields — no DSN with credentials — and passwords are removed from
  error messages, including wrapped driver errors.
- One exception, by the owner's choice: the **AI keys** may be entered in Administration → Settings → Meeting
  summaries instead of `MEETHUB_AI_API_KEY`. They are stored in the database encrypted with AES-256-GCM under
  `MEETHUB_SECRET_KEY` (bound to their column), are never returned, and need the owner's password again to set,
  replace or remove. A database dump alone reveals no key; with another `MEETHUB_SECRET_KEY` they count as not set.
  The variable, when set, wins and locks the form.
- Never written to logs, the audit log, task payloads or API responses.

## First run

- While the install is not initialized, the setup page needs a one-time **setup token**: `MEETHUB_SETUP_TOKEN`, or one
  generated at startup, logged once and written to `<data_dir>/setup-token` (mode 0600). It is deleted when setup
  completes.
- Initialization (web or `meethub user create --org-name …`) creates the organization, the owner, the membership and
  the `system.initialized` audit event in one transaction; a unique singleton column makes a second initialization fail
  on every database.

## Passwords

- 12 to 256 bytes; Argon2id (64 MiB, 3 passes, 2 lanes, 16-byte salt) stored as a PHC string, so parameters can be
  raised later. Concurrent hash computations are bounded (GOMAXPROCS slots, 5 s wait, then `503 busy`).
- Unknown e-mails are verified against a fixed dummy hash, so a failed login costs the same whether the account exists
  or not; unknown, wrong and disabled all answer `401 invalid_credentials`.

## Two-factor authentication

- Anyone can add an authenticator app in Settings → Security: TOTP per RFC 6238 (SHA-1, 6 digits, 30 seconds, a
  160-bit random secret), one step of tolerance either way. A code is accepted once: the last step used is stored and
  a code of that step or older is refused. The QR code is drawn by the server; the secret never goes to a third party.
- The secret is stored encrypted with AES-256-GCM (random nonce, the user id as associated data) under
  `MEETHUB_SECRET_KEY` (32 random bytes in base64, also `MEETHUB_SECRET_KEY_FILE`). Without the key the two-factor
  routes answer `503 two_factor_unavailable` and Settings says so; with another key, enrolled people cannot log in
  (fail closed) until the right key is back or an operator resets them. A database dump alone reveals no secret.
- Ten recovery codes of 10 characters (about 50 bits each), shown once and stored as Argon2id hashes like passwords;
  each works once in place of a code. New ones replace the old.
- With two-factor on, the right password creates no session: it answers a ticket (32 random bytes, 5 minutes, single
  use, in Redis by its SHA-256, bound to the user and the client address). The ticket with a code creates the session.
  The password attempt keeps counting against the login limits until the code is right; five wrong codes void the
  ticket and count as one failed login. Turning two-factor off needs the password and a code, under the same limits.
- An administrator with `manage_users` resets another person's two-factor (an owner's only by an owner); the last
  owner uses `meethub user reset-two-factor --email …` on the server. A reset ends every session of the person.
- Audited: `user.two_factor_enabled`, `user.two_factor_disabled`, `user.two_factor_reset`,
  `user.recovery_codes_generated`, `auth.recovery_code_used`, `auth.two_factor_failed` (a wrong code) and
  `auth.two_factor_ticket_voided` — never a code or a secret.

## Single sign-on (OpenID Connect)

- One provider, standards only: the authorization code flow with PKCE (S256), a random `state` (single use, in Redis
  by its SHA-256 for 10 minutes, and in an `HttpOnly; SameSite=Lax` cookie, so a callback started in another browser
  fails: no login CSRF) and a `nonce`. The ID token is verified with `go-oidc` against the provider's published keys:
  issuer, audience (`oidc.client_id`), signature, expiry and nonce. The client secret comes only from
  `MEETHUB_OIDC_CLIENT_SECRET`; tokens are never stored, logged or audited.
- **Trust model:** accounts are matched by e-mail, so the provider's `email_verified` must be `true` — a login without
  it is refused. Choose a provider that verifies addresses before asserting them: a provider that marks unverified
  e-mails as verified can sign anyone in as any MeetHub user. The provider's subject (`sub`) is stored on the first
  single sign-on of an account and must match afterwards, so an e-mail recycled at the provider cannot enter the old
  account. A disabled account is refused.
- Unknown e-mails get an account only with `oidc.auto_create = true` (off by default) and a domain in
  `oidc.allowed_domains` (empty: any — never with a public provider such as a consumer Google account), as
  `oidc.default_role` (`member` or `guest`, never more) and a random password nobody knows.
- The provider is responsible for the second factor: a single sign-on does not ask for MeetHub's.
- `auth.password_login = false` refuses password logins (`403 password_login_disabled`); first-run setup and the CLI
  keep working, and `MEETHUB_AUTH_PASSWORD_LOGIN=true` turns passwords back on if the provider is lost.
- Every refusal shows the person only that the sign-in could not be completed; the reason goes to the audit log
  (`auth.oidc_refused`, with the e-mail and the client address). A success records `auth.oidc_login`; an account it
  creates, `user.created` with `"via": "oidc"`.

## Sessions

- A random 32-byte token in the cookie; the database stores only its SHA-256. Idle timeout 7 days (activity written at
  most once a minute, never reviving a revoked or expired session), absolute lifetime 30 days; checked on every request,
  so revocation is immediate on every replica.
- Cookie `HttpOnly; SameSite=Lax; Path=/`, host-only; `Secure` and the `__Host-` prefix when the public URL is HTTPS.
  Production requires an HTTPS public URL.
- Login always creates a new session. Changing one's password ends every session and continues the current browser
  with a new token; an administrator or CLI reset, or disabling the user, ends all of their sessions.

## CSRF

- Every state-changing request must be JSON (the profile picture upload alone is `multipart/form-data`) and carry an
  `Origin` equal to the public URL's origin (missing or `null` fails). Authenticated requests also carry `X-CSRF-Token`, derived from the session token (synchronizer token) and
  compared in constant time; a token of another session fails.

## Uploaded pictures

The picture route takes at most 5 MiB (its own body limit; JSON routes keep `http.body_limit_bytes`), reads the format from the bytes, checks the declared size (4096 px a side) from the header
before decoding anything, and stores only its own re-encoding: a 256 px PNG, so EXIF data (location, device) and
anything appended to the file never reach the database or other users. Pictures are served as `image/png` with
`X-Content-Type-Options: nosniff`, only to callers who may see that user; "no picture" and "not visible" are the same
`404`. They live in the database (`user_images`), so replicas and backups need nothing else.

## Attached files

Any file type may be attached, so a file is never trusted to be what it says:

- **Access**: a file is served only to callers who may read its channel at that moment (a former member gets `404`),
  and to its uploader alone until a message carries it. There are no public links.
- **Never rendered from MeetHub's origin**: `GET /api/v1/files/:id` always answers `Content-Disposition: attachment`,
  `X-Content-Type-Options: nosniff` and `Content-Security-Policy: sandbox; default-src 'none'`, whatever the type —
  an uploaded HTML or SVG file downloads, it does not run. The media type shown is sniffed from the bytes; the type and
  the name the client declared are never used to decide anything.
- **Storage keys** are built by the server from a date and ids; the file's name is metadata only, so no name can
  place a file outside the storage or over another one.
- **Thumbnails and previews** are the only responses shown inline, and they are the server's own JPEG encodings
  (EXIF location and anything appended to the image are gone). An image's size is read from its header first: above
  24 megapixels, or undecodable, it is stored as an ordinary file and never decoded; decodes run two at a time.
- **Limits**: 50 MiB per file and 10 files per message by default (`[files]`), 60 uploads per user per minute, the
  route's own body limit; an upload over the limit leaves nothing in the storage. There is no virus scanning:
  put a scanner in front of the bucket if your policy needs one.
- **Lifecycle**: deleting a message makes its files unreachable at once; their bytes, and uploads never attached for
  24 hours, are removed by an hourly task.
- **S3 credentials** come only from the environment and are never logged.

## Rate limits

- Failed logins: 5 per normalized e-mail and 20 per client address per 15 minutes, in Redis (shared by replicas). During
  a Redis outage a bounded in-memory LRU (100 000 keys) applies per replica.

## Client address and proxies

- The client address is the TCP peer unless it is listed in `http.trusted_proxies`; only then are `X-Forwarded-For`
  (right-most untrusted hop) and `X-Forwarded-Proto` honored. It feeds rate limits and the audit log.

## HTTP headers

`Content-Security-Policy: default-src 'self'; script-src 'self'; style-src 'self'; img-src 'self' data: blob:;
font-src 'self'; connect-src 'self'; object-src 'none'; base-uri 'none'; form-action 'self'; frame-ancestors 'none'`,
`X-Content-Type-Options: nosniff`, `Referrer-Policy: same-origin`, `X-Frame-Options: DENY`, and HSTS with an HTTPS
public URL. The SPA uses no inline script (the theme boot script is a file).

## Authorization and audit

- Roles and permissions follow Mattermost's system roles (see `docs/api.md`); every endpoint is authorized on the
  server. Owner changes are serialized so two concurrent changes cannot leave the organization without an owner.
- The audit log is a transactional application history: a privileged change and its event commit together, and the
  change fails if its event cannot be written. Events never contain passwords, hashes, tokens or message bodies.

## Settings in Administration

- The server never writes its configuration and cannot: `config.toml` stays root's and read-only to the service.
  A change from Administration → Settings (owners only, `manage_system`, Origin and CSRF checks, a rate limit) is
  validated as at startup and left as a request in the service's data directory; a separate root unit,
  `meethub-settings.service`, carries it out ([Settings in Administration](admin-settings.md)).
- The applier trusts nothing the server wrote: it checks the keys against its own allowlist and bounds, the file's
  hash, and its own list of requests already carried out (a request is never applied twice, even after a
  rollback), and allows three changes an hour. It opens the mailbox without following links, reads only regular
  files of a bounded size, never builds a path from a request, and writes only in its own root-owned directory.
- The allowlist holds no mail server, single sign-on provider, LiveKit or proxy setting, no secret and nothing
  that could lock the page out. Secrets are shown only as *set* or *not set*.
- **Sensitive settings ask for the password again.** The AI addresses (where meeting audio and transcripts go),
  the recordings' retention (what is deleted), `recordings.ffmpeg_path` (a program the server runs) and
  `recordings.dir`, and every change of an AI key, need the owner's current password, and a current authenticator
  or recovery code when two-factor is on, checked before anything is validated or written; failures count as
  failed logins and are audited as `settings.reauth_failed`. A stolen session alone can therefore neither point
  meetings at another server nor change a key. An account without a password (single sign-on only) cannot make
  these changes from the page.
- A shorter retention deletes nothing until the owner keeps the change: the expiry does not run while a change
  awaits its confirmation, so a rollback loses no recording.
- What this does **not** cover: a server that was taken over (not a session) can ask the applier for any allowlisted
  key within its bounds, the AI addresses and `recordings.ffmpeg_path` included, since the applier does not see
  the owner's password. Such a server already holds the AI key and every message. A wrong `ffmpeg_path` is run once
  by the startup probe, as the service's user, before the change is rolled back.
- The container images run no applier and their page is read-only. One can be turned on
  ([Container installs](admin-settings.md#container-installs-opt-in)): it then runs as the container's only
  user, so the first point does not hold there — the configuration file, in the data volume, is writable by a
  server that was taken over. The mounted `config.toml` stays read-only and secrets stay in the environment.
- The confirmation after the restart guards against mistakes (the server does not start, the owner is locked
  out): without it in two minutes the previous file is restored. Each request and its result are audited
  (`settings.requested` with the old and new values, then `settings.applied`, `settings.rolled_back` or
  `settings.refused`).

## Meetings

- Rooms are addressed only by the server: each channel's LiveKit room has a random 128-bit name, and clients ask for
  a token for a channel, never for a room. The token request follows the chat's read rule (404 for channels the
  caller may not discover, 403 otherwise, refused for archived channels), carries the CSRF check, and is answered
  with `Cache-Control: no-store`.
- Tokens last 2 minutes, name the user id as identity and grant only joining that room (publish, subscribe, data,
  own attributes): no room administration, recording or hidden participants. Tokens and the API secret are never
  logged.
- LiveKit's webhooks are accepted only with a valid signature over the exact body (64 KiB at most).
- Every 3 seconds the server removes from LiveKit anyone in a room who may no longer read its channel (removed, left
  the team, deactivated, channel archived).

## Recordings

- Off by default. Who may record is decided by the server: a member in the room who administers the channel (any
  member in a conversation, or with `recordings.start = "members"`). A recording's file is an attachment of its
  channel: whoever reads the channel may play and download it, nobody else (404).
- The recorder is a hidden participant with a token of its own: that room only, subscribe only, no publishing. It
  records only while its server holds the recording's lease; a server that cannot renew it stops at once, and
  reconciliation disconnects any recorder without a live recording.
- People are informed (the room's mark, announcements, the pre-join notice) — not asked for consent; whether that
  suffices is a legal question for each organization. Starting and stopping are audited, never the content.
- Captures stay on the capture volume until composed, then are deleted; `ffmpeg` runs with argument lists the server
  builds (no shell), and its output never reaches logs or messages.

## Meeting summaries

- **Off by default, and the organization's switch is real.** With the Meeting summaries setting `off`, or without
  an AI service configured (a key and both models), nothing of any meeting is sent anywhere. The setting is read from the database before every
  request to the AI service and every 5 seconds while one runs; turning it off stops every server within 10 seconds
  (immediately through Redis when it is up). What was already sent cannot be recalled. Only `manage_system`
  changes it; changes are audited with both values, requests for a summary with the recording, never the content.
- **What is sent**, and only to the configured base URLs (redirects are not followed; `http://` only to private
  addresses): the audio of each participant's speech, in pieces; then the transcript — what people said, with the
  **display names they chose** — and fixed instructions. MeetHub adds no e-mail, user id, channel or organization
  name. But what people *say* goes as it is, **including anything sensitive they say**: names, numbers, health,
  customers. Choose the service accordingly; a server of your own keeps it in house. The connection test sends only
  a generated tone and a fixed line.
- **Keys** only from the environment (`MEETHUB_AI_API_KEY`, `MEETHUB_AI_TRANSCRIPTION_API_KEY`); never in files,
  logs or answers.
- **Nothing of the content is logged**: no audio, transcript or summary, and no error body of the service (it may
  repeat what was sent). Failures are reduced to a class and a status code (`ai_unauthorized`, `ai_unavailable`, …).
- **The model is not trusted.** The transcript is passed as content under instructions that say it is a record of
  speech, not instructions; the summary is rendered by the same sanitizing Markdown pipeline as any message (no HTML,
  no remote images), its mentions notify nobody, and the "Written by AI" label is the interface's. Answers are read
  up to 1 MiB.
- **Access**: the summary and its transcript are a reply and a file of the recording's channel — readable by who
  reads the channel, nobody else (404) — and go with the recording's message. Summary sources are internal objects,
  not listed or downloadable, removed when the summary is posted or after seven days. A recording's retention removes
  the transcript and keeps the summary text.
- People are told (the room's mark, the start confirmation, the announcement, the pre-join screen) under both modes
  — informing, not consent.

## Notification e-mails

- Sent only for direct messages and mentions by name, and only of messages the person can read when the e-mail is
  written (a member of the channel, the message not deleted, the channel not archived) and has not read yet.
- They carry who wrote, where and, unless `notifications.email_excerpt = false`, the start of each message (160
  characters). Organizations that must keep message text out of mailboxes turn the excerpt off.
- The SMTP password comes only from `MEETHUB_SMTP_PASSWORD`; `smtp.tls = "none"` with a password is refused unless the
  host is loopback or private. STARTTLS is required in the `starttls` mode (no silent downgrade) and certificates
  are verified.
- Each e-mail has its own random 256-bit unsubscribe token; only its SHA-256 is stored, it lasts 90 days, and every
  token of a person ends when they turn e-mails on again. The one-click endpoint needs no session; the page of the
  link asks before turning e-mails off (link scanners open links). A failed send is logged without the message's
  content or the address.
