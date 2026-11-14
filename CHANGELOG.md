# Changelog

What changes for the people who run and use MeetHub, newest first. The format follows
[Keep a Changelog](https://keepachangelog.com/en/1.1.0/); versions are the git tags.

## [Unreleased]

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

### Changed

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

[Unreleased]: https://github.com/jniltinho/meethub/compare/v0.1.0...HEAD
[0.1.0]: https://github.com/jniltinho/meethub/releases/tag/v0.1.0
