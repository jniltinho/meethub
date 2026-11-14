# Tasks

## 1. Server

- [x] 1.1 Add `internal/tlsutil` (load a pair; generate, keep and replace the self-signed one) with tests: the
  self-signed certificate served and reused, a chain signed by a test authority served whole, a wrong key refused
- [x] 1.2 Add `http.cert_file`, `http.key_file` and `http.self_signed` with validation, `TLSFiles` and
  `config.toml.example`; serve TLS from `httpserver.Run`; log the fingerprint; verify the server test over HTTPS
  (HTTP/2, the request's scheme)
- [x] 1.3 Make `meethub healthcheck` probe over HTTPS with the served certificate as its only root; verify against
  the right and a wrong certificate

## 2. Install and documents

- [x] 2.1 Extend `packaging/smoke.sh` with a start over the server's own HTTPS; verify under systemd on Debian 13:
  self-signed on 8443, a signed chain on 443 with the capability drop-in, login, WebSocket, health probe
- [x] 2.2 Write `docs/standalone-https.md` (settings, the two kinds of certificate, port 443, the ports to open)
  and link it from the README and the guides; add the change to `CHANGELOG.md`
- [x] 2.3 Run `make check`, `make test`, `make test-db`, the browser tests and `make test-package`; review with
  Codex
- [x] 2.4 CI green on GitHub

## Workflow follow-up

- Archive with `openspec archive add-native-tls` after review.
