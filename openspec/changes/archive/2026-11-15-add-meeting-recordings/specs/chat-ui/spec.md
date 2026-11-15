# Spec Delta

## ADDED Requirements

### Requirement: Recordings in the channel
A recording message SHALL show a themed player (audio or video) with the duration, play and pause from the keyboard
and a download action; a recording that failed or expired SHALL say so instead. The channel details SHALL list the
channel's most recent recordings, each opening its message.

#### Scenario: Play in the message
- **WHEN** a member presses play on a recording in `#projects`
- **THEN** it plays in the message without leaving the channel

#### Scenario: Failed recording
- **WHEN** a recording could not be saved
- **THEN** its message reads "The recording of this meeting could not be saved" and has no player
