# http-runtime Specification

## Purpose
Defines the HTTP surface of the server: the versioned JSON API, health and readiness probes, security headers, error
format and serving the embedded single-page application.

## Requirements

### Requirement: Versioned JSON API
All API endpoints SHALL live under `/api/v1`, accept and return JSON, and report errors as
`{"error": {"code": "<machine-code>", "message": "<text>"}}` with a matching HTTP status.

#### Scenario: Unknown API route
- **WHEN** a client requests `GET /api/v1/nope`
- **THEN** the response is 404 JSON with code `not_found`, not the SPA's HTML

#### Scenario: Malformed body
- **WHEN** a client posts invalid JSON to an API endpoint
- **THEN** the response is 400 with code `bad_request`

### Requirement: Health and readiness probes
`GET /api/v1/health` SHALL return 200 while the process runs, and `GET /api/v1/ready` SHALL return 200 only when the
database is reachable, migrations are applied and Redis responds; neither requires authentication.

#### Scenario: Redis down
- **GIVEN** Redis is unreachable
- **WHEN** `/api/v1/ready` is requested
- **THEN** the response is 503 naming `redis`, while `/api/v1/health` still returns 200

### Requirement: Embedded single-page application
The server SHALL serve the SPA built into the binary, returning `index.html` for unknown non-API paths so client-side
routes work on reload, setting `data-theme` on its `<html>` element to the organization's default theme, and serving
hashed assets with long-lived cache headers. A binary built without the SPA SHALL serve a plain "frontend not built"
page instead.

#### Scenario: Deep link reload
- **WHEN** a browser requests `/settings/appearance` directly
- **THEN** the response is the SPA's `index.html` with status 200

### Requirement: Security headers
Every production response SHALL carry the Content-Security-Policy `default-src 'self'; script-src 'self';
style-src 'self'; img-src 'self' data: blob:; font-src 'self'; connect-src 'self'; object-src 'none';
base-uri 'none'; form-action 'self'; frame-ancestors 'none'`, plus `X-Content-Type-Options: nosniff`,
`Referrer-Policy: same-origin` and, when the public URL is HTTPS, HSTS. Later changes extend `connect-src` and
`media-src` only for the configured LiveKit URL.

#### Scenario: Headers on the SPA
- **WHEN** `/` is requested
- **THEN** the response includes exactly that policy and the other headers

#### Scenario: Inline script blocked, app works
- **WHEN** the production SPA loads in a browser with that policy
- **THEN** no CSP violation is reported while booting, applying a theme, loading fonts and calling the API, and an
  injected inline script does not run

#### Scenario: Development exception
- **WHEN** the SPA runs under the Vite dev server (`make dev`)
- **THEN** only that server relaxes the policy for hot reload; the Go server's policy is unchanged

### Requirement: Request logging without sensitive data
The server SHALL log each request as structured JSON with method, path, status, duration and request ID, and SHALL NOT
log request bodies, cookies or authorization headers.

#### Scenario: Login request logged
- **WHEN** a login request is made
- **THEN** its log line has the path and status but no password or cookie value

### Requirement: Request size limits
The server SHALL reject JSON bodies larger than the configured limit (default 1 MiB) with status 413.

#### Scenario: Oversized body
- **WHEN** a client posts a 2 MiB JSON body to `/api/v1/auth/login`
- **THEN** the response is 413 with code `payload_too_large`

### Requirement: Native HTTPS
With a certificate configured or `http.self_signed` on, the server SHALL speak HTTPS on `http.addr` with TLS 1.2
or later and HTTP/2, serving the whole configured chain, and SHALL treat those requests as secure without any
proxy header. Without them it SHALL speak plain HTTP as before. It SHALL refuse to start with a certificate and a
key that do not belong together, naming the two settings.

#### Scenario: Certificate signed by an authority
- **GIVEN** `http.cert_file` holds a leaf and its authority and `http.key_file` the leaf's key
- **WHEN** a client that trusts only that authority requests `/api/v1/ready` under the certificate's name
- **THEN** the TLS handshake succeeds with both certificates sent and the answer is 200

#### Scenario: Self-signed certificate
- **GIVEN** `http.self_signed = true` and no certificate configured
- **WHEN** the server starts for the first time
- **THEN** it serves HTTPS with a certificate it generated for the public URL's host, a client that trusts that
  certificate is answered, and a client that does not refuses the connection

#### Scenario: Login and WebSocket over native HTTPS
- **GIVEN** the server speaks HTTPS itself on the public URL
- **WHEN** a user logs in and the app opens its WebSocket
- **THEN** the session cookie is `Secure` and the upgrade answers 101

#### Scenario: Key of another certificate
- **WHEN** `http.key_file` is not the key of `http.cert_file`
- **THEN** the server exits with an error naming `http.cert_file` and `http.key_file`
