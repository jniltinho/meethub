# Security model

MeetHub's foundation follows the OWASP session and CSRF guidance. Requirements are in
`openspec/changes/add-meethub-foundation/specs/` (authentication, configuration, http-runtime).

## Secrets

- Accepted only from the environment or from a file named by `<VARIABLE>_FILE` (Docker secrets):
  `MEETHUB_DATABASE_PASSWORD`, `MEETHUB_REDIS_PASSWORD`, `MEETHUB_SETUP_TOKEN`, `MEETHUB_LIVEKIT_API_KEY`,
  `MEETHUB_LIVEKIT_API_SECRET`. A secret in `config.toml` fails
  validation. Database settings are structured fields — no DSN with credentials — and passwords are removed from
  error messages, including wrapped driver errors.
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

## Meetings

- Rooms are addressed only by the server: each channel's LiveKit room has a random 128-bit name, and clients ask for
  a token for a channel, never for a room. The token request follows the chat's read rule (404 for channels the
  caller may not discover, 403 otherwise, refused for archived channels), carries the CSRF check, and is answered
  with `Cache-Control: no-store`.
- Tokens last 2 minutes, name the user id as identity and grant only joining that room (publish, subscribe, data,
  own attributes): no room administration, recording or hidden participants. Tokens and the API secret are never
  logged.
- LiveKit's webhooks are accepted only with a valid signature over the exact body (64 KiB at most).
- Every 5 seconds the server removes from LiveKit anyone in a room who may no longer read its channel (removed, left
  the team, deactivated, channel archived).
