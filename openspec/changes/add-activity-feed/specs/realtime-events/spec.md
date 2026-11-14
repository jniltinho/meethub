# Spec Delta

## ADDED Requirements

### Requirement: Activity events
The server SHALL send `activity_updated` with `{unseen}` only to the connections of the person it concerns.

#### Scenario: Only to the person
- **WHEN** Bruno mentions Ana in a channel that Carla also reads
- **THEN** Ana receives `activity_updated` and Carla does not
