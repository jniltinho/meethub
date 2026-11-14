# Spec Delta

## ADDED Requirements

### Requirement: AI settings
The configuration SHALL have an `ai` section: `base_url` (default `https://api.openai.com/v1`),
`transcription_model`, `summary_model`, `language`, `timeout`, `max_upload_bytes`, `context_tokens`,
`max_output_tokens`, and an optional `ai.transcription.base_url`. The keys SHALL be accepted only from
`MEETHUB_AI_API_KEY` and `MEETHUB_AI_TRANSCRIPTION_API_KEY`. AI is configured when the key and both models are set.

#### Scenario: Key in the file
- **WHEN** `config.toml` contains an AI key
- **THEN** startup fails saying secrets come only from the environment

#### Scenario: Not configured
- **WHEN** the server starts without `MEETHUB_AI_API_KEY`
- **THEN** it starts normally and summaries are unavailable

### Requirement: AI endpoints are validated
Validation SHALL reject a base URL that is not `https://` (except `http://` to loopback and private addresses), one
with credentials, a query or a fragment, and a transcription override without its own key.

#### Scenario: Plain HTTP to the internet
- **WHEN** `ai.base_url = "http://api.example.com/v1"`
- **THEN** startup fails naming `ai.base_url`

#### Scenario: Local server
- **WHEN** `ai.base_url = "http://127.0.0.1:11434/v1"`
- **THEN** the configuration is accepted

#### Scenario: Credentials in the URL
- **WHEN** `ai.base_url = "https://user:secret@api.example.com/v1"`
- **THEN** startup fails naming `ai.base_url`, without printing the URL
