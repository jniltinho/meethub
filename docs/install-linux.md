# Install on Ubuntu and Debian (packages)

MeetHub without Docker: a `.deb` that puts everything in `/opt/meethub` and runs it as a systemd service. Written
for **Ubuntu 26.04 LTS** and **Debian 13**, linux/amd64. An `.rpm` with the same contents is published too
([RPM-based distributions](#rpm-based-distributions)); to install the bare binary by hand, see
[Install the binary](install-binary.md).

## What gets installed

| Path | What | Owner, mode |
| --- | --- | --- |
| `/opt/meethub/bin/meethub` | The server and its command line: one static binary with the web app inside | `root`, `0755` |
| `/opt/meethub/config.toml` | The settings, each one commented | `root`, `0644` |
| `/opt/meethub/meethub.env` | The secrets (passwords, keys), as `NAME=value` lines | `root:meethub`, `0640` |
| `/opt/meethub/data/` | The SQLite database, the uploaded files and the first-run setup token | `meethub`, `0750` |
| `/opt/meethub/settings-state/` | The settings applier's own files: the previous `config.toml`, its state and results | `root:meethub`, `2750` |
| `/usr/lib/systemd/system/meethub.service` | The systemd unit | `root`, `0644` |
| `/usr/lib/systemd/system/meethub-settings.service` and `.path` | The settings applier ([Settings in Administration](admin-settings.md)), as root | `root`, `0644` |

The service runs as the system user `meethub`, which the package creates. It may write only
`/opt/meethub/data`; the rest of the system is read-only to it. The units are the only files outside
`/opt/meethub`.

MeetHub needs a **Redis** server. The package recommends `redis-server`, so `apt` installs and starts it on the
same host unless you ask it not to.

Meeting recordings (off by default) are composed with the distribution's **ffmpeg**, which the `.deb` depends on,
so `apt` installs it too. It has `libx264`: recordings can be MP4 (`video_format = "mp4"`) as well as WebM
([Recordings](deployment.md#recordings)).

## Install

The files of each release carry its version (`meethub_X.Y.Z_amd64.deb`); pick one on the
[releases page](https://github.com/jniltinho/meethub/releases/latest), or let the first line find the newest:

```bash
VERSION=$(curl -fsSL -o /dev/null -w '%{url_effective}' https://github.com/jniltinho/meethub/releases/latest | sed 's|.*/v||')
```

```bash
curl -fLO "https://github.com/jniltinho/meethub/releases/download/v${VERSION}/meethub_${VERSION}_amd64.deb"
```

```bash
sudo apt install "./meethub_${VERSION}_amd64.deb"
```

`apt` installs the package and Redis, creates the `meethub` user, and enables and starts `meethub.service`.
To check the download first, fetch `SHA256SUMS` from the same release and run
`sha256sum -c --ignore-missing SHA256SUMS`.

Check that it is up:

```bash
systemctl status meethub
```

```bash
curl -s http://127.0.0.1:8080/api/v1/ready
```

## First start

1. Tell MeetHub the address people will open. In `/opt/meethub/config.toml`:

   ```toml
   [http]
   public_url = "http://meethub.example.com:8080"
   ```

   Connections from any other address are refused, so `localhost` only works on the server itself. Then
   `sudo systemctl restart meethub`.

2. Read the one-time setup token:

   ```bash
   sudo cat /opt/meethub/data/setup-token
   ```

3. Open that address in a browser: [the setup wizard](#the-setup-wizard) takes it from there.

Or create the owner from the command line instead of steps 2 and 3:

```bash
cd /opt/meethub && sudo -u meethub bin/meethub user create --org-name "Example Company Ltd" \
  --email ana@example.com --name "Ana Silva" --role owner
```

## The setup wizard

The first time MeetHub is opened it shows a wizard of seven steps. Every field has a hint, finished steps can be
opened again, and the account exists after the third step: the rest is optional.

| Step | What it does |
| --- | --- |
| 1. Access | Asks for the one-time setup token and shows the command that prints it. The token proves that you run this server; it stops working when the setup ends. |
| 2. This server | A diagnosis of the server's parts, with **Check again**: the database, Redis, the file storage, HTTPS, production mode, meetings (LiveKit), recordings, notification e-mails, the key for stored secrets, AI summaries and whether settings can be changed from the browser. Each is ready, a note (an optional part that is not set up), a warning or a **problem**, with what to change. Only a problem — the database, Redis or the file storage — stops the setup. |
| 3. Organization and account | The organization's name and the owner: name, e-mail, password (twice). **Create organization** creates both and logs you in. |
| 4. Meetings and recordings | Turns meeting recordings on and chooses what is recorded, the video format and who may record. Needs meetings: without LiveKit the step says what to set. |
| 5. AI summaries | The AI service's key (stored encrypted, never shown again), its address, the transcription and summary models, the language, and whether summaries are Off, Manual or Automatic. |
| 6. Review | What will be done, in plain sentences. A key, or the AI address, asks for your password again. |
| 7. Finish | Does it step by step and shows the progress: stores the key, applies the settings — the server restarts once and you press **Keep these settings** — then tests the AI service and turns summaries on. Then **Open MeetHub**. |

If the address in your browser is not the one in `public_url`, the first step says so and names the setting:
MeetHub refuses requests from any other address.

Steps 4 and 5 use the same routes as Administration → Settings, with the same checks, confirmation, rollback and
audit; nothing can be configured with the setup token alone. Where the settings page cannot write `config.toml` —
a container without the [settings applier](admin-settings.md#container-installs-opt-in) — the review shows the
lines to add to `config.toml` instead, with a Copy button; the AI key and the summaries setting are stored either
way. Skip both steps with **Continue** and nothing changes.

An owner can come back to steps 4 to 7 at any time at `/setup`, or change the same things in Administration →
Settings. Once logged in, the diagnosis is at `GET /api/v1/admin/diagnosis`.

## Configure

Settings live in `/opt/meethub/config.toml`; every one is explained there. Secrets are **refused** in that file:
they go in `/opt/meethub/meethub.env`, which only `root` and the `meethub` group can read. Many settings — the
file limits, the session lifetimes, the log level and format, the single sign-on button's label, meeting
recordings and the AI service of meeting summaries — can also be changed by an owner in **Administration →
Settings**, one tab per group with a hint under each setting, which writes them to `config.toml` and restarts the
service; the AI key is entered there too, stored encrypted in the database
([Settings in Administration](admin-settings.md)). After a change
by hand:

```bash
sudo systemctl restart meethub
```

For a real install, follow the [production checklist](deployment.md#production-checklist). In short:

```toml
[app]
env = "production"

[http]
addr = "127.0.0.1:8080"                     # only the reverse proxy on this host reaches it
public_url = "https://meethub.example.com"  # production requires https://
trusted_proxies = ["127.0.0.1"]
```

For TLS, either let MeetHub serve HTTPS itself — a certificate it generates or one you bring, and the ports to
open, in [Standalone with HTTPS](standalone-https.md) — or put a reverse proxy in front, which must allow WebSocket
upgrades: with nginx, follow [MeetHub behind nginx](nginx.md); for the others see
[Real-time chat behind a proxy](deployment.md#real-time-chat-behind-a-proxy). The block above is for a proxy on
the same host.

**PostgreSQL or MariaDB instead of SQLite** — create a database and a user, then:

```toml
[database]
driver = "postgres"  # or "mariadb"
host = "localhost"
name = "meethub"
user = "meethub"
```

and the password in `/opt/meethub/meethub.env`:

```
MEETHUB_DATABASE_PASSWORD='...'
```

Write each value in single quotes when it has anything but letters, digits and `- _ . / : @`: systemd and the
shell (the command line below) both read it literally then.

**Meetings** need a LiveKit server ([Meetings](deployment.md#meetings-livekit)); **attachments in a bucket** and
**notification e-mails** are in the [deployment guide](deployment.md) as well.

**Meeting recordings** — once meetings work, an owner turns them on in the setup wizard or in Administration →
Settings → Recordings (audio or video, WebM or MP4, who may record, the limits, how long they are kept), or in
`[recordings]` of `config.toml`. The server
makes a short test recording with the distribution's `ffmpeg` when it starts and refuses to start if that fails;
from the page, such a change is undone by itself ([Recordings](deployment.md#recordings)).

**Meeting summaries written by AI** — with recordings on, an owner pastes the AI service's key and names the two
models in Administration → Settings → Meeting summaries (OpenAI: `whisper-1` and `gpt-4.1-mini`), tests the
connection and chooses Manual or Automatic: [Set it up with OpenAI](deployment.md#set-it-up-with-openai).

**Data somewhere else** — if you point `app.data_dir` or `files.local.dir` outside `/opt/meethub/data`, allow the
service to write there:

```bash
sudo systemctl edit meethub
```

```ini
[Service]
ReadWritePaths=/srv/meethub
```

## Day to day

| | |
| --- | --- |
| Logs | `journalctl -u meethub -f` |
| Start, stop, restart | `sudo systemctl start\|stop\|restart meethub` |
| Command line | `cd /opt/meethub && sudo -u meethub bin/meethub <command>` — `user list`, `user create`, `migrate status`, `version`… |
| Health | `curl -s http://127.0.0.1:8080/api/v1/health` (alive) and `/api/v1/ready` (database, Redis, file storage) |

The command line reads `config.toml` from the directory it runs in, hence the `cd`. With secrets in
`meethub.env` (a database password, for instance), load them for the command:

```bash
cd /opt/meethub && sudo -u meethub sh -c 'set -a; . ./meethub.env; exec bin/meethub user list'
```

## Upgrade

Download the new `.deb` (the three commands of [Install](#install)) and install it the same way:

```bash
sudo apt install "./meethub_${VERSION}_amd64.deb"
```

The service restarts on the new version and the database migrates itself at startup; a change from
Administration → Settings that was waiting for its confirmation is undone first. Your `config.toml` and
`meethub.env` are kept; if a new version changes the shipped `config.toml` and you edited yours, `apt` asks which
one to keep (keep yours, and compare with the new one it leaves beside it).

## Back up

Stop the service, copy `/opt/meethub` (it holds the settings, the secrets, the SQLite database and the uploaded
files), start it again. The archive holds everyone's messages and the secrets, so it is written readable by
`root` only, into `/root`:

```bash
sudo sh -c 'umask 077; systemctl stop meethub; tar -C /opt -czf /root/meethub-backup.tar.gz meethub; systemctl start meethub'
```

With PostgreSQL or MariaDB, back up that database with its own tools as well.

## Remove

```bash
sudo apt remove meethub
```

This stops the service and removes the binary and the unit. `config.toml`, `meethub.env`, `/opt/meethub/data` and
the `meethub` user stay. `sudo apt purge meethub` also removes the two configuration files. The data is never
deleted by the package; to delete it yourself, once you are sure:

```bash
sudo rm -rf /opt/meethub && sudo userdel meethub
```

## RPM-based distributions

The same tree as an `.rpm`, for AlmaLinux, Rocky Linux and RHEL 9 and 10 (x86_64):

```bash
VERSION=$(curl -fsSL -o /dev/null -w '%{url_effective}' https://github.com/jniltinho/meethub/releases/latest | sed 's|.*/v||')
```

```bash
curl -fLO "https://github.com/jniltinho/meethub/releases/download/v${VERSION}/meethub-${VERSION}-1.x86_64.rpm"
```

```bash
sudo dnf install "./meethub-${VERSION}-1.x86_64.rpm"
```

`dnf` brings Redis or Valkey (whichever the distribution ships) but does not start it — do that once, then start
MeetHub. The base repositories of AlmaLinux and RHEL have no `ffmpeg`, so the package only recommends one: to
record meetings, install `ffmpeg-free` from EPEL (`sudo dnf install epel-release`, then
`sudo dnf install --enablerepo=crb ffmpeg-free`; WebM and audio), or RPM Fusion's `ffmpeg` for MP4 as well.

```bash
sudo systemctl enable --now valkey || sudo systemctl enable --now redis
```

```bash
sudo systemctl restart meethub
```

Everything above applies, with `dnf` in place of `apt` (`sudo dnf remove meethub` keeps the data too). Open the
port if the firewall is on: `sudo firewall-cmd --permanent --add-port=8080/tcp && sudo firewall-cmd --reload`.

## Build the packages yourself

From a checkout, with Docker and `make`:

```bash
make package        # dist/packages: the .deb, the .rpm and the .tar.gz of this version, and SHA256SUMS
make test-package   # installs them in clean Debian, Ubuntu and AlmaLinux containers
```
