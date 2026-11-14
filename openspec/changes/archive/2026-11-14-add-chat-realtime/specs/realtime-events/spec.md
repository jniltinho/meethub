# Spec Delta

## Purpose

Defines the real-time event stream: one authenticated WebSocket per browser tab carrying Mattermost-named events
(baseline Mattermost v12.0.2) to exactly the users allowed to see them, across server replicas, with a recovery path
that works even when events are lost.

## ADDED Requirements

### Requirement: Authenticated connection
`GET /api/v1/websocket` SHALL upgrade only with a valid session cookie and an `Origin` equal to the public URL's origin,
SHALL send `hello` (with the connection id and the server's version) first, and SHALL limit each user to 10 open
connections (the oldest is closed).

#### Scenario: Foreign origin
- **WHEN** a page on another origin opens the WebSocket with the user's cookie
- **THEN** the upgrade is refused with 403

#### Scenario: Eleventh tab
- **GIVEN** a user with 10 open connections
- **WHEN** they open another
- **THEN** their oldest connection is closed

### Requirement: Connections end with their session
Every open connection SHALL be revalidated against the database at least every 5 seconds (session not revoked, not
idle- or absolute-expired, user active) and closed when its session has ended, independently of Redis; a revalidation
that cannot complete for 15 seconds SHALL close the connection (fail closed). Socket activity SHALL count as session
activity (the idle timeout is refreshed at most once a minute).

#### Scenario: Disabled while Redis is down
- **GIVEN** Redis is unavailable and Ana has an open WebSocket
- **WHEN** an administrator disables Ana
- **THEN** her connection is closed within 5 seconds

#### Scenario: Logout, password change, reset and expiry
- **WHEN** a session ends by logout, by its owner changing the password, by an administrator or CLI reset, or by
  reaching its idle or absolute expiry
- **THEN** every connection of that session is closed within 5 seconds, on every replica

### Requirement: Envelopes
Server events SHALL be `{"event", "data", "broadcast", "seq"}` with `seq` increasing by one per connection; client
actions SHALL be `{"action", "seq", "data"}` and SHALL be answered by a response `{"status": "OK"|"FAIL", "seq_reply",
"data"?, "error"?}` as in Mattermost.

#### Scenario: Gap detection
- **WHEN** a client receives `seq` 41 after 39
- **THEN** it knows an event was lost and resynchronizes

### Requirement: Server events
The server SHALL emit `hello`, `posted`, `post_edited`, `post_deleted`, `reaction_added`, `reaction_removed`,
`typing`, `status_change`, `channel_viewed`, `channel_created`, `channel_updated`, `channel_deleted`,
`channel_restored`, `channel_member_updated`, `direct_added`, `group_added`, `user_added`, `user_removed`,
`added_to_team`, `leave_team`, `update_team`, `delete_team`, `restore_team`, `user_role_updated` and `resync`.

#### Scenario: Channel renamed
- **WHEN** a channel admin renames `#devops`
- **THEN** its members receive `channel_updated` with the new name

### Requirement: Client actions
The server SHALL accept the actions `user_typing`, `user_activity`, `get_statuses` and `ping` (answered with `pong`
data), and answer any other action with status `FAIL` and error `unknown_action` without closing the connection.

#### Scenario: Unknown action
- **WHEN** a client sends the action `drop_tables`
- **THEN** it receives a response with status `FAIL` and error `unknown_action`, and the connection stays open

### Requirement: Authorization boundary of delivery
An event about a change SHALL be delivered only to users authorized to read that channel or team at the moment the
change commits; membership changes and the changes they authorize SHALL be serialized per channel, so after a removal
commits the removed user receives no event about any later change; the removed user SHALL receive the event that
removes them (`user_removed`, `leave_team`) and their connections SHALL drop queued events of that channel.

#### Scenario: Removal racing a post
- **GIVEN** Diego is a member of the private `#finance`
- **WHEN** his removal and a new message are committed concurrently
- **THEN** Diego receives the message only if it committed before his removal, and never any later message

#### Scenario: Removed member
- **GIVEN** Diego was removed from `#finance`
- **WHEN** a message is posted there
- **THEN** none of Diego's connections receives it, and he received `user_removed` for `#finance`

### Requirement: Fan-out across replicas
Events SHALL reach connections on every replica through Redis Pub/Sub; a replica SHALL deliver events of its own users
directly even while Redis is unavailable, and SHALL resubscribe on its own.

#### Scenario: Two replicas
- **GIVEN** Ana is connected to replica A and Bruno to replica B, both in `#projects`
- **WHEN** Bruno posts through replica B
- **THEN** Ana receives `posted` through replica A

### Requirement: Resynchronization when events may be lost
The WebSocket SHALL NOT be a durable queue: a replica that lost its Redis subscription SHALL send `resync` to all its
connections once it resubscribes; clients SHALL resynchronize through the API after `resync`, a `seq` gap or a
reconnection; and clients SHALL also resynchronize the open channel and the unread counts at least every 60 seconds
while visible, bounding the staleness caused by a crash between commit and publication.

#### Scenario: Redis subscription lost with sockets open
- **GIVEN** replica A loses its Redis subscription for 10 seconds while its browser sockets stay open, and 3 messages
  are posted through replica B meanwhile
- **WHEN** replica A resubscribes
- **THEN** its clients receive `resync` and show the 3 messages exactly once

#### Scenario: Network drop during messages
- **GIVEN** Bruno's connection drops while 5 messages are posted in his open channel
- **WHEN** he reconnects
- **THEN** his timeline shows the 5 messages exactly once, in order

### Requirement: Backpressure and limits
Each connection SHALL have an outgoing buffer bounded to 256 events and 1 MiB; a client that cannot keep up SHALL be
disconnected (and recover through the API) rather than slow others; incoming frames over 64 KiB or more than 20 actions
per second SHALL close the connection.

#### Scenario: Slow consumer
- **GIVEN** a client that stops reading its socket
- **WHEN** its buffer fills
- **THEN** the server closes that connection and other members keep receiving events without delay

### Requirement: Clean shutdown
On shutdown each replica SHALL stop accepting connections, close its WebSockets with a "going away" status within the
shutdown timeout and stop its background loops.

#### Scenario: Rolling restart
- **WHEN** a replica restarts
- **THEN** its clients reconnect to a replica with backoff and resynchronize without duplicates
