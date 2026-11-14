# Spec Delta

## ADDED Requirements

### Requirement: AI summary notice
A meeting recorded while the Meeting summaries setting is `automatic` SHALL show "Recording · AI summary" in the
room and say on the pre-join screen that it is being recorded and will be summarized by AI; under `manual`, the
confirmation to start recording, its announcement and the pre-join screen SHALL say that an AI summary may be
written from the recording.

#### Scenario: Automatic
- **GIVEN** the setting is `automatic`
- **WHEN** a recording starts
- **THEN** everyone in the room sees "Recording · AI summary"

#### Scenario: Manual
- **GIVEN** the setting is `manual`
- **WHEN** Carla opens the pre-join screen of a room being recorded
- **THEN** it says the meeting is being recorded and that an AI summary may be written from it

#### Scenario: Setting off
- **GIVEN** the setting is `off`
- **WHEN** a recording starts
- **THEN** the mark reads "Recording" only and nothing mentions AI
