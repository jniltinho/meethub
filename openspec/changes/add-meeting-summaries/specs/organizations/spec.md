# Spec Delta

## ADDED Requirements

### Requirement: Meeting summaries setting
The organization SHALL have a Meeting summaries setting with the values `off` (default), `manual` and `automatic`,
changed only with `manage_system`, effective on every replica without a restart; while AI is not configured it
SHALL read as `off` and the control SHALL say why it is unavailable.

#### Scenario: Owner turns it on
- **GIVEN** AI is configured and its test passes
- **WHEN** an owner sets Meeting summaries to `automatic`
- **THEN** the next recording that starts is summarized when it ends, without a restart

#### Scenario: Admin may not
- **WHEN** an `admin` changes the setting
- **THEN** the answer is `403` with code `forbidden`

#### Scenario: Not configured
- **GIVEN** no AI key is configured
- **WHEN** an owner opens the setting
- **THEN** it is disabled and says that an AI service must be configured first

### Requirement: The AI service is tested before it is used
Turning Meeting summaries on SHALL first send a short sample MeetHub generates to the transcription endpoint and a short
request to the summary endpoint; the setting SHALL stay `off`, with the reason, unless both answer and the
transcription comes with timed segments.

#### Scenario: No timed transcription
- **GIVEN** a transcription model that answers text without timing
- **WHEN** an owner turns the setting on
- **THEN** it stays `off` and says the service cannot give timed transcription (`ai_unsupported`)

#### Scenario: Wrong key
- **WHEN** the test is refused by the service
- **THEN** the setting stays `off` and says the key was refused, without showing it
