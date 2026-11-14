# Spec Delta

## ADDED Requirements

### Requirement: The recorder is not a participant
The recorder SHALL join a room hidden, able to subscribe and unable to publish, and SHALL never appear in the room's
participant list, in the counts of people in a room, in `meeting_updated` or in the Meetings page; a room with only
the recorder in it SHALL count as empty.

#### Scenario: Count while recording
- **GIVEN** Ana and Bruno are in a room that is being recorded
- **WHEN** a member reads the room
- **THEN** it lists two participants

### Requirement: No recorder without a recording
Reconciliation SHALL remove from a room any recorder whose recording is not running under a live lease.

#### Scenario: Orphan recorder
- **GIVEN** a recorder is still connected to a room whose recording was marked failed
- **WHEN** reconciliation runs
- **THEN** the recorder is disconnected from the room
