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
