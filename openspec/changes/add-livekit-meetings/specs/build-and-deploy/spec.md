# Spec Delta

## ADDED Requirements

### Requirement: LiveKit in the Compose stack
The Compose stack SHALL run a LiveKit server (pinned image) next to MeetHub, configured with the same API key and
secret, sending its webhooks to MeetHub, and publishing its signalling and media ports. `make init` SHALL add a
generated LiveKit key and a random secret of 32 bytes to `.env` when missing, so no shared credential is versioned.

#### Scenario: Fresh install
- **GIVEN** a checkout without `.env`
- **WHEN** the operator runs `make up`
- **THEN** `.env` gains `MEETHUB_LIVEKIT_API_KEY` and a random `MEETHUB_LIVEKIT_API_SECRET`, and LiveKit and MeetHub
  start with meetings enabled

#### Scenario: Existing install
- **GIVEN** an `.env` from before this change
- **WHEN** the operator runs `make up`
- **THEN** the LiveKit variables are appended once and the existing values are left untouched
