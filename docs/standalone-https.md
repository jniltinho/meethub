# Standalone with HTTPS: certificates and ports

MeetHub can speak HTTPS by itself, with no reverse proxy: give it a certificate, or let it generate a self-signed
one. This page covers both, the port it listens on, and every port a firewall has to let through. It assumes the
binary under systemd in `/opt/meethub` ([package](install-linux.md) or [by hand](install-binary.md)).

Use a reverse proxy instead ([MeetHub behind nginx](nginx.md)) when port 443 is shared with other sites, when
plain `http://` should redirect to `https://`, or for **meetings**: browsers on an HTTPS page only talk to
LiveKit over `wss://`, and LiveKit does not end TLS for its signalling itself. Without a proxy the chat, the
files and everything else work; meetings need one in front of LiveKit.

## 1. The settings

In `/opt/meethub/config.toml`:

```toml
[app]
env = "production"

[http]
addr = ":8443"                                   # every interface, port 8443 (port 443: section 4)
public_url = "https://meethub.example.com:8443"  # what people type; must be https:// once TLS is on
trusted_proxies = []                             # no proxy in front

# one of the two:
self_signed = true                               # a certificate MeetHub generates (section 2)
# cert_file = "/opt/meethub/tls/tls.crt"      # or a certificate you bring (section 3)
# key_file  = "/opt/meethub/tls/tls.key"
```

```bash
sudo systemctl restart meethub
```

A certificate you bring wins over `self_signed`. At every start the log names the certificate in use and its
SHA-256 fingerprint:

```bash
journalctl -u meethub | grep 'serving https'
```

## 2. A self-signed certificate

`self_signed = true` and nothing else. On the first start MeetHub generates a certificate and keeps it:

| | |
| --- | --- |
| Where | `/opt/meethub/data/tls/selfsigned.crt` and `selfsigned.key`, readable by the `meethub` user only |
| Names | The host of `public_url`, `localhost`, the loopback addresses, the machine's name and its addresses |
| Lasts | 825 days. MeetHub looks at it only when it starts: a start within the last 30 days, or after the host of `public_url` changed, generates a new one — the fingerprint changes then, and the log warns. A server that is never restarted keeps the old one until it expires, so restart it within those 30 days |

Nobody trusts a self-signed certificate until told to. Browsers show a warning at each visit, and some refuse
the camera, the microphone and notifications on a page they do not trust. To make a device trust it, copy the
certificate (not the key) to it and add it as a trusted authority:

```bash
sudo cat /opt/meethub/data/tls/selfsigned.crt
```

| Device | Where to add it |
| --- | --- |
| Windows | `certmgr.msc` → Trusted Root Certification Authorities → Import |
| macOS | Keychain Access → System → import, then "Always Trust" |
| Linux | `/usr/local/share/ca-certificates/meethub.crt` and `sudo update-ca-certificates`; Firefox and Chrome also keep their own lists (Settings → Certificates → Authorities) |
| iOS | Install it as a profile, then enable it under Settings → General → About → Certificate Trust Settings |
| Android | Not possible: Android only trusts certificates of an authority, and this one is deliberately not one (its key cannot sign other certificates). Use a signed certificate for Android |

Compare the fingerprint the device shows with the one in MeetHub's log before trusting it. A self-signed
certificate is fine for a first look, a lab or a small team that controls its devices; for anything else use a
signed one.

## 3. A certificate signed by an authority

Any authority works — a public one such as Let's Encrypt, or your organization's own. MeetHub needs two PEM
files:

- **the chain**: the server's certificate first, then the intermediate certificates (`fullchain.pem` in certbot's
  terms). Serving the leaf alone works on some devices and fails on others.
- **the key**, without a passphrase.

Put them where the service can read them and nobody else can:

```bash
sudo install -d -o root -g meethub -m 0750 /opt/meethub/tls
```

```bash
sudo install -o root -g meethub -m 0640 fullchain.pem /opt/meethub/tls/tls.crt
```

```bash
sudo install -o root -g meethub -m 0640 privkey.pem /opt/meethub/tls/tls.key
```

Then set `cert_file` and `key_file` (section 1) and restart. The certificate must name the host of `public_url`.
If the key is not that certificate's, MeetHub refuses to start and says so
(`http.cert_file and http.key_file: tls: private key does not match public key`).

**Let's Encrypt.** certbot can obtain the certificate by itself while port 80 is reachable from the Internet:

```bash
sudo apt install certbot && sudo certbot certonly --standalone -d meethub.example.com
```

MeetHub reads a certificate only when it starts, and certbot's files belong to `root`. A deploy hook copies each
renewed certificate and restarts the service — `/etc/letsencrypt/renewal-hooks/deploy/meethub`, executable:

```sh
#!/bin/sh
set -e
# hooks in this directory run for every certificate certbot renews: only this one is MeetHub's
[ "$RENEWED_LINEAGE" = /etc/letsencrypt/live/meethub.example.com ] || exit 0
install -d -o root -g meethub -m 0750 /opt/meethub/tls
install -o root -g meethub -m 0640 "$RENEWED_LINEAGE/fullchain.pem" /opt/meethub/tls/tls.crt
install -o root -g meethub -m 0640 "$RENEWED_LINEAGE/privkey.pem" /opt/meethub/tls/tls.key
systemctl restart meethub
```

