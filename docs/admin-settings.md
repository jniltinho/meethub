# Settings in Administration

**Administration → Settings** lets an owner change a short list of `config.toml` settings from the browser, with a
way back to each one's default. The change is written to `config.toml` itself — every other line and comment stay
as they are — by a small **settings applier** that runs beside the server, restarts it, and puts the previous file
back unless the owner confirms the change after the restart. Only owners see the page (`manage_system`).

## What can be changed

| Group | Setting | Bounds |
| --- | --- | --- |
| Files | `files.max_size_bytes` | 1 MiB to 2 GiB |
| Files | `files.max_per_post` | 1 to 50 |
| Sessions | `session.idle_timeout` | 5 minutes to 90 days (a Go duration: `30m`, `168h`) |
| Sessions | `session.absolute_lifetime` | 1 hour to 365 days, and not below the idle time |
| Single sign-on | `oidc.name` | the button's label, 1 to 40 characters |
| Logging | `log.level`, `log.format` | `debug`, `info`, `warn`, `error`; `json`, `text` |
| Recordings | `recordings.enabled` | on or off |
| Recordings | `recordings.mode`, `recordings.video_format`, `recordings.start` | `audio`, `video`; `webm`, `mp4`; `admins`, `members` |
| Recordings | `recordings.max_minutes` | 1 to 1440 |
| Recordings | `recordings.max_bytes` | 1 MiB to 1 TiB |
| Recordings | `recordings.max_concurrent` | 1 to 50 |
| Recordings | `recordings.retention_days` *(password)* | 0 (keep) to 3650 |
| Recordings | `recordings.ffmpeg_path` *(password)* | a name in `PATH` or a path, up to 200 characters |
| Recordings | `recordings.dir` *(password)* | a directory, or empty for `<data_dir>/recordings` |
| Meeting summaries | `ai.base_url` *(password)* | an `https://` address (`http://` only to a loopback or private one) |
| Meeting summaries | `ai.transcription_model`, `ai.summary_model` | up to 100 characters |
| Meeting summaries | `ai.language` | empty, or a two-letter code |
| Meeting summaries | `ai.timeout` | 10 seconds to 30 minutes |
| Meeting summaries | `ai.max_upload_bytes` | 1 MiB to 100 MiB |
| Meeting summaries | `ai.context_tokens`, `ai.max_output_tokens` | 4 000 to 2 000 000; 256 to 200 000 |
| Meeting summaries | `ai.transcription.base_url` *(password)* | as `ai.base_url`, or empty |

Each shows a one-line hint, its value, its default, and where the value comes from: `config.toml`, the built-in
default, or an environment variable. **Restore default** writes the built-in default into the file, so the line and its comment
stay. A shorter session time applies to the sessions that exist: someone idle for longer than the new idle time is
logged out at the restart, and the page says so before applying.

