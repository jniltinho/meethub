# Settings in Administration, written to config.toml

## Why

Every setting of MeetHub lives in `config.toml` or in the environment: changing the upload limit or the session
timeout means a shell on the server, an editor and a restart. The owner asked for a Settings tab in
Administration, as ad-manager has: the simple settings changed from the browser, written to the file, with a way
back to the default.

## What Changes

- **Administration → Settings** (owners only) lists the settings that are safe to change from the browser: the
  file limits, the session lifetimes, the log level and format, and the label of the single sign-on button. Each shows its value, its default and where the value comes from, with **Restore default**.
- **Apply** changes those keys in `config.toml` itself — the other lines and every comment stay as they are. The
  server does not write the file: it leaves a request that a small **applier** outside the server carries out
  (as ad-manager's agent does), after validating the whole resulting configuration. The applier keeps the
  previous file, replaces the file atomically, restarts the server and waits for it.
- **Keep or go back**: after the restart the page asks the owner to confirm. Without a confirmation in two
  minutes — the server did not start, or the owner can no longer reach the page — the applier puts the previous
  file back and restarts again.
- A setting defined by an environment variable is shown **locked**, with the variable's name.
- Everything else — addresses, TLS, the database, Redis, storage, the mail server, the LiveKit and single sign-on
  addresses and account creation, password login, the trusted proxies, the e-mail excerpt — is shown **read-only**, with whether each secret is set (never
  its value).
- The Linux packages gain the applier as a second, root-owned unit; `config.toml` stays root's and read-only to
  the service. Where there is no applier (the container images, configured by variables), the page is read-only
  and says so.

Not in scope: editing anything that names where secrets or people's data are sent (mail server, single sign-on
provider, LiveKit, proxies), anything whose mistake cannot be undone from the page (address, TLS, database, public
URL, password login, body limit), applying without a restart, other replicas, an applier for the container
images.

## Capabilities

### New Capabilities

- `admin-settings`: the editable settings, the request, the applier, confirmation and rollback, the environment
  locks, the read-only view, the API and the page.

### Modified Capabilities

- `configuration`: `config.toml` may be rewritten, for the allowlisted keys, by the applier.

## Impact

- New: `internal/config` (allowlist, comment-preserving edit of a TOML text), `internal/settings` (request,
  apply, confirm, rollback), the `meethub settings-applier` command, `/api/v1/admin/settings`,
  `frontend/src/views/AdminSettingsView.vue`, `packaging/meethub-settings.service` and `.path`, a page in `docs/`.
- Changed: `packaging/` (the second unit in the packages and their scripts), the audit actions, the i18n catalogs.
- No database migration: the file is the store.
