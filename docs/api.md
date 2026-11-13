# MeetHub API (`/api/v1`)

JSON over HTTPS. Errors always have the shape `{"error": {"code": "<machine code>", "message": "<English text>"}}`;
the web app translates by `code`. Timestamps are RFC 3339 in UTC; ids are ULIDs.

## Conventions

- **Session:** login and setup set an `HttpOnly; SameSite=Lax; Path=/` cookie — `__Host-meethub_session` (with
  `Secure`) when the public URL is HTTPS, `meethub_session` otherwise.
- **State-changing requests** (`POST`, `PATCH`, `PUT`, `DELETE`) must send `Content-Type: application/json` and an
  `Origin` equal to the public URL's origin (`403 csrf_failed` / `415 unsupported_media_type` otherwise).
  Authenticated ones also send the session's `X-CSRF-Token` (returned by login, setup and `GET /me` as `csrf_token`).
- **Unknown JSON fields are rejected** with `400 invalid_input`; malformed JSON is `400 bad_request`; bodies above
  `http.body_limit_bytes` (1 MiB) are `413 payload_too_large`.
- **Lists** are cursor-paginated: pass `limit` (default 50, max 200) and the previous page's `next_cursor`.

## Common error codes

| Status | Code | When |
| --- | --- | --- |
| 400 | `bad_request`, `invalid_input`, `weak_password`, `invalid_theme`, `invalid_language` | invalid request |
| 401 | `unauthenticated`, `session_expired`, `invalid_credentials` | no or ended session, wrong login |
| 403 | `forbidden`, `csrf_failed`, `invalid_setup_token` | not allowed |
| 404 | `not_found` | unknown route or resource |
| 405 | `method_not_allowed` | route exists with another method |
| 409 | `already_initialized`, `email_taken`, `last_owner` | conflicts |
| 413 | `payload_too_large` | body too large |
| 415 | `unsupported_media_type` | not JSON |
| 429 | `rate_limited` | too many failed logins (`Retry-After`) |
| 503 | `busy`, `unavailable` | password hashing saturated (`Retry-After`), dependency down |

## Probes and public data

| Route | Auth | Description |
| --- | --- | --- |
| `GET /api/v1/health` | — | Liveness: `200 {"status":"ok"}` while the process runs. |
| `GET /api/v1/ready` | — | Readiness: `200` or `503` with `{"status", "checks": {"database", "redis"}}` (`migrations pending`, `database unreachable`, `redis unavailable`). |
| `GET /api/v1/themes` | — | Theme catalog: `{"themes": [{id, label, scheme, default}], "default"}`. |
| `GET /api/v1/bootstrap` | — | `{"setup_required", "default_theme", "languages"}` — nothing about users or the organization. |

## Authentication

| Route | Auth | Description |
| --- | --- | --- |
| `POST /api/v1/auth/setup` | setup token | First run only. Body `{setup_token, organization_name, display_name, email, password}`. Creates the organization and its owner atomically and logs in: `201` + profile. `403 invalid_setup_token`, `409 already_initialized`. |
| `POST /api/v1/auth/login` | — | Body `{email, password}`. Always a new session: `200` + profile. Unknown e-mail, wrong password and disabled account all get the same `401 invalid_credentials`. Limits: 5 failures per e-mail and 20 per client address per 15 minutes (`429`). |
| `POST /api/v1/auth/logout` | session | Revokes the session and clears the cookie: `204`. |

## Current user

| Route | Auth | Description |
| --- | --- | --- |
| `GET /api/v1/me` | session | Profile: `{user, organization, role, permissions, preferences: {theme, language}, csrf_token}`. |
| `PATCH /api/v1/me` | session | Body `{display_name?, preferences?: {theme?, language?}}` (`""` resets to default). Languages: `en`, `pt-BR`, `es`. |
| `POST /api/v1/me/password` | session | Body `{current_password, new_password}`. Ends every session of the user and continues this browser with a new one: `200` + profile. |

## Administration

| Route | Permission | Description |
| --- | --- | --- |
| `GET /api/v1/admin/users` | `manage_users` | Users with their role, ordered by name then id. Query: `role`, `status`, `q` (name/e-mail), `limit`, `cursor`. |
| `POST /api/v1/admin/users` | `manage_users` | Body `{email, display_name, role, password}`: `201` + user. Only owners may create owners. |
| `PATCH /api/v1/admin/users/:id` | `manage_users` | Body with only `display_name`, `role`, `status` (`active`/`disabled`). Disabling ends the user's sessions. Admins cannot change owners; the last active owner cannot be demoted or disabled (`409 last_owner`). |
| `POST /api/v1/admin/users/:id/password` | `manage_users` | Body `{password}`: sets the password and ends all of the user's sessions: `204`. Owners only for owner accounts. |
| `GET /api/v1/admin/audit` | `read_audits` | Audit events, newest first by `(created_at, id)`. Query: `actor`, `action`, `from`, `to` (RFC 3339), `limit`, `cursor`. |

## Roles and permissions

| Permission | owner | admin | member | guest |
| --- | --- | --- | --- | --- |
| `manage_system` | ✓ | | | |
| `manage_owners` | ✓ | | | |
| `manage_users` | ✓ | ✓ | | |
| `read_audits` | ✓ | ✓ | | |
| `create_team` | ✓ | ✓ | | |
| `edit_own_profile` | ✓ | ✓ | ✓ | ✓ |
