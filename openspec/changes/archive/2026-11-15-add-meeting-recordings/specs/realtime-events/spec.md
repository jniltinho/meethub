# Spec Delta

## ADDED Requirements

### Requirement: Recording state in meeting events
`meeting_updated` and the room read through the meetings API SHALL carry `recording`: the id of the running
recording, who started it, when, its mode and whether a stop was requested, or `null`; starting, a stop request and
the actual stop SHALL each send `meeting_updated` with a higher revision.

#### Scenario: Start announced
- **WHEN** Ana starts recording the room of `#projects`
- **THEN** every member of `#projects` receives `meeting_updated` whose `recording.started_by` is Ana

#### Scenario: Stop announced
- **WHEN** the recorder has left the room after a stop request
- **THEN** the members receive `meeting_updated` with `recording: null`

#### Scenario: Stopping
- **WHEN** a stop was requested and the recorder is still in the room
- **THEN** the members receive `meeting_updated` whose `recording.stopping` is true
