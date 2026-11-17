# admin-settings Specification

## Purpose
TBD - created by archiving change add-admin-settings. Update Purpose after archive.

## Requirements

### Requirement: Environment locks
A key whose environment variable is set SHALL be shown with that variable's name and SHALL NOT be changeable.

#### Scenario: Locked by a variable
- **GIVEN** `MEETHUB_FILES_MAX_SIZE_BYTES` is set
- **WHEN** an owner sends a change for `files.max_size_bytes`
- **THEN** the answer is 409 naming the variable and no request is written

### Requirement: The server never writes its configuration
A change SHALL leave the server, once the resulting configuration validates as at startup, as a request; only
the applier, a separate process, SHALL write `config.toml`, after checking the request itself: allowlisted keys
within bounds, the file unchanged since the request, a request never applied before. The service's user SHALL NOT
be able to write `config.toml` or anything the applier writes, and the applier SHALL NOT follow links or build
paths from the request.

#### Scenario: A forged request
- **GIVEN** a request file naming `database.host`
- **WHEN** the applier reads it
- **THEN** it refuses the request, writes nothing and reports why

#### Scenario: A request replayed
- **GIVEN** a request that was applied and rolled back
- **WHEN** the same request file appears again
- **THEN** the applier refuses it and does not restart the server

#### Scenario: A link in the mailbox
- **GIVEN** the result's name in the request directory is a link to another file
- **WHEN** the applier finishes a request
- **THEN** that file is untouched and the result is in the applier's own directory

#### Scenario: The file changed meanwhile
- **GIVEN** an administrator edited `config.toml` after the request was made
- **WHEN** the applier reads the request
- **THEN** it refuses it as stale and `config.toml` keeps the hand edit

### Requirement: Only the changed keys change
Applying SHALL rewrite only the requested keys of `config.toml`, leaving every other line and comment as it was.
Restore default SHALL write the key's built-in default.

#### Scenario: One key changed
- **WHEN** an owner sets the upload limit to 100 MiB and the change is applied
- **THEN** `config.toml` differs from before only in the value of `files.max_size_bytes`, its comment included

#### Scenario: Restore default
- **GIVEN** `files.max_per_post = 25` in the file
- **WHEN** the owner restores its default and the change is applied
- **THEN** the file holds the built-in default and the page shows the value as default

### Requirement: Apply, confirm and roll back
The applier SHALL keep the previous file, replace `config.toml` atomically and durably, restart the server and
wait for it to be ready. The change SHALL stay only if an owner confirms it from the page within two minutes;
otherwise the applier SHALL restore the previous file and restart the server. Each request SHALL end with a
result carrying its id: applied, rolled back or refused, with the reason.

#### Scenario: Applied and kept
- **WHEN** the upload limit is changed, applied, and the owner presses "Keep these settings"
- **THEN** the new value is in effect, people are still logged in, and the result is "applied"

#### Scenario: A shorter session time
- **GIVEN** Bruno was last active 20 minutes ago
- **WHEN** the idle time is set to 10 minutes and applied
- **THEN** the page warned that idle people are logged out, and Bruno is asked to log in again

#### Scenario: The server does not start
- **GIVEN** a change with which the server cannot become ready
- **WHEN** it is applied
- **THEN** within three minutes the server runs with the previous file and the result is "rolled back" with the
  reason

#### Scenario: Nobody confirms
- **GIVEN** a change after which the owner cannot use the page
- **WHEN** two minutes pass without a confirmation
- **THEN** the previous file is back and the server runs with it

#### Scenario: Stopped in the middle
- **GIVEN** the host stopped while the applier was replacing the file
- **WHEN** it starts again
- **THEN** `config.toml` is whole, either the previous or the new one, the transaction is finished or undone, and
  the result says which

#### Scenario: One at a time
- **WHEN** a second change is sent while one is pending
- **THEN** it is refused until the first has its result

#### Scenario: Too many restarts
- **WHEN** a fourth request arrives within an hour
- **THEN** the applier refuses it without restarting the server

#### Scenario: Interrupted and the request gone
- **GIVEN** the host rebooted during a transaction and the request file was deleted
- **WHEN** the host starts
- **THEN** the applier finishes or undoes the transaction from its own state

### Requirement: Where settings can be changed
The Linux packages SHALL install and enable the applier as a root unit separate from the server. Where no applier
runs, the page SHALL show every value read-only and say why. The container images SHALL run without an applier
unless the operator opts in.

#### Scenario: Installed from the package
- **WHEN** an owner applies and keeps a change on an install from the `.deb`
- **THEN** `/opt/meethub/config.toml` holds it, still owned by root and not writable by the service

#### Scenario: A container image
- **GIVEN** the all-in-one image
- **WHEN** an owner opens the settings
- **THEN** every control is disabled and the page says the install is configured by variables

