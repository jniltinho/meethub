# Spec Delta

## ADDED Requirements

### Requirement: Recordings are audited
Starting and stopping a recording SHALL each record an audit event with the actor, the channel and the recording,
never its content.

#### Scenario: Start recorded
- **WHEN** Ana starts recording the room of `#projects`
- **THEN** an event `meeting.recording_started` names Ana and the channel
