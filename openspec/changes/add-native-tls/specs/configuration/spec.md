## ADDED Requirements

### Requirement: TLS settings
`http.cert_file` and `http.key_file` (PEM, set together) SHALL select the certificate the server serves;
`http.self_signed` SHALL, when no certificate is configured, select a certificate generated once in
`<data_dir>/tls`, readable by the server's user only, reused across restarts and replaced when it no longer names
the public URL's host or has under 30 days left. With either on, `http.public_url` SHALL be `https://`. The
fingerprint of the served certificate SHALL be logged at start.

#### Scenario: Certificate without its key
- **WHEN** only `http.cert_file` is set
- **THEN** validation fails saying `http.cert_file` and `http.key_file` must be set together

#### Scenario: TLS with a plain public URL
- **WHEN** `http.self_signed = true` and `http.public_url` starts with `http://`
- **THEN** validation fails saying the public URL must use `https://`

#### Scenario: Generated certificate kept
- **GIVEN** a server that generated its certificate
- **WHEN** it restarts
- **THEN** it serves the same certificate, with the same fingerprint

#### Scenario: Public host changed
- **GIVEN** a generated certificate for `chat.example.com`
- **WHEN** `http.public_url` changes to another host and the server restarts
- **THEN** a new certificate naming that host is generated and a warning is logged
