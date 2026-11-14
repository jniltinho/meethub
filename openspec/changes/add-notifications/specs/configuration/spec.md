# Spec Delta

## ADDED Requirements

### Requirement: SMTP settings
The configuration SHALL have an `smtp` section (`host`, `port`, `username`, `from`, `tls`: `starttls`, `tls` or
`none`) with the password only from `MEETHUB_SMTP_PASSWORD`, and `notifications.email_excerpt` (default true).
Validation SHALL reject a host without a sender address and `tls = "none"` with a password to a host that is not
loopback or private.

#### Scenario: Not configured
- **WHEN** the server starts without `smtp.host`
- **THEN** it starts, sends no e-mail, and Settings says e-mail notifications are not available

#### Scenario: Password in clear
- **WHEN** `smtp.tls = "none"` with a password and a public host
- **THEN** startup fails naming `smtp.tls`
