# Design

## Context

`add-admin-settings` made a short allowlist of `config.toml` keys editable through a root applier with a
confirmation and a rollback, and left the rest read-only for stated reasons: destinations (`ai.base_url`,
`ai.transcription.base_url`), secrets (the AI keys), what a rollback cannot take back
(`recordings.retention_days`) and where data is (`recordings.ffmpeg_path`, `recordings.dir`). The owner asked for
all of these to be editable in Administration, and for the key to be entered there. The organization row already
holds the `meeting_summaries` setting, read by the summaries service before every request and announced on the
bus; authenticator secrets are already sealed with `MEETHUB_SECRET_KEY` (`internal/auth/totp.go`).

Checked against OpenAI on 2026-11-17 (synthetic voices, the local stack): `whisper-1` reads the Ogg/Opus pieces
and answers timed segments; `gpt-4o-transcribe` and `gpt-4o-mini-transcribe` refuse `verbose_json` (the
connection test's `ai_unsupported`); `gpt-4.1-mini` and `gpt-4o-mini` write valid summaries; the `gpt-5` family
and later refuse `max_tokens` and a `temperature` other than the default (tracked in `add-meeting-summaries`).

## Goals / Non-Goals

**Goals:**
- Meeting summaries can be set up from the browser alone: key, models, address, limits.
- The reasons the settings were read-only keep an answer: a stolen session cannot redirect data or change a key,
  a rollback of a retention loses nothing, a broken path rolls back.
- The documentation says what to type for OpenAI and for a self-hosted server.

**Non-Goals:**
- Other secrets (SMTP, single sign-on, LiveKit, database) and other destinations stay as they are.
- No per-channel or per-team AI settings; no second summary provider.
- No change to what is sent to the AI service.

## Decisions

### D1. The AI keys live in the database, sealed

Two nullable columns on the organization row, `ai_api_key` and `ai_transcription_api_key`, hold the key sealed
with AES-GCM under `MEETHUB_SECRET_KEY` (the seal of `internal/auth` moves to a small shared package, bound to the
column name instead of a user id). The summaries service reads them at every entry point — with the
`meeting_summaries` setting it already reads before every request and every five seconds while one runs — so a
new key is used by every replica at its next request, with no message between servers. The AI clients take the
key from the service at each request. Precedence: `MEETHUB_AI_API_KEY`/`_FILE` > the stored key. "AI
configured" becomes a runtime answer (a key from either place and both models) instead of a startup constant.

*Why not the applier writing an environment file:* the key would cross the server's mailbox in plain text, need a
second file format to rewrite and a restart, and not exist on containers without an applier. *Why not
`config.toml`:* it is world-readable on package installs and the rule that it holds no secret stays whole.

A stored key that no longer opens (another `MEETHUB_SECRET_KEY`) counts as not set; nothing is deleted.

### D2. The other settings join the allowlist

| Key | Kind and bounds |
| --- | --- |
| `ai.base_url`, `ai.transcription.base_url` | text, up to 200 characters; the second may be empty; validated as at startup (https, or http to loopback/private; no credentials, query or fragment) |
| `ai.transcription_model`, `ai.summary_model` | text, up to 100 characters; empty allowed (then AI is not configured) |
| `ai.language` | empty or a two-letter code |
| `ai.timeout` | 10 seconds to 30 minutes |
| `ai.max_upload_bytes` | 1 MiB to 100 MiB |
| `ai.context_tokens` | 4 000 to 2 000 000 |
| `ai.max_output_tokens` | 256 to 200 000, below `context_tokens` (startup validation) |
| `recordings.retention_days` | 0 (keep) to 3 650 |
| `recordings.ffmpeg_path` | text, up to 200 characters |
| `recordings.dir` | text, up to 200 characters; empty = `<data_dir>/recordings` |

`KindString` gains a lower bound of 0 for the keys that may be empty. The applier checks the same table, as for
every key. `recordings.dir` is the capture volume, not where finished recordings are kept, so changing it moves
nothing; it is refused while a recording is unfinished (the check that already guards `recordings.enabled`).
`recordings.ffmpeg_path` is proven by the startup probe: a wrong one never survives the restart check.

### D3. Sensitive changes re-authenticate

`config.AdminKey` gains `Sensitive`. `PUT /admin/settings` with a sensitive key, and the keys route, verify
`password` (and `code` with two-factor on) through the auth service's existing password and code checks, under
the login rate limit, before anything is validated or written (`auth.Reauthenticate`; `403 reauth_failed`, not
the login's `401`, so the page does not take it for an ended session). The page shows "Confirm it is you" above
*Apply and restart*, only when needed; `GET /admin/settings/reauth` says whether a code will be asked. The applier does not see credentials: re-authentication is the server's guard against
a stolen session, the applier's allowlist remains the guard against a taken-over server.

Stated plainly in `docs/security.md`: a taken-over **server** (not a session) can now ask the applier to point
the AI address elsewhere, where before it could not. It could already read the key from its own environment.

### D4. Retention waits for "Keep these settings"

The expiry job skips its run while the settings service reports a pending change (`recordings.Service.Hold`).
`GET /admin/settings/retention?days=N` counts the recordings a retention would delete and the confirmation dialog
says so. With several servers on one database the request is known only to the host that serves the page; such
installs manage `config.toml` from outside, as before.

### D5. Hints

Each editable key gets one translated line under its label (what it is, a typical value). No new component.

## Risks / Trade-offs

- A database backup now contains the sealed AI key → useless without `MEETHUB_SECRET_KEY`, which is not in the
  database; documented with the backup notes.
- Owners who sign in only through single sign-on have no password → they change these settings on the server, as
  today; documented.
- A wrong `recordings.ffmpeg_path` runs that program once with ffmpeg's arguments as the service user before the
  probe fails → only an owner with the password can ask for it; documented.
- More keys behind a restart → nothing new; the two-minute confirmation applies.

## Migration Plan

One migration adds the two nullable columns (no backfill). Rollback: drop them; keys entered in the page are
lost and the environment variable remains.

## Review (Codex, 2026-11-17) and what changed

- **A key of one service never goes to another untested.** `ai.base_url` and `ai.transcription.base_url` are
  refused while summaries are on: the owner turns them off, changes the address, tests and turns them on again.
  The setup wizard does this in order (pause, apply, store the key, test and turn on).
- **No recording starts while `recordings.dir` is being changed** (`recordings.Service.Frozen`,
  `409 settings_pending`): one begun in a directory the confirmation may still undo would be stranded. The check
  for unfinished recordings and the request are not one atomic step; the window between them is the write of the
  request file.
- **A server that cannot use the AI service never cancels shared work.** Every server with recordings on now
  consumes the summaries queue; one whose stored key does not open (another `MEETHUB_SECRET_KEY`) returns the task
  to the queue instead of reading "off" and cancelling it. "Off" is what the organization stored, not what a
  server can do.
- **Removing a key turns summaries off in the same transaction**, by the stored value, whatever this server can
  read; turning them on re-checks under the organization's lock that the keys are there.
- `SetKeys` writes the missing keys of several new tables each under its own header.
- The pages clear the password and the code after every request, on Discard and once nothing sensitive is
  pending; with two-factor on, the wizard asks for a fresh code for its second protected operation; the wizard
  confirms only the request it made; the two passwords of the account step are compared on every change; the
  browser checks only the shape of an AI address (a private name such as `whisper` is the server's to judge).

## Checked on the local stack against OpenAI (task 5.2, 2026-11-17)

With no `MEETHUB_AI_API_KEY` in the environment: the key was pasted in Settings → Meeting summaries with the
owner's password and reads "Set: entered here and stored encrypted"; "Test connection" passed; the summary model
was changed from `gpt-4.1-mini` to `gpt-4o-mini` on the same tab (apply, restart, keep) and the test passed again;
a two-minute meeting of two synthetic voices in Portuguese was recorded and summarized in 22 seconds, with every
section and its headings in Portuguese.

