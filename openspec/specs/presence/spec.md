# presence Specification

## Purpose
Defines whether people are available — online, away, offline or do not disturb — and the "is typing" indicator, shared
across server replicas, with stated timing and behavior during Redis outages.

## Requirements

### Requirement: Status values
Each user SHALL have a status `online`, `away`, `offline` or `dnd`; `online`, `away` and `offline` SHALL follow
activity automatically; `dnd` SHALL be set by the user (`PUT /api/v1/users/me/status`) until a chosen time or until they
change it.

#### Scenario: Do not disturb kept while active
- **GIVEN** Bruno set `dnd` until 18:00
- **WHEN** he keeps using the app
- **THEN** his status stays `dnd` until 18:00, then follows activity again

### Requirement: Automatic status
A user with a live connection SHALL be `online` when active in the last 5 minutes (input in any tab, reported with the
`user_activity` action at most every 30 seconds, or a post) and `away` otherwise; a user without live connections SHALL
be `offline`; a connection is live while its heartbeat is younger than 60 seconds; status changes SHALL be published
within 75 seconds of the cause; state lives in Redis and is shared by every replica.

#### Scenario: Two replicas
- **GIVEN** Ana is connected to replica A
- **WHEN** Bruno, connected to replica B, loads Ana's status
- **THEN** he sees `online`

#### Scenario: Crash without a clean disconnect
- **GIVEN** Ana's browser disappears without closing its WebSocket
- **WHEN** 75 seconds pass
- **THEN** her status is `offline` and `status_change` reached users who can see her

#### Scenario: Last tab closes while another opens elsewhere
- **WHEN** Ana closes her last tab on replica A while opening one on replica B
- **THEN** she ends `online`, never stuck `offline`

### Requirement: Presence during a Redis outage
While Redis is unavailable, status lookups SHALL return the last known statuses marked as stale and no status changes
SHALL be published; after recovery every replica SHALL re-register its live connections and the statuses SHALL converge
within 75 seconds.

#### Scenario: Recovery
- **GIVEN** Redis was down for 2 minutes
- **WHEN** it returns
- **THEN** within 75 seconds every connected user shows `online` or `away` again

### Requirement: Status visibility
A user's status SHALL be visible only to users who share a team (guests: a channel or conversation) with them; the
same rule SHALL govern `status_change` pushes, `POST /api/v1/users/status/ids` (up to 200 ids) and the `get_statuses`
action, with unknown or invisible users omitted.

#### Scenario: Batch lookup
- **WHEN** the sidebar asks the status of 12 visible users
- **THEN** one request returns all 12

#### Scenario: Invisible user
- **WHEN** a guest asks the status of a member they share nothing with
- **THEN** that user is omitted from the answer

### Requirement: Typing indicator
A member typing in a channel or thread SHALL be announced to the channel's other members as `typing` (at most once
every 3 seconds per user and channel), and the indicator SHALL disappear 5 seconds after the last announcement or when
that user's message arrives.

#### Scenario: Typing in a thread
- **WHEN** Carla types a reply in a thread
- **THEN** members viewing that thread see "Carla is typing…", and it disappears when her reply is posted