### Requirement: Settings in a container, opt-in
A container install SHALL be able to opt in to editable settings: the applier then runs the server as its child
process and applies, confirms and rolls back as on a package install, on a configuration file in the data volume
that starts as a copy of the mounted `config.toml` (empty without one). A later change of the mounted file SHALL
be reported in the log and not copied. The documents SHALL say that the applier is then the server's own user.

#### Scenario: The all-in-one image, opted in
- **GIVEN** the all-in-one image started with `MEETHUB_SETTINGS_APPLIER=on`
- **WHEN** an owner applies and keeps a change
- **THEN** `/data/config/config.toml` holds it, the server restarted with it, and no process runs as root

#### Scenario: The Compose stack, opted in
- **GIVEN** the Compose stack started with `compose.settings.yml`
- **WHEN** an owner applies a change and nobody keeps it
- **THEN** the previous file is put back and the server restarts with it

#### Scenario: The container stops during a change
- **GIVEN** an opted-in container waiting for the owner's confirmation
- **WHEN** the container is stopped
- **THEN** the previous file is put back, the server is not started again, and the next start runs with it

#### Scenario: The mounted file changes later
- **GIVEN** an opted-in container whose configuration file was already created
- **WHEN** the mounted `config.toml` is edited and the container restarts
- **THEN** the file in the data volume is unchanged and the log says the mounted one is not read again

### Requirement: Access and audit
Only users with `manage_system` SHALL read or change the settings. The audit log SHALL record each request with
the keys and their old and new values, and its result.

#### Scenario: An administrator who is not an owner
- **WHEN** an admin requests `/api/v1/admin/settings`
- **THEN** the answer is 403 and the page is not in their navigation

#### Scenario: Audit trail
- **WHEN** an owner applies and keeps a change
- **THEN** the audit log holds `settings.requested` with the key, its old and new value, and `settings.applied`

### Requirement: Settings page
Administration SHALL have a Settings page for owners: the editable keys by group with their value, default,
source and a one-line hint, a lock on keys set by the environment, Restore default, a count of pending changes, a
themed confirmation before applying, the progress of the restart, "Keep these settings" with the time left, the
result, and the read-only values with their reasons. It SHALL be usable by keyboard and screen reader, in the
three languages, in every theme.

#### Scenario: Confirmation before the restart
- **WHEN** an owner presses "Apply and restart" with two changes pending
- **THEN** a themed dialog names the two settings and says the server restarts

#### Scenario: A hint under a setting
- **WHEN** an owner opens the Meeting summaries tab
- **THEN** `ai.transcription_model` shows a hint naming a model that returns timed segments

### Requirement: Settings page tabs
Each group of settings, the meeting summaries setting and the read-only values SHALL be a tab of the Settings
page, the open one kept in the address. A change made in one tab SHALL stay pending, and be counted on its tab,
while another tab is open.

#### Scenario: A change waiting in another tab
- **GIVEN** an owner changed a value in the Files tab
- **WHEN** they open the Sessions tab
- **THEN** the Files tab shows one pending change, and "Apply and restart" still names it

### Requirement: Settings open to editing
The server SHALL offer for editing only an allowlist of `config.toml` keys, within finite bounds: the file
limits, the session lifetimes, the log level and format, the single sign-on button's label, the meeting
recordings settings (their retention, `ffmpeg_path` and `dir` among them) and the AI settings (the two addresses,
the two models, the language, the timeout and the limits). Every other setting SHALL be read-only with its
reason, and a secret SHALL be reported only as set or not set.

#### Scenario: A key outside the allowlist
- **WHEN** an owner sends a change for `smtp.host`
- **THEN** the answer is 400 naming the key and no request is written

#### Scenario: A value out of bounds
- **WHEN** an owner sets `session.idle_timeout` to one second
- **THEN** the answer is 400 naming the lowest value allowed

#### Scenario: Recordings turned on
- **WHEN** an owner sets `recordings.enabled` to on, applies and keeps the change
- **THEN** `config.toml` holds `enabled = true` in `[recordings]` and meetings can be recorded

#### Scenario: Recordings turned off under a recording
- **GIVEN** a meeting is being recorded, or its recording is still being prepared
- **WHEN** an owner sets `recordings.enabled` to off
- **THEN** the answer is 400 saying a recording is in progress and no request is written

#### Scenario: The capture directory under a recording
- **GIVEN** a meeting is being recorded, or its recording is still being prepared
- **WHEN** an owner sends a change for `recordings.dir`
- **THEN** the answer is 400 saying a recording is in progress and no request is written

#### Scenario: The summary model changed
- **WHEN** an owner sets `ai.summary_model`, applies and keeps the change
- **THEN** `config.toml` holds it in `[ai]` and the next summary is asked of that model

#### Scenario: An AI address that validation refuses
- **WHEN** an owner sets `ai.base_url` to a plain `http://` address that is neither loopback nor private
- **THEN** the answer is 400 with the validation's reason and no request is written

