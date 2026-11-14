# Design

## Context

- `config.Load` reads defaults, then one TOML file (`--config`, `./config.toml`, `/etc/meethub/config.toml`), then
  `MEETHUB_<SECTION>_<KEY>` variables, and validates the result statically. Unknown keys and secrets in the file
  are refused. The values are copied into the services once, in `cmd/server.go`: nothing re-reads them, and
  `runServer` returns at once when the configuration does not load.
- ad-manager, whose conventions MeetHub follows, has on its Settings screen an allowlist of `config.toml` keys
  that the panel never writes: it sends the change to a root agent on the same host, which edits the text (keeping
  comments), keeps a `.bak`, restarts the panel, checks that it answers and rolls back otherwise. Keys set by the
  environment are locked.
- Under the package's unit the service may write only `/opt/meethub/data`; `/opt/meethub/config.toml` is root's.
  The container images are configured by variables and run one unprivileged user.
- Owners hold `manage_system`; no route uses it yet. `add-meeting-summaries` adds one organization setting to
  Administration.

## Goals / Non-Goals

**Goals:** the owner's request — settings changed in the browser, saved in the file, with a way back to the
default; no change can leave the server down or its owner locked out; a server taken over cannot rewrite its
own configuration.

**Non-goals:** a second store that overrides the file; live reconfiguration without a restart; editing where
secrets or data are sent; secrets in the browser; container images in this version.

## Decisions

### D1. The file is the store

A change edits `config.toml`; nothing is kept in the database. What the page shows is what a text editor shows,
a backup of `/opt/meethub` holds the whole configuration, and an administrator who prefers the shell works as
before. Alternative: overrides in a table. Rejected: two places to look for a value, and the owner asked for the
file.

### D2. A short allowlist: settings, not destinations

`config.AdminKeys`, each with its type, its bounds and its group:

| Group | Keys | Bounds |
| --- | --- | --- |
| Files | `files.max_size_bytes`, `files.max_per_post` | 1 MiB – 2 GiB; 1 – 50 |
| Sessions | `session.idle_timeout`, `session.absolute_lifetime` | 5 minutes – 90 days; 1 hour – 365 days and not below the idle time |
| Single sign-on | `oidc.name` | the button's label, up to 40 characters |
| Logging | `log.level`, `log.format` | the loader's values |

A shorter session lifetime applies to the sessions that exist: someone idle for longer than the new idle time is
logged out at the restart. The page says so before applying.

Left read-only, each for a reason the page states:

- **Where secrets and data go** — `smtp.*`, `oidc.issuer`, `oidc.client_id`, `livekit.url`, `livekit.api_url`: a
  stolen owner session could point the mail password, the single sign-on client secret, the LiveKit tokens or
  people's messages at another server. Naming a destination stays a decision made on the server.
- **Who is believed** — `http.trusted_proxies`: no check made from one request proves a range safe for the others.
- **What would lock the page out** — `http.public_url`, `auth.password_login`, `http.body_limit_bytes`,
  `http.addr`, the TLS settings.
- **Where the data is** — `app.*`, `database.*`, `redis.*`, `files.driver` and its locations.
- **What cannot be taken back** — `oidc.auto_create`, `oidc.allowed_domains`, `oidc.default_role` and
  `notifications.email_excerpt`: accounts created and e-mails sent during the two minutes before a rollback stay
  created and sent.

Later changes that add simple settings add their keys to the allowlist.

### D3. A text edit that keeps the file

The edit works on the file's text, key by key, inside the right table: an existing `key = value` line gets the
new value and keeps its trailing comment; a key the file lacks is added at the end of its table; a table the file
lacks is appended. The result is parsed and compared with the old file parsed: exactly the requested keys differ,
or the edit is refused. The approach and its checks are ad-manager's (`SetKeys`, `CheckEdit`), ported.

**Restore default** writes the built-in default of the key, so the line and its comment stay; a value equal to
the default is shown as "default".

### D4. The server asks, an applier writes

The server never writes `config.toml` and gets no permission to. It validates the change and writes a **request**
(`<data_dir>/settings/request.json`: an id, who, when, the keys with their new values, the SHA-256 of the file
the change was made from). `meethub settings-applier`, the same binary running as its own root unit started by a
systemd path unit on that request, does the rest:

1. It reads the request and re-checks what it can decide alone, trusting nothing the server wrote: only
   allowlisted keys, within bounds; the file's hash is the one in the request (a hand edit meanwhile refuses the
   request); the candidate parses. Whether the whole configuration validates is the server's check, made before
   it writes the request, because only the server has its real environment (the unit's variables, drop-ins and
   `_FILE` secrets); a candidate the running service then rejects is caught by step 4 and rolled back.
2. It holds a lock for the whole transaction: one apply at a time, a second request waits its turn against the
   new file or is refused as stale.
3. It writes the previous file as `config.toml.bak` beside it and a state file of its own (root's, under
   `/opt/meethub`), each written to a temporary name, synced, renamed, the directory synced; then replaces
   `config.toml` the same way. A stop at any point leaves either the old file or the new one whole, and the state
   says which step was reached.
4. It restarts the service with systemd and waits until `/api/v1/ready` answers **from a process started after
   the restart** (the boot id the server reports), at most 60 seconds.
