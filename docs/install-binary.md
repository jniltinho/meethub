# Install the binary

MeetHub is one static binary (linux/amd64) with the web app inside; it needs nothing from the system but a
**Redis** server it can reach. This page installs it by hand in `/opt/meethub` under systemd, on any Linux
distribution. On Ubuntu and Debian the [package](install-linux.md) does all of this for you.

## 1. Download

```bash
curl -fLO https://github.com/jniltinho/meethub/releases/latest/download/meethub_linux_amd64.tar.gz
```

```bash
curl -fLO https://github.com/jniltinho/meethub/releases/latest/download/SHA256SUMS
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
sudo tar -C /opt -xzf meethub_linux_amd64.tar.gz
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
sudo install -m 0644 /opt/meethub/meethub.service /etc/systemd/system/meethub.service
```

```bash
sudo systemctl daemon-reload && sudo systemctl enable --now meethub
```

```bash
systemctl status meethub
```

The unit runs `/opt/meethub/bin/meethub server` as `meethub`, from `/opt/meethub`, with the variables of
`meethub.env`, and lets it write only `/opt/meethub/data`.

## 5. First start

Set the address people will open in `/opt/meethub/config.toml` (`[http] public_url`), restart, then read the
one-time setup token and finish the setup in the browser:

```bash
sudo systemctl restart meethub && sudo cat /opt/meethub/data/setup-token
```

Configuration, the command line, backups and what a production install needs are the same as for the package:
see [Configure](install-linux.md#configure) and the [deployment guide](deployment.md).

## Without systemd

To try it in a terminal, as the `meethub` user and from the folder that holds `config.toml`:

```bash
cd /opt/meethub && sudo -u meethub bin/meethub server
```

## Upgrade

Replace the binary and restart; the database migrates itself at startup. Extracting the whole archive again would
overwrite `config.toml` and `meethub.env`, so take only the binary:

```bash
sudo tar -C /opt -xzf meethub_linux_amd64.tar.gz meethub/bin/meethub && sudo systemctl restart meethub
```

## Remove

```bash
sudo systemctl disable --now meethub && sudo rm /etc/systemd/system/meethub.service && sudo systemctl daemon-reload
```

Then, once you are sure you no longer need the messages and files: `sudo rm -rf /opt/meethub && sudo userdel meethub`.
