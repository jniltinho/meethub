# The server's own HTTPS

## Why

The binary only speaks plain HTTP: an install without Docker needs a reverse proxy just to have TLS, and there is
no way to try HTTPS on a single server with a certificate of its own. The owner asked for the standalone binary
to cover both a self-signed certificate and one signed by an authority.

## What Changes

- `http.cert_file` and `http.key_file` make the server speak HTTPS (TLS 1.2 or later, HTTP/2) on `http.addr`
  with that PEM chain, whoever signed it.
- `http.self_signed = true`, without a certificate, serves HTTPS with a certificate the server generates once
  under `<data_dir>/tls` (the public URL's host, `localhost` and the machine's addresses; 825 days; replaced 30
  days before its end or when the public host changes). Its SHA-256 fingerprint is logged at every start.
- With TLS on, `http.public_url` must be `https://`.
- `meethub healthcheck` probes over HTTPS when the server serves it, trusting only the server's own certificate.
- Documents: running standalone with HTTPS (both kinds of certificate, port 443 under systemd) and the ports to
  open.

Not in scope: obtaining or renewing certificates (ACME), reloading a certificate without a restart, an HTTP
listener that redirects to HTTPS, client certificates, TLS for LiveKit's signalling.

## Capabilities

### New Capabilities

None.

### Modified Capabilities

- `http-runtime`: a new requirement, native HTTPS.
- `configuration`: a new requirement, the TLS settings.
- `server-cli`: the healthcheck command also probes a server that speaks HTTPS.

## Impact

- New `internal/tlsutil`; `internal/config` (three settings, validation), `internal/httpserver` (`Options.TLS`),
  `cmd/server.go`, `cmd/healthcheck.go`, `config.toml.example`, `packaging/smoke.sh`.
- New `docs/standalone-https.md`, links from the other guides.
- Nothing changes for installs that leave the three settings at their defaults.
