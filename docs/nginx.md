# MeetHub behind nginx

nginx in front of MeetHub ends TLS, sends plain HTTP to HTTPS and passes everything — pages, the API, uploads and
the chat's WebSocket — to the server on `127.0.0.1:8080`. This page is for nginx on the same host as MeetHub
(Ubuntu 26.04, Debian 13; nginx 1.26 or later), with `chat.example.com` as the address people open. Without a
proxy, MeetHub can also serve HTTPS itself: [Standalone with HTTPS](standalone-https.md).

```
browser ──HTTPS 443──▶ nginx ──HTTP──▶ 127.0.0.1:8080  MeetHub
        ──WSS 443────▶ nginx ──WS────▶ 127.0.0.1:7880  LiveKit signalling   (meetings, optional)
        ──UDP/TCP────────────────────▶ 7882/udp, 7881/tcp, 3478/udp  LiveKit media, not through nginx
```

## 1. MeetHub's side

Tell MeetHub its public address and that nginx, on this host, is the only proxy it should believe. In
`config.toml` (`/opt/meethub/config.toml` for the [package](install-linux.md)):

```toml
[app]
env = "production"                       # needs an https:// public_url; cookies become Secure, HSTS is sent

[http]
addr = "127.0.0.1:8080"                  # only nginx reaches it
public_url = "https://chat.example.com"  # the address in the browser, exactly
trusted_proxies = ["127.0.0.1"]          # whose X-Forwarded-For and X-Forwarded-Proto count
```

```bash
sudo systemctl restart meethub
```

In a container the same settings are `MEETHUB_APP_ENV`, `MEETHUB_HTTP_PUBLIC_URL` and
`MEETHUB_HTTP_TRUSTED_PROXIES`. Publish the port on the loopback only (`-p 127.0.0.1:8080:8080`) and trust the
address nginx's connections arrive from, which is Docker's gateway (`172.17.0.1` on the default bridge), not
`127.0.0.1`.

## 2. nginx and a certificate

```bash
sudo apt install nginx certbot python3-certbot-nginx
```

```bash
sudo certbot certonly --nginx -d chat.example.com
```

The name must already point to this host and ports 80 and 443 must be open. certbot stores the certificate in
`/etc/letsencrypt/live/chat.example.com/` and renews it by itself.

## 3. The site

`/etc/nginx/sites-available/meethub`:

```nginx
# /etc/nginx/sites-available/meethub — MeetHub behind nginx

# "upgrade" for a WebSocket handshake, "close" for everything else
map $http_upgrade $connection_upgrade {
    default upgrade;
    ''      close;
}

# plain HTTP only redirects
server {
    listen 80;
    listen [::]:80;
    server_name chat.example.com;
    return 301 https://$host$request_uri;
}

server {
    listen 443 ssl;
    listen [::]:443 ssl;
    http2 on;
    server_name chat.example.com;

    ssl_certificate     /etc/letsencrypt/live/chat.example.com/fullchain.pem;
    ssl_certificate_key /etc/letsencrypt/live/chat.example.com/privkey.pem;

    # the largest attachment (files.max_size_bytes, 50 MiB by default) and the rest of its form
    client_max_body_size 55m;

    location / {
        proxy_pass http://127.0.0.1:8080;
        proxy_http_version 1.1;

        # MeetHub checks these against http.public_url and http.trusted_proxies
        proxy_set_header Host              $host;
        proxy_set_header X-Forwarded-For   $proxy_add_x_forwarded_for;
        proxy_set_header X-Forwarded-Proto $scheme;

        # the chat's WebSocket (/api/v1/websocket)
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

- **One `location` for everything.** The web app, the API and the WebSocket all live under the same origin;
  MeetHub answers `/` with the app and `/api/v1/…` with the API.
- **`Host` and the `X-Forwarded-*` headers.** MeetHub refuses a WebSocket whose `Origin` is not `public_url`, and
  takes the browser's address and the `https` scheme from these headers — only from a proxy listed in
  `trusted_proxies`.
- **`Upgrade` and `Connection`.** Without them the chat connects, fails and keeps showing "Reconnecting…".
- **Timeouts above 30 seconds.** The server pings each WebSocket every 30 seconds; nginx's default of 60 is
  enough, 120 leaves room.
- **`client_max_body_size`.** nginx refuses larger requests itself with a plain `413` page. Raise it together
  with `files.max_size_bytes`.
- **No security headers here.** MeetHub already sends HSTS, the content security policy and the others
  ([Security](security.md)); adding them in nginx as well duplicates them.

## 4. Check

```bash
curl -s https://chat.example.com/api/v1/ready
```

Every check must say `ok`. Then open `https://chat.example.com`, log in and send a message from two browsers: it
must appear in the other one at once, with no "Reconnecting…" banner. In MeetHub's log
(`journalctl -u meethub -f`) the `client_ip` of each request must be the browser's address, not `127.0.0.1`.

