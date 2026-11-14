# Spec Delta

## ADDED Requirements

### Requirement: LiveKit settings
The `[livekit]` section SHALL hold `url` (the WebSocket URL browsers use) and `api_url` (the HTTP URL the server uses,
defaulting to `url` with `ws`→`http`); `MEETHUB_LIVEKIT_API_KEY` and `MEETHUB_LIVEKIT_API_SECRET` SHALL be accepted
only from the environment or `_FILE`. Validation SHALL require all three together, a secret of at least 32 bytes, and
`wss://` in production.

#### Scenario: Secret in the file
- **WHEN** `config.toml` sets `livekit.api_secret`
- **THEN** startup fails saying secrets must come from `MEETHUB_LIVEKIT_API_SECRET` or `MEETHUB_LIVEKIT_API_SECRET_FILE`

#### Scenario: Incomplete settings
- **GIVEN** `livekit.url` is set and `MEETHUB_LIVEKIT_API_SECRET` is not
- **WHEN** the server starts
- **THEN** it fails naming the missing setting

#### Scenario: Insecure URL in production
- **GIVEN** `app.env` is production
- **WHEN** `livekit.url` is `ws://livekit.example.com`
- **THEN** validation fails asking for `wss://`

#### Scenario: Meetings off
- **WHEN** no LiveKit setting is given
- **THEN** the server starts with meetings disabled
