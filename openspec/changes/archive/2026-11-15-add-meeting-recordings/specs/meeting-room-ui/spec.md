# Spec Delta

## ADDED Requirements

### Requirement: Recording control and mark
The room SHALL show a record control to people allowed to record, and to everyone a "Recording" mark with the
elapsed time for as long as a recording runs. Starting SHALL ask for confirmation in a themed dialog that says
everyone will be told; starting and stopping SHALL be announced by a toast and to screen readers.

#### Scenario: Everyone sees the mark
- **WHEN** Ana starts a recording
- **THEN** Ana and Bruno both see "Recording" in the room and Bruno gets a toast "Ana Silva started recording"

#### Scenario: Not allowed
- **GIVEN** Bruno may not record
- **WHEN** he opens the room's controls
- **THEN** there is no record control

#### Scenario: Stopping
- **WHEN** Ana stops the recording and the recorder has not left yet
- **THEN** the mark reads "Stopping the recording…" until the room reports no recording

### Requirement: Notice before joining
The pre-join screen of a room that is being recorded SHALL say so before the person joins.

#### Scenario: Joining a recorded meeting
- **GIVEN** the room of `#projects` is being recorded
- **WHEN** Carla opens its pre-join screen
- **THEN** it says "This meeting is being recorded" above the Join button
