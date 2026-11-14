## ADDED Requirements

### Requirement: Healthcheck over HTTPS
When the server speaks HTTPS itself, `meethub healthcheck` SHALL probe `/api/v1/health` over HTTPS, trusting only
the certificate file the server serves, under the public URL's host.

#### Scenario: Server on its own HTTPS
- **GIVEN** a running server with `http.self_signed = true`
- **WHEN** `meethub healthcheck` runs with the same settings
- **THEN** it prints `ok` and exits 0

#### Scenario: Another certificate on the port
- **WHEN** the port answers with a certificate other than the configured one
- **THEN** the command exits 1
