# meetings Specification

## Purpose
Lets the people who may read a channel or conversation meet in its persistent LiveKit room, with the server deciding
who may join, keeping who is in each room, and removing anyone who loses access.

## Requirements

### Requirement: Meetings are enabled by configuration
Meetings SHALL be enabled only when the LiveKit URL, API key and API secret are configured. When disabled, every
meetings route except the list SHALL answer 503 `meetings_unavailable`, and `GET /api/v1/meetings` SHALL answer
`enabled: false` with no rooms.

#### Scenario: No LiveKit configured
- **GIVEN** a server started without LiveKit settings
- **WHEN** a member asks for a meeting token for `#projects`
- **THEN** the answer is 503 with the code `meetings_unavailable`

#### Scenario: The list says meetings are off
- **GIVEN** a server started without LiveKit settings
- **WHEN** a user lists meetings
- **THEN** the answer is 200 with `enabled: false` and an empty room list

### Requirement: One persistent room per channel
Every channel and conversation SHALL have exactly one meeting room, created on first use and kept for the channel's
lifetime. The room's LiveKit name SHALL be a server-generated random value of at least 128 bits, never derived from
the channel's id or name, and clients SHALL never choose or send a room name.

#### Scenario: Same room every time
- **GIVEN** Ana obtained a token for `#projects`
- **WHEN** Bruno obtains a token for `#projects` later
- **THEN** both tokens grant the same room

#### Scenario: Concurrent first use
- **WHEN** two members ask for the first token of a channel at the same time
- **THEN** both tokens grant the same room and only one room exists for the channel

#### Scenario: Rooms differ by channel
- **WHEN** a member obtains tokens for `#projects` and `#infra`
- **THEN** the tokens grant different room names, neither containing the channel's id or name

### Requirement: Access tokens only for readers
`POST /api/v1/channels/{id}/meeting/token` SHALL issue a LiveKit access token only to a caller who may read the
channel (the chat policy's read rule), whose channel is not archived. Refusals SHALL follow the chat's rules: 404 when
the channel may not be discovered, 403 when it may be discovered but not read, 403 `channel_archived` when archived.

#### Scenario: Member gets a token
- **GIVEN** Ana is a member of `#projects`
- **WHEN** she asks for a meeting token for `#projects`
- **THEN** the answer is 200 with the LiveKit URL, the token and its expiry

#### Scenario: Non-member of a public channel
- **GIVEN** Bruno is in the team but not in the public channel `#infra`
- **WHEN** he asks for a meeting token for `#infra`
- **THEN** the answer is 403

#### Scenario: Private channel of others
- **GIVEN** Carla is not a member of the private channel `#board`
- **WHEN** she asks for a meeting token for `#board`
- **THEN** the answer is 404, as if the channel did not exist

#### Scenario: Conversation of others
- **WHEN** a user asks for a token for a direct conversation between two other people
- **THEN** the answer is 404

#### Scenario: Archived channel
- **GIVEN** `#old` is archived and Ana is a member
- **WHEN** she asks for a meeting token for `#old`
- **THEN** the answer is 403 with the code `channel_archived`

### Requirement: Token contents
A token SHALL expire 2 minutes after issue, SHALL name the caller's user id as the participant identity, and SHALL
grant joining only that channel's room, publishing and subscribing to audio, video and screen share, sending data
messages and updating the caller's own attributes. It SHALL NOT grant room administration, recording or hidden
participation. Tokens and the API secret SHALL never be logged or audited.

#### Scenario: Claims of a token
- **WHEN** Ana obtains a token for `#projects`
- **THEN** its identity is Ana's user id, its room is the room of `#projects`, it expires within 2 minutes and it
  has no `roomAdmin`, `roomRecord`, `roomCreate` or `hidden` grant

### Requirement: Room presence from LiveKit webhooks
`POST /api/v1/meetings/webhook` SHALL accept only requests whose LiveKit signature verifies with the configured key
and secret and whose body matches the signed hash (401 otherwise). Participant joins and leaves and room ends SHALL
update who is in the room; events for unknown rooms SHALL be acknowledged and ignored.

#### Scenario: Unsigned webhook
- **WHEN** a request without a valid LiveKit signature posts a `participant_joined` event
- **THEN** the answer is 401 and nobody is added to any room

#### Scenario: Join and leave
- **GIVEN** a signed `participant_joined` for Ana in the room of `#projects`
- **WHEN** a signed `participant_left` for the same participant session arrives
- **THEN** Ana is listed in the room between the two and not after the second

#### Scenario: Stale leave of a replaced session
- **GIVEN** Ana joined from a second tab, replacing her first participant session
- **WHEN** the `participant_left` of the first session arrives after the second session's join
- **THEN** Ana is still listed in the room

#### Scenario: Room finished
- **WHEN** a signed `room_finished` arrives for the room of `#projects`
- **THEN** nobody is listed in that room

### Requirement: Reconciliation and eviction
At least every 10 seconds the server SHALL remove from each room, in LiveKit and in its list, every participant who
may no longer read the channel (removed or left, left the team, deactivated, channel archived), and at least every
minute SHALL replace the list of each room it believes occupied with LiveKit's own participant list.

#### Scenario: Removed from the channel during a meeting
- **GIVEN** Bruno is in the room of `#projects`
- **WHEN** an administrator removes him from `#projects`
- **THEN** within 10 seconds LiveKit is told to remove Bruno from that room and he is no longer listed

#### Scenario: Deactivated during a meeting
- **GIVEN** Carla is in the room of `#projects`
- **WHEN** an administrator deactivates her account
- **THEN** within 10 seconds she is removed from the room

#### Scenario: Lost leave webhook
- **GIVEN** the server listed Diego in a room but LiveKit no longer has him
- **WHEN** the next reconciliation runs
- **THEN** Diego is no longer listed and the channel's members are told

### Requirement: Meetings API
`GET /api/v1/meetings` SHALL list the rooms that have people in them among the channels and conversations the caller
may read, each with its channel, team and participants (user id and join time). `GET /api/v1/channels/{id}/meeting`
SHALL return one room under the token's read rule.

#### Scenario: Only readable rooms
- **GIVEN** people are in the rooms of `#projects` (Ana is a member) and of the private `#board` (she is not)
- **WHEN** Ana lists meetings
- **THEN** only the room of `#projects` is listed

#### Scenario: Empty room
- **GIVEN** nobody is in the room of `#projects`
- **WHEN** a member asks for that room
- **THEN** the answer is 200 with an empty participant list