5. It then waits for the owner's **confirmation**: the page, reloaded, calls the server with the request's id,
   and the server records it where the applier reads it. No confirmation in two minutes — the server never
   became ready, or the owner cannot log in or reach the page with the new values — and the applier restores
   `config.toml.bak` the same durable way, restarts and waits for ready again.
6. It writes the result (applied, rolled back with the reason, refused with the reason) with the request's id;
   the server shows it and records it in the audit log.

At its own start the applier finishes or undoes a transaction its state file shows unfinished, before looking at
any request, so a reboot in the middle ends in a consistent file whatever the server's configuration says.

What a compromised server gains is what an owner has: allowlisted keys within bounds. The confirmation is a
safeguard against mistakes, not a security boundary — a server that was taken over can confirm its own request —
which is why nothing on the allowlist is dangerous to hold and nothing irreversible is on it.

### D4a. The mailbox between the two

The request directory belongs to the service, so the applier treats it as hostile:

- fixed file names; the request's id is data, never part of a path;
- files opened without following links, required to be regular files of a bounded size, read once into memory;
- everything the applier writes — the backup, its state, the consumed-request list and the results — lives in a
  directory only root can write (`/opt/meethub/settings-state`), which the service may read;
- a request is consumed once: its id is recorded before anything is changed, and an id seen before is refused,
  so a rolled-back request cannot be replayed although the file's hash matches again;
- at most three applies an hour, counted by the applier, whatever the server's own rate limit says;
- the confirmation the applier accepts names the request's id and the boot id of the process started by the
  restart, and the server writes it only on an explicit, CSRF-protected action of the owner who made the request,
  in the same session, before the deadline.

### D4b. The applier's own lifecycle

- The unit also runs at boot (not only when a request appears), so a transaction interrupted by a reboot is
  finished or undone from root's state even if the request was deleted.
- Its sandbox lets it write `/opt/meethub` (a rename needs the directory) while the binary and `meethub.env` are
  read-only to it; the replacement is tested under that sandbox.
- The package's pre-removal and upgrade steps wait for a running transaction to end, or undo it, before the
  binary is replaced or removed.
- A path unit that failed is reset by the next boot or package operation; the page reports "the applier is not
  running" rather than waiting.

Alternative: the service writes `config.toml` itself (group-writable file, re-executing the binary). Rejected: it
gives the service control of every key, not only the allowlisted ones; a write in place cannot be atomic in a
directory the service may not write; and a server that hangs or dies cannot roll itself back.

### D5. Where it works

- **Linux packages**: `meethub-settings.path` and `meethub-settings.service` (root, a oneshot with its own
  hardening: it may write `/opt/meethub/config.toml`, its backup and its state, and restart `meethub.service`).
  The post-install script enables the path unit; removal disables it.
- **Manual install from the archive**: the two units are in the archive; the binary install guide installs them.
- **Container images**: no applier. The page is read-only and says the install is configured by variables.

### D6. Environment locks

A key whose `MEETHUB_…` variable is set is locked, with the variable named: the variable wins at load, so a file
change would be a lie.

### D7. Who, and what is recorded

Owners only (`manage_system`), with the usual `Origin` and CSRF checks and a rate limit; one request pending at a
time. The audit log records `settings.requested` with each key and its old and new value (no allowlisted key is a
secret), then `settings.applied`, `settings.rolled_back` or `settings.refused` with the reason.

### D8. One replica

The request is written and applied on the host that served it. With several replicas the page says so and names
the host; such installs manage `config.toml` from outside.

### D9. The page

`/admin/settings`: the groups of D2, each key with a control of its type, its default, a lock with the variable's
name when locked, and **Restore default**; a bar with the number of pending changes and **Apply and restart**,
which asks for confirmation in a themed dialog naming the keys; a waiting state that follows the request by its
id; after the restart, **Keep these settings** with the time left; then the result. Below, the read-only values
with their reasons. The summaries setting of `add-meeting-summaries` sits on the same page.

## Risks / Trade-offs

- **A root unit** → small and separate: it accepts one kind of request, re-validates it, and touches three
  files and one service. Its hardening and its tests are part of the change.
- **A restart from the browser** → graceful, a few seconds; sessions are in the database and sockets reconnect; a
  meeting in progress continues (LiveKit is another process).
- **A bad value that starts but makes the page unusable** → the confirmation of D4.5.
- **The applier itself stopped mid-way** → the state file and the recovery at its start.
- **Fewer settings than the file has** → by design; the page says why each other one is read-only.
- **A large mechanism for a short list** → the root applier, its mailbox rules and its lifecycle are most of this
  change. The alternative that avoids them — the same short list kept in the database and applied without a
  restart, as ad-manager does for its runtime options, the file left alone — works in the container images too
  and is a fraction of the work. Both were put to the owner after the two reviews; the owner chose the file with
  the applier, so `config.toml` stays the one place a setting lives.

## Migration Plan

No migration. Upgrading the package adds and enables the path unit. Rolling back the binary leaves a normal
`config.toml` behind; a pending request is refused by an older applier's absence (the page stays read-only).

## Open Questions

- Whether applying should ask for the owner's password or second factor again.
- An applier for the all-in-one image (supervisord could run it), once this version has run somewhere real.
