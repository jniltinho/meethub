# Delta for admin-settings

## MODIFIED Requirements

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

## ADDED Requirements

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

## REMOVED Requirements

### Requirement: Editable settings
**Reason**: Its scenario "Recordings retention stays read-only" is reversed by this change; the requirement is
restated as "Settings open to editing" with the retention, the recording paths and the AI settings in the allowlist.
**Migration**: None for installs. Tests named after the old scenario move to "A new retention waits for the
confirmation" and "Sensitive settings need the password again".