Run it once by hand after the first issue (`sudo RENEWED_LINEAGE=/etc/letsencrypt/live/meethub.example.com
/etc/letsencrypt/renewal-hooks/deploy/meethub`). With any other authority, repeat the three `install` lines and
restart whenever the certificate is renewed.

## 4. Port 443

The service runs as the `meethub` user, which may not listen on ports below 1024. To use the standard HTTPS port,
so the address needs no `:8443`, give the service that one permission:

```bash
sudo systemctl edit meethub
```

```ini
[Service]
AmbientCapabilities=CAP_NET_BIND_SERVICE
CapabilityBoundingSet=CAP_NET_BIND_SERVICE
```

and set `addr = ":443"` and `public_url = "https://meethub.example.com"`, then restart. Without the drop-in the
service keeps restarting with `listen tcp :443: bind: permission denied` in its log.

MeetHub listens on one port only: nothing answers on port 80, so `http://meethub.example.com` does not redirect.
Give people the `https://` address, or put a proxy in front.

## 5. Ports to open

**From the people who use it, to the server** — open these in the firewall:

| Port | Protocol | What | When |
| --- | --- | --- | --- |
| `8443` (or `443`) | TCP | The web app, the API and the chat's WebSocket, all over HTTPS | Always: it is `http.addr` |
| `80` | TCP | Let's Encrypt's check when certbot obtains or renews the certificate | Only with `certbot --standalone`; MeetHub itself does not use it |
| `443` | TCP | LiveKit's signalling (`wss://`) through a proxy, and TURN over TLS | Meetings ([nginx](nginx.md)) |
| `7881` | TCP | Meeting media over TCP, straight to LiveKit | Meetings |
| `7882` | UDP | Meeting media over UDP, straight to LiveKit | Meetings |
| `3478` | UDP | TURN, for people behind strict NATs | Meetings |

**Only on the server itself** — keep these closed to the outside; MeetHub reaches them locally:

| Port | Protocol | What |
| --- | --- | --- |
| `6379` | TCP | Redis |
| `5432` or `3306` | TCP | PostgreSQL or MariaDB (nothing with SQLite) |
| `7880` | TCP | LiveKit's API and plain signalling, behind the proxy |
| `5349` | TCP | LiveKit's TURN over TLS, behind the proxy |

If Redis, the database or LiveKit run on another machine, open their port between the two machines only.

**From the server, outwards** — needed only for what you turn on:

| Port | Protocol | What |
| --- | --- | --- |
| `587`, `465` or `25` | TCP | Notification e-mails (`[smtp]`) |
| `443` | TCP | An S3-compatible bucket for the attachments (`[files.s3]`), Let's Encrypt |
| `53` | UDP/TCP | DNS |

With `ufw` (Ubuntu, Debian), the chat alone on 8443:

```bash
sudo ufw allow 8443/tcp
```

and, with meetings:

```bash
sudo ufw allow 443/tcp && sudo ufw allow 7881/tcp && sudo ufw allow 7882/udp && sudo ufw allow 3478/udp
```

With `firewalld` (RHEL family): `sudo firewall-cmd --permanent --add-port=8443/tcp && sudo firewall-cmd --reload`,
and the same for the others. A cloud provider's security group needs the same list.

## 6. Check

From another machine, a signed certificate verifies by itself:

```bash
curl -s https://meethub.example.com:8443/api/v1/ready
```

A self-signed one verifies against its own file — copy `selfsigned.crt` over, never use `-k` as proof:

```bash
curl -s --cacert selfsigned.crt https://meethub.example.com:8443/api/v1/ready
```

Every check must say `ok`. To see the chain the server sends and when it ends:

```bash
openssl s_client -connect meethub.example.com:8443 -servername meethub.example.com </dev/null | openssl x509 -noout -subject -issuer -enddate
```

On the server, `meethub healthcheck` probes the HTTPS port and accepts only the certificate the server is
configured with (the command loads the service's settings, `meethub.env` included):

```bash
cd /opt/meethub && sudo -u meethub sh -c 'set -a; . ./meethub.env; exec bin/meethub healthcheck'
```

## When something is wrong

| What you see | Cause | Fix |
| --- | --- | --- |
| `http.public_url: must use https:// when the server serves TLS` | TLS is on with an `http://` address | Set the `https://` address, with the port if it is not 443 |
| `http.cert_file: and http.key_file must be set together` | Only one of the two is set | Set both, or neither |
| `tls: private key does not match public key` | The key belongs to another certificate | Use the pair that was issued together |
| `open /opt/meethub/tls/tls.key: permission denied` | The `meethub` user cannot read the files | Section 3: group `meethub`, mode `0640`, directory `0750` |
| `listen tcp :443: bind: permission denied` | Port below 1024 without the permission | Section 4: the drop-in |
| The browser warns on every visit | A self-signed certificate that device does not trust, or a name the certificate does not carry | Section 2; `public_url` must be the name in the certificate |
| "Reconnecting…" in the chat | The address in the browser is not `public_url` (a missing `:8443`, an IP address) | Open exactly `public_url` |
| It works on the server and not from outside | The firewall | Section 5 |
| A renewed certificate is not served | MeetHub reads it at start | Restart the service; the deploy hook of section 3 |
