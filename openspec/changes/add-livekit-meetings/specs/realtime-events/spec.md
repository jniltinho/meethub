# Spec Delta

## ADDED Requirements

### Requirement: Meeting room events
Whenever the people in a channel's meeting room change (a join, a leave, the room ending, a reconciliation or an
eviction), the server SHALL send `meeting_updated` with the channel id and the room's full participant list (user id
and join time) to the channel's members only, as a channel-scoped event.

#### Scenario: Join announced to members
- **GIVEN** Ana and Bruno are members of `#projects` and Carla is not
- **WHEN** Bruno joins the room of `#projects`
- **THEN** Ana receives `meeting_updated` for `#projects` listing Bruno, and Carla receives nothing

#### Scenario: Recovery after a gap
- **GIVEN** Ana's socket missed a `meeting_updated`
- **WHEN** her client resynchronizes
- **THEN** it reloads the rooms through the meetings API and shows the current participants
