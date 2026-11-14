# MeetHub behind nginx

nginx in front of MeetHub ends TLS, sends plain HTTP to HTTPS and passes everything — pages, the API, uploads,
the chat's WebSocket and the meetings' signalling — to the services on the same host. **One name and one
certificate are enough**: `meethub.example.com` here. This page is for nginx on the same host as MeetHub
(Ubuntu 26.04, Debian 13; nginx 1.26 or later). Without a proxy, MeetHub can also serve HTTPS itself:
[Standalone with HTTPS](standalone-https.md).

```
browser ──https://meethub.example.com/──────────▶ nginx ──▶ 127.0.0.1:8080  MeetHub
        ──wss://meethub.example.com/livekit/────▶ nginx ──▶ 127.0.0.1:7880  LiveKit signalling (meetings)
        ──UDP/TCP────────────────────────────────────▶ 7882/udp, 7881/tcp, 3478/udp  LiveKit media, not through nginx
```

The whole configuration is one file, ready to copy: [`examples/nginx-meethub.conf`](examples/nginx-meethub.conf).
The steps below explain it.

## 1. MeetHub's side

Tell MeetHub its public address and that nginx, on this host, is the only proxy it should believe. In
`config.toml` (`/opt/meethub/config.toml` for the [package](install-linux.md)):

```toml
[app]
env = "production"                          # needs an https:// public_url; cookies become Secure, HSTS is sent

[http]
addr = "127.0.0.1:8080"                     # only nginx reaches it
public_url = "https://meethub.example.com"  # the address in the browser, exactly
trusted_proxies = ["127.0.0.1"]             # whose X-Forwarded-For and X-Forwarded-Proto count
```

With meetings, LiveKit's address for browsers is a path of the same name:

```toml
[livekit]
url = "wss://meethub.example.com/livekit"  # what the browsers use, through nginx
api_url = "http://127.0.0.1:7880"          # what the server uses, on this host
```

```bash
sudo systemctl restart meethub
```

In a container the same settings are `MEETHUB_APP_ENV`, `MEETHUB_HTTP_PUBLIC_URL`,
`MEETHUB_HTTP_TRUSTED_PROXIES`, `MEETHUB_LIVEKIT_URL` and `MEETHUB_LIVEKIT_API_URL`. Publish the port on the loopback only (`-p 127.0.0.1:8080:8080`) and trust the
address nginx's connections arrive from, which is Docker's gateway (`172.17.0.1` on the default bridge), not
`127.0.0.1`.

## 2. nginx and a certificate

```bash
sudo apt install nginx certbot python3-certbot-nginx
```

```bash
sudo certbot certonly --nginx -d meethub.example.com
```

The name must already point to this host and ports 80 and 443 must be open. certbot stores the certificate in
`/etc/letsencrypt/live/meethub.example.com/` and renews it by itself.

## 3. The site

`/etc/nginx/sites-available/meethub` — the same file as [`examples/nginx-meethub.conf`](examples/nginx-meethub.conf):

