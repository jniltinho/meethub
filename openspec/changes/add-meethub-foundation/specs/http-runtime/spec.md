# Spec Delta

## Purpose

Defines the HTTP surface of the server: the versioned JSON API, health and readiness probes, security headers, error
format and serving the embedded single-page application.

## ADDED Requirements

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
database is reachable, migrations are applied and, if enabled, Redis responds; neither requires authentication.

#### Scenario: Redis down
- **GIVEN** Redis is enabled and unreachable
- **WHEN** `/api/v1/ready` is requested
- **THEN** the response is 503 naming `redis`, while `/api/v1/health` still returns 200

### Requirement: Embedded single-page application
The server SHALL serve the SPA built into the binary, returning `index.html` for unknown non-API paths so client-side
routes work on reload, and serving hashed assets with long-lived cache headers.

#### Scenario: Deep link reload
- **WHEN** a browser requests `/settings/appearance` directly
- **THEN** the response is the SPA's `index.html` with status 200

### Requirement: Security headers
Every response SHALL carry a Content-Security-Policy without `unsafe-inline` scripts, `X-Content-Type-Options:
nosniff`, `Referrer-Policy: same-origin` and `X-Frame-Options: DENY`, and HSTS when served over HTTPS.

#### Scenario: Headers on the SPA
- **WHEN** `/` is requested
- **THEN** the response includes the security headers listed above

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
