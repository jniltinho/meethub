# Spec Delta

## ADDED Requirements

### Requirement: Key for stored secrets
Authenticator secrets SHALL be stored encrypted with a key read only from `MEETHUB_SECRET_KEY` (32 bytes, base64);
a key of another length SHALL fail validation, and without the key two-factor SHALL be unavailable while everything
else works.

#### Scenario: No key
- **WHEN** the server starts without `MEETHUB_SECRET_KEY`
- **THEN** it starts, and the two-factor routes answer `503` with code `two_factor_unavailable`

#### Scenario: Bad key
- **WHEN** `MEETHUB_SECRET_KEY` decodes to 16 bytes
- **THEN** startup fails naming the variable, never its value