```nginx
# MeetHub behind nginx: one site, one name, one certificate.
#
#   https://meethub.example.com/           the web app, the API and the chat's WebSocket   → 127.0.0.1:8080
#   wss://meethub.example.com/livekit/     LiveKit's signalling, for the meetings          → 127.0.0.1:7880
#
# Copy to /etc/nginx/sites-available/meethub, replace meethub.example.com (4 places), then:
#   sudo ln -s /etc/nginx/sites-available/meethub /etc/nginx/sites-enabled/
#   sudo nginx -t && sudo systemctl reload nginx
#
# MeetHub's side (config.toml): app.env = "production", http.addr = "127.0.0.1:8080",
# http.public_url = "https://meethub.example.com", http.trusted_proxies = ["127.0.0.1"],
# livekit.url = "wss://meethub.example.com/livekit", livekit.api_url = "http://127.0.0.1:7880".
# The whole walk-through: docs/nginx.md.

# "upgrade" for a WebSocket handshake, "close" for everything else
map $http_upgrade $connection_upgrade {
    default upgrade;
    ''      close;
}

# plain HTTP only redirects
server {
    listen 80;
    listen [::]:80;
    server_name meethub.example.com;
    return 301 https://$host$request_uri;
}

server {
    listen 443 ssl;
    listen [::]:443 ssl;
    http2 on;
    server_name meethub.example.com;

    ssl_certificate     /etc/letsencrypt/live/meethub.example.com/fullchain.pem;
    ssl_certificate_key /etc/letsencrypt/live/meethub.example.com/privkey.pem;

    # the largest attachment (files.max_size_bytes, 50 MiB by default) and the rest of its form
    client_max_body_size 55m;

    # meetings: LiveKit's signalling. The trailing slash of proxy_pass drops the /livekit prefix.
    # Leave this block out when the install has no meetings.
    location /livekit/ {
        proxy_pass http://127.0.0.1:7880/;
        proxy_http_version 1.1;
        proxy_set_header Host       $host;
        proxy_set_header Upgrade    $http_upgrade;
        proxy_set_header Connection $connection_upgrade;
        proxy_read_timeout 120s;
        proxy_send_timeout 120s;
    }

    # everything else: the web app, the API, uploads and the chat's WebSocket (/api/v1/websocket)
    location / {
        proxy_pass http://127.0.0.1:8080;
        proxy_http_version 1.1;

        # MeetHub checks these against http.public_url and http.trusted_proxies
        proxy_set_header Host              $host;
        proxy_set_header X-Forwarded-For   $proxy_add_x_forwarded_for;
        proxy_set_header X-Forwarded-Proto $scheme;

        proxy_set_header Upgrade    $http_upgrade;
        proxy_set_header Connection $connection_upgrade;

        # longer than the server's 30-second WebSocket ping, and long enough for a slow upload
        proxy_read_timeout 120s;
        proxy_send_timeout 120s;

        # uploads go straight to MeetHub instead of being written to nginx's disk first
        proxy_request_buffering off;
    }
}
```

Enable it, check and reload:

```bash
sudo ln -s /etc/nginx/sites-available/meethub /etc/nginx/sites-enabled/
```

```bash
sudo nginx -t && sudo systemctl reload nginx
```

What matters in it:

- **Two `location` blocks, one name.** `/livekit/` goes to LiveKit, everything else to MeetHub: the web app, the
  API (`/api/v1/…`) and the chat's WebSocket live under the same origin.
- **`Host` and the `X-Forwarded-*` headers.** MeetHub refuses a WebSocket whose `Origin` is not `public_url`, and
  takes the browser's address and the `https` scheme from these headers — only from a proxy listed in
  `trusted_proxies`.
- **`Upgrade` and `Connection`.** Without them the chat connects, fails and keeps showing "Reconnecting…", and
  meetings do not connect.
- **Timeouts above 30 seconds.** The server pings each WebSocket every 30 seconds; nginx's default of 60 is
  enough, 120 leaves room.
- **`client_max_body_size`.** nginx refuses larger requests itself with a plain `413` page. Raise it together
  with `files.max_size_bytes`.
- **The trailing slash in `proxy_pass http://127.0.0.1:7880/;`.** It removes the `/livekit` prefix, which
  LiveKit does not know about.
- **No security headers here.** MeetHub already sends HSTS, the content security policy and the others
  ([Security](security.md)); adding them in nginx as well duplicates them.

## 4. Check

```bash
curl -s https://meethub.example.com/api/v1/ready
```

Every check must say `ok`. Then open `https://meethub.example.com`, log in and send a message from two browsers: it
must appear in the other one at once, with no "Reconnecting…" banner. In MeetHub's log
(`journalctl -u meethub -f`) the `client_ip` of each request must be the browser's address, not `127.0.0.1`.

## 5. Meetings: what does not go through nginx

Browsers reach LiveKit in two ways. The **signalling** is a WebSocket and goes through nginx, on
`wss://meethub.example.com/livekit` (the `location /livekit/` above, and `livekit.url` in section 1). The **media**
(audio, video, screen) goes straight to LiveKit's ports and never through nginx — open them in the firewall:

| Port | Protocol | What |
| --- | --- | --- |
| `7882` | UDP | Media |
| `7881` | TCP | Media, when UDP is blocked |
| `3478` | UDP | TURN, for people behind strict NATs |