#### Scenario: The AI address with summaries on
- **GIVEN** meeting summaries are Manual or Automatic
- **WHEN** an owner sends a change for `ai.base_url` with their password
- **THEN** the answer is 400 saying to turn summaries off first, and no request is written

#### Scenario: A recording during a change of the capture directory
- **GIVEN** a change of `recordings.dir` awaits "Keep these settings"
- **WHEN** someone starts a recording
- **THEN** the answer is 409 `settings_pending` and nothing is captured

#### Scenario: An ffmpeg that does not work
- **WHEN** an owner sets `recordings.ffmpeg_path` to a program that fails the startup probe and applies
- **THEN** the server does not come back, the applier restores the previous file and the result is rolled back

#### Scenario: A secret is never shown
- **WHEN** an owner opens the settings with `MEETHUB_SMTP_PASSWORD` set
- **THEN** the page says the password is set and no response carries its value

### Requirement: Sensitive settings need the password again
A change that includes an AI address, the recordings' retention, `recordings.ffmpeg_path` or `recordings.dir`,
and every change of an AI key, SHALL carry the owner's current password, and a current authenticator or recovery
code when the owner has two-factor on. Without them, or with wrong ones, nothing SHALL be written; wrong attempts
SHALL count against the login rate limit. An owner whose account has no password cannot make these changes from
the page.

#### Scenario: A stolen session
- **GIVEN** an owner's session without knowledge of their password
- **WHEN** it sends a change of `ai.base_url`
- **THEN** the answer is 403 `reauth_required`, no request is written and `ai.base_url` is unchanged

#### Scenario: With the password
- **WHEN** the owner sends the same change with their current password
- **THEN** the request is accepted and audited as any other

#### Scenario: Two-factor on
- **GIVEN** the owner has two-factor on
- **WHEN** they send the change with the password and no code
- **THEN** the answer is 403 `reauth_required` naming the code

#### Scenario: A setting that is not sensitive
- **WHEN** an owner changes `log.level`
- **THEN** no password is asked

### Requirement: A new retention waits for the confirmation
While a settings change awaits "Keep these settings", recordings SHALL NOT be deleted for their age. The
confirmation before applying a shorter retention SHALL say how many recordings it will delete.

#### Scenario: Rolled back
- **GIVEN** recordings older than seven days exist and the retention is off
- **WHEN** an owner sets `recordings.retention_days` to 7, applies, and the change is rolled back
- **THEN** every recording is still there

#### Scenario: Kept
- **WHEN** the owner presses "Keep these settings"
- **THEN** the recordings older than seven days are deleted at the next expiry run

#### Scenario: Told before applying
- **WHEN** an owner applies a retention that would delete three recordings
- **THEN** the confirmation dialog says three recordings will be deleted and cannot be recovered

### Requirement: AI keys from Administration
An owner SHALL be able to set, replace and remove the AI key and the transcription key in Settings → Meeting
summaries. A key entered there SHALL be kept encrypted in the database, SHALL be used by every server from the
next request on without a restart, and SHALL never be returned, logged or audited. A key from the environment
SHALL win and lock the field. Without `MEETHUB_SECRET_KEY` the form SHALL be unavailable and say why. This works
with or without a settings applier.

#### Scenario: A key entered in the page
- **GIVEN** no `MEETHUB_AI_API_KEY` and both models set
- **WHEN** an owner enters a key with their password
- **THEN** the page shows the key as set, "Test connection" passes with it, and no response carries the key

#### Scenario: The environment wins
- **GIVEN** `MEETHUB_AI_API_KEY` is set
- **WHEN** an owner opens the form
- **THEN** the key is shown locked with the variable's name, and a change answers 409

#### Scenario: Removed
- **GIVEN** summaries are on with a key entered in the page
- **WHEN** an owner removes the key
- **THEN** summaries become unavailable, the setting reads `off` and running summaries stop as when switched off

#### Scenario: No key for stored secrets
- **GIVEN** the server runs without `MEETHUB_SECRET_KEY`
- **WHEN** an owner sends a key
- **THEN** the answer is 503 `secret_key_missing` and nothing is stored

#### Scenario: Two servers
- **GIVEN** two servers on one database
- **WHEN** an owner replaces the key on one
- **THEN** a summary run by the other uses the new key

#### Scenario: A server that cannot read the key
- **GIVEN** summaries are on and one server runs with another `MEETHUB_SECRET_KEY`
- **WHEN** that server takes a queued summary
- **THEN** it leaves the summary queued for another server and cancels nothing

#### Scenario: An unreadable key removed
- **GIVEN** summaries are Automatic and the stored key does not open on this server
- **WHEN** an owner removes the key here
- **THEN** the stored setting becomes `off` in the same transaction, so a key entered later does not resume them untested
