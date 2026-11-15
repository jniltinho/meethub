# Install the binary

MeetHub is one static binary (linux/amd64) with the web app inside; it needs nothing from the system but a
**Redis** server it can reach. This page installs it by hand in `/opt/meethub` under systemd, on any Linux
distribution. On Ubuntu and Debian the [package](install-linux.md) does all of this for you.

## 1. Download

The files of each release carry its version; the first line finds the newest (or pick one on the
[releases page](https://github.com/jniltinho/meethub/releases/latest)):

```bash
VERSION=$(curl -fsSL -o /dev/null -w '%{url_effective}' https://github.com/jniltinho/meethub/releases/latest | sed 's|.*/v||')
```

```bash
curl -fLO "https://github.com/jniltinho/meethub/releases/download/v${VERSION}/meethub_${VERSION}_linux_amd64.tar.gz"
```

```bash
curl -fLO "https://github.com/jniltinho/meethub/releases/download/v${VERSION}/SHA256SUMS"
```

```bash
sha256sum -c --ignore-missing SHA256SUMS
```

The archive holds one folder:

```
meethub/
├── bin/meethub        the server and its command line
├── config.toml        the settings, each one commented (data in /opt/meethub/data)
├── meethub.env        the secrets, as NAME=value lines
├── meethub.service    the systemd unit
├── LICENSE
└── NOTICE
```

Building it yourself instead: `make build` writes `dist/meethub` (the bare binary) and `make package` writes the
archive to `dist/packages/` — both need only Docker and `make`.

## 2. Install into /opt/meethub

```bash
sudo tar -C /opt -xzf "meethub_${VERSION}_linux_amd64.tar.gz"
```

Create the user the service runs as, its data directory, and hide the secrets file from everyone else:

```bash
sudo useradd --system --user-group --home-dir /opt/meethub --no-create-home --shell /usr/sbin/nologin meethub
```

```bash
sudo install -d -o meethub -g meethub -m 0750 /opt/meethub/data
```

```bash
sudo chown root:meethub /opt/meethub/meethub.env && sudo chmod 0640 /opt/meethub/meethub.env
```

The settings applier's directory (Administration → Settings, [Settings in Administration](admin-settings.md)):
root writes it, the service reads it.

```bash
sudo install -d -o root -g meethub -m 2750 /opt/meethub/settings-state
```

## 3. Redis

Install the distribution's Redis (or Valkey) and start it, or point MeetHub at one you already run with
`[redis] addr` in `config.toml`:

```bash
sudo apt install redis-server      # Debian, Ubuntu
```

```bash
sudo dnf install valkey && sudo systemctl enable --now valkey      # RHEL family
```

## 4. The service

```bash
sudo install -m 0644 /opt/meethub/meethub.service /opt/meethub/meethub-settings.service /opt/meethub/meethub-settings.path /etc/systemd/system/
```

```bash
sudo systemctl daemon-reload && sudo systemctl enable --now meethub meethub-settings.path && sudo systemctl enable --now meethub-settings.service
```

```bash
systemctl status meethub
```

The unit runs `/opt/meethub/bin/meethub server` as `meethub`, from `/opt/meethub`, with the variables of
`meethub.env`, and lets it write only `/opt/meethub/data`. The two `meethub-settings` units are the settings
applier: it runs as root when an owner applies a change in Administration → Settings, and at boot; without them
that page is read-only.

## 5. First start

Set the address people will open in `/opt/meethub/config.toml` (`[http] public_url`), restart, then read the
one-time setup token and finish the setup in the browser:

```bash
sudo systemctl restart meethub && sudo cat /opt/meethub/data/setup-token
```

Configuration, the command line, backups and what a production install needs are the same as for the package:
see [Configure](install-linux.md#configure) and the [deployment guide](deployment.md). To serve HTTPS from the
binary itself, and for the ports to open: [Standalone with HTTPS](standalone-https.md).

## Without systemd

To try it in a terminal, as the `meethub` user and from the folder that holds `config.toml`:

```bash
cd /opt/meethub && sudo -u meethub bin/meethub server
```

## Upgrade

Download the new archive (step 1), replace the binary and restart; the database migrates itself at startup.
Extracting the whole archive again would overwrite `config.toml` and `meethub.env`, so take only the binary:

```bash
sudo systemctl stop meethub-settings.path meethub-settings.service
```

```bash
sudo tar -C /opt -xzf "meethub_${VERSION}_linux_amd64.tar.gz" meethub/bin/meethub && sudo systemctl restart meethub meethub-settings.path
```

Stopping the settings applier first ends a change it is carrying out (it is undone) before its binary changes.

## Remove

```bash
sudo systemctl disable --now meethub-settings.path meethub-settings.service
```

The settings applier first: a change it is carrying out is undone (and the server restarted) before it stops. Then
the server:

```bash
sudo systemctl disable --now meethub
```

```bash
sudo rm /etc/systemd/system/meethub.service /etc/systemd/system/meethub-settings.service /etc/systemd/system/meethub-settings.path && sudo systemctl daemon-reload
```

Then, once you are sure you no longer need the messages and files: `sudo rm -rf /opt/meethub && sudo userdel meethub`.