LiveKit must also know the public address of the host (`LIVEKIT_NODE_IP` in the Compose stack and the all-in-one
image). What each setting is for is in [Meetings](deployment.md#meetings-livekit); every port of an install is
listed in [Standalone with HTTPS](standalone-https.md#5-ports-to-open).

```bash
cd /opt/meethub && sudo -u meethub sh -c 'set -a; . ./meethub.env; exec bin/meethub meetings check'
```

reports whether the key is accepted and whether each TURN server answers.

A name of its own for LiveKit (`wss://livekit.example.com`, a second `server` block and certificate) works as well;
it is only needed when LiveKit runs on another machine.

## 6. Optional: TURN over TLS on port 443

Some networks allow nothing but outgoing TCP 443. For them LiveKit offers TURN over TLS, which it always
announces on port 443 of a name of its own — this is the one case that needs a **second name**
(`turn.example.com`, pointing to the same host). Port 443 then has two kinds of traffic: HTTPS for the site, and
a raw TLS stream for TURN. nginx separates them by the name the browser asks for, before ending TLS — in its
`stream` module:

```bash
sudo apt install libnginx-mod-stream && sudo certbot certonly --nginx -d turn.example.com
```

`/etc/nginx/stream.d/meethub-turn.conf` (create the directory):

```nginx
# /etc/nginx/stream.d/meethub-turn.conf — port 443 shared by the web sites and TURN over TLS

# the name the browser asks for decides where the connection goes, before any TLS is ended
map $ssl_preread_server_name $meethub_upstream {
    turn.example.com turn_tls;
    default          https_sites;
}
upstream turn_tls    { server 127.0.0.1:5350; }
upstream https_sites { server 127.0.0.1:8443; }

server {
    listen 443;
    listen [::]:443;
    ssl_preread on;
    proxy_pass $meethub_upstream;
    proxy_protocol on;      # carries the browser's address to the two listeners below
}

# TURN: ends TLS with the certificate of the TURN name, then plain TCP to LiveKit
server {
    listen 127.0.0.1:5350 ssl proxy_protocol;
    ssl_certificate     /etc/letsencrypt/live/turn.example.com/fullchain.pem;
    ssl_certificate_key /etc/letsencrypt/live/turn.example.com/privkey.pem;
    proxy_pass 127.0.0.1:5349;
    proxy_timeout 1h;
}
```

At the end of `/etc/nginx/nginx.conf`, outside the `http` block:

```nginx
stream {
    include /etc/nginx/stream.d/*.conf;
}
```

The site now sits behind that listener instead of on port 443. In `/etc/nginx/sites-available/meethub`, replace
the two `listen … 443 ssl;` lines and take the browser's address from the PROXY protocol — otherwise every
request seems to come from `127.0.0.1` and everyone shares one login rate limit:

```nginx
    listen 127.0.0.1:8443 ssl proxy_protocol;
    http2 on;
    set_real_ip_from 127.0.0.1;
    real_ip_header   proxy_protocol;
```

```bash
sudo nginx -t && sudo systemctl reload nginx
```

Set `LIVEKIT_TURN_DOMAIN=turn.example.com` for LiveKit ([Meetings](deployment.md#meetings-livekit)) and run
`meethub meetings check` again: it must list a `turns:turn.example.com:443` server that answers.

## When something is wrong

| What you see | Cause | Fix |
| --- | --- | --- |
| "Reconnecting…" that never goes away; `403` on `/api/v1/websocket` | The address in the browser is not `http.public_url` (another name, `http://`, a port) | Make `public_url` exactly what people open, restart MeetHub |
| The same, with `400` or `426` in nginx's log | The `Upgrade` and `Connection` headers are not passed | Section 3: the two `proxy_set_header` lines and `proxy_http_version 1.1` |
| MeetHub does not start: `http.public_url: must use https:// when app.env is production` | `env = "production"` with an `http://` address | Set the `https://` address |
| The login page reloads without logging in | The site is opened over `http://` while cookies are `Secure` | Open the `https://` address; the port 80 server must redirect |
| `client_ip` is `127.0.0.1` in the log; "too many attempts" for everyone at once | nginx's address is not in `trusted_proxies`, or (section 6) the PROXY protocol lines are missing | List the address nginx connects from; add `set_real_ip_from` and `real_ip_header` |
| A plain "413 Request Entity Too Large" page on an upload | The file is larger than `client_max_body_size` | Raise it together with `files.max_size_bytes` |
| `502 Bad Gateway` | MeetHub is not running, or listens elsewhere | `systemctl status meethub`; `http.addr` and `proxy_pass` must agree |
| Meetings: "could not connect" while the chat works | `livekit.url` is not `wss://<the name>/livekit`, the `location /livekit/` block is missing, or the media ports are closed | Sections 1, 3 and 5; `meethub meetings check` |

The same rules apply to any reverse proxy; Traefik and Caddy pass WebSocket upgrades by default. Replicas behind a
load balancer need no sticky sessions ([Deployment](deployment.md#real-time-chat-behind-a-proxy)).