## 5. Meetings: LiveKit's signalling

Browsers reach LiveKit in two ways. The **signalling** is a WebSocket and goes through nginx, on a name of its
own; the **media** (audio, video, screen) goes straight to LiveKit's ports and never through nginx.

Get a certificate for the second name (`sudo certbot certonly --nginx -d meet.example.com`) and add
`/etc/nginx/sites-available/meethub-meet` (it reuses the `map` of the first site):

```nginx
# /etc/nginx/sites-available/meethub-meet — LiveKit's signalling for the meetings

server {
    listen 80;
    listen [::]:80;
    server_name meet.example.com;
    return 301 https://$host$request_uri;
}

server {
    listen 443 ssl;
    listen [::]:443 ssl;
    http2 on;
    server_name meet.example.com;

    ssl_certificate     /etc/letsencrypt/live/meet.example.com/fullchain.pem;
    ssl_certificate_key /etc/letsencrypt/live/meet.example.com/privkey.pem;

    location / {
        proxy_pass http://127.0.0.1:7880;
        proxy_http_version 1.1;
        proxy_set_header Host       $host;
        proxy_set_header Upgrade    $http_upgrade;
        proxy_set_header Connection $connection_upgrade;
        proxy_read_timeout 120s;
        proxy_send_timeout 120s;
    }
}
```

```bash
sudo ln -s /etc/nginx/sites-available/meethub-meet /etc/nginx/sites-enabled/ && sudo nginx -t && sudo systemctl reload nginx
```

Then point MeetHub at it — `config.toml`, the key and the secret staying in the environment:

```toml
[livekit]
url = "wss://meet.example.com"           # what the browsers use
api_url = "http://127.0.0.1:7880"        # what the server uses, on this host
```

and open, in the firewall, the ports LiveKit serves directly: `7881/tcp`, `7882/udp` and `3478/udp` (TURN).
LiveKit must know the public address of the host (`LIVEKIT_NODE_IP` in the Compose stack and the all-in-one
image). What each port is for, and the settings of the stack, are in
[Meetings](deployment.md#meetings-livekit).

```bash
cd /opt/meethub && sudo -u meethub sh -c 'set -a; . ./meethub.env; exec bin/meethub meetings check'
```

reports whether the key is accepted and whether each TURN server answers.

## 6. Optional: TURN over TLS on port 443

Some networks allow nothing but outgoing TCP 443. For them LiveKit offers TURN over TLS, which it always
announces on port 443 of a third name (`turn.example.com`). Port 443 then has two kinds of traffic: HTTPS for the
sites, and a raw TLS stream for TURN. nginx separates them by the name the browser asks for, before ending TLS —
in its `stream` module:

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

The two sites now sit behind that listener instead of on port 443. In **both** site files, replace the two
`listen … 443 ssl;` lines and take the browser's address from the PROXY protocol — otherwise every request
seems to come from `127.0.0.1` and everyone shares one login rate limit:

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
| Meetings: "could not connect" while the chat works | `wss://meet.example.com` is not reachable, or the media ports are closed | Section 5: the second site, `livekit.url`, the firewall; `meethub meetings check` |

The same rules apply to any reverse proxy; Traefik and Caddy pass WebSocket upgrades by default. Replicas behind a
load balancer need no sticky sessions ([Deployment](deployment.md#real-time-chat-behind-a-proxy)).