Turning [recordings](deployment.md#recordings) on needs meetings (LiveKit) and a working `ffmpeg`: without
meetings the change is refused before anything is written, and when the server's test recording at startup fails
(no `ffmpeg`, or `mp4` without `libx264`) the server does not start and the applier puts the previous file back.
A recording in progress when the server restarts is finished from what was captured, as on any restart; recordings
cannot be turned off while one is being recorded or prepared, since nothing would finish it.

**Settings marked *(password)*** decide where meeting audio and transcripts are sent, what is deleted, or which
program the server runs. A change that includes one shows "Confirm it is you" above *Apply and restart*: your
current password and, with two-factor on, a code from your authenticator (or a recovery code). They are checked
before anything else; a wrong one is audited as `settings.reauth_failed` and counts as a failed login. An account
without a password (single sign-on only) changes these on the server.

**The AI addresses** cannot be changed while meeting summaries are on: turn them off, change the address, press
*Test connection* and turn them on again. The key kept for one service is thus never sent to another before you
tested it.

**The recordings' retention** deletes for good. The confirmation says how many recordings the new value would
delete, and nothing is deleted until you press "Keep these settings": while a change waits for its confirmation
the expiry does not run, so a change that is rolled back loses nothing. `recordings.dir` cannot be changed while a
recording is in progress or being prepared, and no recording starts until that change is kept or undone. A `recordings.ffmpeg_path` that does not work fails the server's test
recording at startup, and the applier puts the previous file back.

## The AI keys

Settings → Meeting summaries has a form for the AI key (and, with `ai.transcription.base_url` set, the
transcription service's key). It is **not** part of the apply-and-restart flow and needs no settings applier: it
works on every install, containers included.

- Paste the key, enter your password (and code), press *Save key*. The key is stored in the database encrypted
  with `MEETHUB_SECRET_KEY`, used by every server from its next request — no restart — and never shown again: the
  page says only *Set*.
- *Remove* deletes it; if summaries were on they are turned off first and summaries being written stop.
- With `MEETHUB_AI_API_KEY` set the variable wins and the form is locked, naming it.
- Without `MEETHUB_SECRET_KEY` nothing can be stored and the form says so; set the key by the variable. Started
  with another `MEETHUB_SECRET_KEY`, the server counts a stored key as not set and asks for it again.
- Every change is audited as `ai.key_changed` with which key and `set` or `removed`, never the key.

A setting whose `MEETHUB_…` variable is set (in `meethub.env`, a drop-in or the container's environment) is shown
**locked**, with the variable's name: the variable wins when the server starts, so a change in the file would not
take effect.

Everything else is shown **read-only** with its reason — a secret only as *set* or *not set*, never its value:

- **Where secrets and data go** — `[smtp]`, `oidc.issuer`, `oidc.client_id`, `oidc.scopes`, the LiveKit
  addresses: a stolen owner session could otherwise point the mail password, the single sign-on secret or the
  meeting tokens at another server.
- **Who is believed** — `http.trusted_proxies`.
- **What would lock the page out** — `http.public_url`, `http.addr`, `http.body_limit_bytes`, the TLS settings,
  `auth.password_login`.
- **Where the data is** — `[app]`, `[database]`, `[redis]`, the file storage.
- **What a rollback cannot take back** — `oidc.auto_create`, `oidc.allowed_domains`, `oidc.default_role`,
  `notifications.email_excerpt`: accounts created and e-mails sent before a rollback stay created and sent.
- **Secrets** — they come only from the environment (the AI keys: from it, or from the form above).

These are changed in `config.toml` (or the environment) on the server, as before.

## Apply, keep or go back

1. **Apply and restart** asks for confirmation in a dialog naming the settings. The server validates the whole
   resulting configuration as at startup (with its real environment), records `settings.requested` in the audit
   log with each key's old and new value, and leaves a **request** in its own data directory
   (`/opt/meethub/data/settings/request.json`). The server never writes `config.toml`.
2. The **applier** (`meethub settings-applier`, as root) takes the request and checks it again on its own: only the
   keys above, within their bounds; `config.toml` unchanged since the request was made (a hand edit meanwhile
   refuses it as stale); a request never carried out before; at most three changes an hour.
3. It keeps the previous file (`/opt/meethub/settings-state/config.toml.bak`), replaces `config.toml` atomically
   and durably — a stop at any moment leaves the old file or the new one, whole — restarts the server and waits, at
   most 60 seconds, until a process started after the restart answers `/api/v1/ready`.
4. The page shows **Keep these settings** with the time left. Only the owner who applied the change can press it,
   in the same session, within **two minutes**. Without it — the server did not come back, or the owner cannot log
   in or reach the page with the new values — the applier puts the previous file back and restarts the server
   again. The file keeps its owner, group and mode (`root:meethub 0640` stays so). If an administrator edited
   `config.toml` by hand in the meantime, the applier leaves that file alone and says so in the result.
5. The result — **applied**, **rolled back** or **refused**, with the reason — is shown on the page and recorded as
   `settings.applied`, `settings.rolled_back` or `settings.refused`.

One change at a time: another waits until the first has its result. If the host or the applier stops in the
middle, the applier finishes or undoes the change at its next start (it runs at every boot), whatever is left in
the server's data directory.

## Where it works

- **The `.deb` and `.rpm` packages** install and enable the applier: `meethub-settings.path` starts
  `meethub-settings.service` when a request appears, and the service also runs at boot. Package upgrades and
  removals stop it first; a change it was carrying out is undone.
- **The archive** carries the two units beside the binary; [Install the binary](install-binary.md) installs them.
- **The container images** (Compose, the all-in-one image) are configured by environment variables and have no
  applier unless you turn one on: the page shows every value read-only and says so.

### Container installs (opt-in)

A container can run the applier too. It then runs the server as its child process and applies, restarts, waits
for "Keep these settings" and rolls back exactly as above:

- **Compose**: add `deployments/compose/compose.settings.yml` to the stack
  (`docker compose -f compose.yml -f compose.settings.yml up -d`).
- **The all-in-one image**: start it with `-e MEETHUB_SETTINGS_APPLIER=on`.

Two things differ from a package install, and are why this is not the default:

- **The file.** The applier cannot replace a mounted `config.toml`, so the configuration lives in the data volume,
  in `/data/config/config.toml` (its backup and state beside it, in `/data/config/settings-state`). It starts as a
  copy of the mounted `config.toml` — or empty in the all-in-one image, when it runs on variables alone — and is
  from then on the file the server reads. Editing the mounted file later changes nothing: the log says so at each start. Edit the file
  in the volume, or delete it to start again from the mounted one. Environment variables still win over it, and
  their settings stay locked on the page. The health check reads that file. Commands run with `docker exec`
  read it in the all-in-one image; in the Compose stack give them `--config /data/config/config.toml`.
- **No separation.** The applier is the container's only user, the server's own. A server that was taken over
  can write that file itself, any key of it, where on a package install it can only ask the root applier for the
  keys above. Secrets are still only in the environment, and the checks, the restart check, the confirmation, the
  rollback and the audit log work the same.

Stopping the container while a change waits for "Keep these settings" undoes it: the previous file is put back
and the next start runs with it.

Without the opt-in — the override file or the variable left out — the container reads its mounted `config.toml`
and its variables as before, and ignores `/data/config`.

With several servers on one database the page names them and the host it is served by: a change is applied only
on that host, so such installs manage `config.toml` from outside.

## Who can do what

The applier is the only writer of `config.toml` and of `/opt/meethub/settings-state` (root's, readable by the
service); the service's user can write neither. It treats the server's mailbox as hostile: fixed file names (the
request's id is never part of a path), no links followed, only regular files of a bounded size, nothing it writes
kept there. A server that was taken over gains the keys above, within their bounds — the AI addresses and the recordings'
paths included, because the applier does not see the owner's password: the password again guards against a stolen
session, not against a compromised server (which already holds the AI key and every message). The confirmation
guards against mistakes.

## When something is wrong

```bash
systemctl status meethub-settings.path meethub-settings.service
```

```bash
journalctl -u meethub-settings.service -n 50
```

The page says **"the settings applier did not take the request"** when no applier ran within a minute; the request
is withdrawn and nothing changes. After a failed path unit, `systemctl reset-failed meethub-settings.path` and
`systemctl restart meethub-settings.path` (a reboot or a package operation does it too). If a rollback itself could
not finish, the previous file is in `/opt/meethub/settings-state/config.toml.bak`.
