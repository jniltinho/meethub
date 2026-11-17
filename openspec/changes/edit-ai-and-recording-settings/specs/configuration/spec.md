# Delta for configuration

## MODIFIED Requirements

### Requirement: Key for stored secrets
Authenticator secrets, and the AI keys entered in Administration, SHALL be stored encrypted with a key read only
from `MEETHUB_SECRET_KEY` (32 bytes, base64); a key of another length SHALL fail validation, and without the key
two-factor and the AI keys form SHALL be unavailable while everything else works.

#### Scenario: No key
- **WHEN** the server starts without `MEETHUB_SECRET_KEY`
- **THEN** it starts, and the two-factor routes answer `503` with code `two_factor_unavailable`

#### Scenario: Bad key
- **WHEN** `MEETHUB_SECRET_KEY` decodes to 16 bytes
- **THEN** startup fails naming the variable, never its value

#### Scenario: A stored AI key under another secret key
- **GIVEN** an AI key was entered in Administration
- **WHEN** the server starts with a different `MEETHUB_SECRET_KEY`
- **THEN** it starts, the AI key counts as not set and the page says it must be entered again

### Requirement: Settings changed from Administration
The configuration file SHALL remain the only store of settings, except for the AI keys entered in
Administration, which are in the database: a change of any other setting made in Administration SHALL be a
change of `config.toml`, loaded with the same precedence as any other — the environment still wins — and the file
SHALL stay valid for hand editing afterwards. A secret in `config.toml` SHALL still fail validation.

#### Scenario: Hand edit after a change from the page
- **GIVEN** a setting changed from Administration
- **WHEN** an administrator edits another key of `config.toml` by hand and restarts
- **THEN** both values are in effect and the page shows them

#### Scenario: An AI key in the file
- **WHEN** `config.toml` contains `ai.api_key`
- **THEN** startup fails naming `MEETHUB_AI_API_KEY`, as for any secret in the file
