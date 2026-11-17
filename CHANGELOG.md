# Changelog

What changes for the people who run and use MeetHub, newest first. The format follows
[Keep a Changelog](https://keepachangelog.com/en/1.1.0/); versions are the git tags.

## [Unreleased]

### Added

- **A setup wizard** — the first run is seven steps with a hint under every field: the setup token, a diagnosis of
  the server (database, Redis, file storage, HTTPS, meetings, recordings, e-mail, the key for stored secrets, AI,
  settings from the browser) with what to fix, the organization and the owner, then — optional — meeting
  recordings and AI summaries, a review and a progress list. An owner can reopen it at `/setup`.
  [The setup wizard](docs/install-linux.md#the-setup-wizard).
- **The AI key is entered in Administration** — Settings → Meeting summaries stores the AI service's key (and the
  transcription service's) encrypted with `MEETHUB_SECRET_KEY` in the database: no restart, any kind of install.
  `MEETHUB_AI_API_KEY` still works and then locks the form.
- **More settings in Administration** — the AI service's address, models, language, timeout and limits, and the
  recordings' retention, `ffmpeg` program and capture directory, each with a hint.
  [Meeting summaries: set it up with OpenAI](docs/deployment.md#set-it-up-with-openai) says which models work.

### Changed

- Changing where meeting audio goes (the AI addresses), an AI key, the recordings' retention, `ffmpeg_path` or
  `dir` asks for the owner's **password again**, and a two-factor code when it is on.
- A shorter recordings retention deletes nothing until "Keep these settings" is pressed; the confirmation says how
  many recordings it will delete.
- **Subtler corners** — buttons, fields, cards and dialogs are barely rounded (3 and 4 px instead of 6 and 8) in
  every theme.
- `ai.transcription.base_url` no longer needs `MEETHUB_AI_TRANSCRIPTION_API_KEY` to start: its key may be entered
  in Administration; summaries stay unavailable until it has one.

### Fixed

- Field labels on the login page were hard to read in the themes with a dark login box (default, ad-manager,
  RabbitMQ, Pink Panther): they now use the box's own text color.

## [0.2.0] — 2026-11-15

**Meeting recordings with AI summaries, two-factor authentication and single sign-on, HTTPS without a proxy, and
the everyday settings changed from the browser. Upgrading a package now installs the distribution's `ffmpeg`.**

### Added

- **HTTPS without a proxy** — `http.cert_file` and `http.key_file` make the server speak HTTPS itself with any
  certificate, signed by an authority or self-signed; `http.self_signed = true` generates one and keeps it.
  [Standalone with HTTPS](docs/standalone-https.md) covers both, port 443 and the ports to open.
- **Meetings in the all-in-one image** — LiveKit runs inside the container and meetings are on by default; the
  Compose stack turns on TURN, and `meethub meetings check` tests the key, LiveKit and each TURN server.
- [MeetHub behind nginx](docs/nginx.md): a tested configuration for HTTPS, the WebSocket, LiveKit's signalling and
  TURN on port 443.
- **Two-factor authentication** — add an authenticator app in Settings → Security; logins then ask for its code
  after the password, or for one of ten one-time recovery codes. Administrators reset it for someone who lost their
  phone (`meethub user reset-two-factor` on the server). Needs `MEETHUB_SECRET_KEY`, which `make init` generates.
- **Single sign-on** with one OpenID Connect provider (Google Workspace, Microsoft Entra ID, Keycloak, Authentik,
  Okta, …): a "Sign in with …" button on the login page, accounts matched by verified e-mail and, if you allow it,
  created on first sign-in for your domains (`[oidc]`). `auth.password_login = false` leaves only single sign-on.
- **Meeting recordings** — channel administrators record a meeting from its room (`[recordings]`, off by default):
  everyone sees "Recording" and is told, the pre-join screen warns before entering, and the recording — an `.m4a`
  of the voices, or a `.webm` with the shared screen and the cameras — is posted in the channel with a player and a
  download, and listed in the channel's details. The images carry an LGPL `ffmpeg` built from source; the
  Linux packages use the distribution's: `apt` installs it with the `.deb`, and with it recordings can be MP4 too;
  on AlmaLinux and RHEL install `ffmpeg-free` from EPEL. [Deployment: Recordings](docs/deployment.md#recordings).
- **Meeting summaries written by AI** — with an OpenAI-compatible service configured (`[ai]`: OpenAI, a gateway, or
  your own server such as whisper.cpp) an owner turns on Administration → Settings → Meeting summaries, Manual or Automatic,
  after a connection test. A recording is transcribed by participant and summarized — overview, decisions, action
  items with their owners, topics, open questions, in the meeting's language — as a reply in its thread, labelled as
  written by AI, with the transcript attached. Everyone is told in the room and before joining. Off by default;
  turning it off stops running summaries within seconds. [Deployment: Meeting summaries](docs/deployment.md#meeting-summaries).
- **Settings in Administration** — owners change the file limits, the session lifetimes, the log level and format,
  the single sign-on button's label and the meeting recordings settings (on or off, audio or video, who may
  record, the limits) from Administration → Settings, one tab per group, with Restore default; the change is
  written to `config.toml` keeping its comments, the server restarts, and it is undone unless the owner presses
  "Keep these settings" within two minutes. Settings set by environment variables are shown locked, everything
  else read-only with the reason (secrets only as set or not set). The Linux packages install the root
  `meethub-settings` units that apply the changes; the container images keep the page read-only unless they opt
  in (`compose.settings.yml`, or `MEETHUB_SETTINGS_APPLIER=on` in the all-in-one image).
  [Settings in Administration](docs/admin-settings.md).

### Changed

- `GET /api/v1/ready` reports the process's `boot_id`.
- The files of a release carry its version: `meethub_X.Y.Z_amd64.deb`, `meethub-X.Y.Z-1.x86_64.rpm` and
  `meethub_X.Y.Z_linux_amd64.tar.gz` (0.1.0 published them without it). Download commands that used
  `releases/latest/download/meethub_amd64.deb` need the version now; the install guides show how to find it.
- The documented `docker run` of the all-in-one image publishes LiveKit's ports and stops with
  `--stop-timeout 55`.

## [0.1.0] — 2026-11-14

**The first release: team chat, files and video meetings on your own server, as a container or a Linux package.**

### Added

- **Conversations** — teams, public and private channels, direct and group messages, threads, reactions,
  `@mentions`, Markdown, emoji, pinned messages and favorites, delivered in real time with unread counts, typing
  indicators and presence.
- **Activity** — mentions and replies to your threads in one place.
- **Search** — messages by word or phrase with `from:` and `in:`, on each database's full-text index, with a jump
  to the message in its channel.
- **Notifications** — on the desktop, and by e-mail for what you missed (`[smtp]` settings; off until a host is
  set).
- **Files** — attach by button, drag and drop or paste; image thumbnails and previews; a files panel per channel
  and a Files page; stored on disk or in an S3-compatible bucket.
- **Meetings** through LiveKit — one persistent room per channel and conversation, a camera and microphone check
  before joining, screen sharing at the screen's own resolution, a "You are presenting" banner to change what is
  shared, raise hand, reactions, the elapsed time and the channel's chat beside the call.
- **Administration** — roles (owner, admin, member, guest), user management and an audit log.
- **22 themes**, English, Portuguese (Brazil) and Spanish, keyboard and screen-reader support, phone to 4K; the
  channel details open full screen on phones and tablets.
- **Linux packages** — `meethub_amd64.deb` (Ubuntu 26.04, Debian 13) and `meethub.x86_64.rpm` (AlmaLinux, Rocky
  Linux, RHEL 9 and 10): everything in `/opt/meethub`, a hardened systemd service, the data kept on upgrade and
  removal. `meethub_linux_amd64.tar.gz` holds the same for an install by hand.
- **All-in-one image** — `ghcr.io/jniltinho/meethub:0.1.0-allinone`: the server and Redis in one container, SQLite
  on one volume.
- SQLite, MariaDB or PostgreSQL; several replicas behind a load balancer with Redis as the event bus.

### Security

- Server-side sessions, CSRF protection, rate limits, strict security headers and a locked-down content policy.
- Files are served only to people who can read their channel, always as downloads, never from MeetHub's origin.
- Secrets only through the environment: a secret in `config.toml` is refused.

[Unreleased]: https://github.com/jniltinho/meethub/compare/v0.2.0...HEAD
[0.2.0]: https://github.com/jniltinho/meethub/compare/v0.1.0...v0.2.0
[0.1.0]: https://github.com/jniltinho/meethub/releases/tag/v0.1.0
