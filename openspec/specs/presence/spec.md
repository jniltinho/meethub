# presence Specification

## Purpose
Defines whether people are available — online, away, offline or do not disturb — and the "is typing" indicator, shared
across server replicas, with stated timing and behavior during Redis outages.

## Requirements

### Requirement: Status values
Each user SHALL have a status `online`, `away`, `offline` or `dnd`. Without a choice, `online`, `away` and
`offline` SHALL follow activity automatically. The user MAY choose (`PUT /api/v1/users/me/status`) `away` or
`offline`, kept while they are connected until they choose another status, or `dnd`, until a chosen time or until
they change it; choosing `online` SHALL return to following activity.

#### Scenario: Do not disturb kept while active
- **GIVEN** Bruno set `dnd` until 18:00
- **WHEN** he keeps using the app
- **THEN** his status stays `dnd` until 18:00, then follows activity again

#### Scenario: Away by choice
- **GIVEN** Ana chose `away`
- **WHEN** she keeps writing messages
- **THEN** the people who can see her see `away`, until she chooses another status

#### Scenario: Appearing offline
- **GIVEN** Ana chose `offline` and is connected
- **WHEN** Bruno loads her status and sends her a direct message
- **THEN** he sees `offline`, and the message reaches her at once

#### Scenario: What the others are told
- **GIVEN** Ana chose `offline` and keeps using the app
- **WHEN** Bruno loads her status
- **THEN** it is `offline`, without saying that it was chosen and without her last activity

#### Scenario: Back to following activity
- **GIVEN** Ana chose `away`
- **WHEN** she chooses `online`
- **THEN** her status is `online` while she is active and `away` after five minutes without activity

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

### Requirement: Status menu
The foot of the sidebar SHALL show the user's own status beside their name and open a menu to choose Online,
Away, Do not disturb — for 30 minutes, 1 hour, 2 hours, until tomorrow or until changed — or Appear offline. The
entry in force SHALL be marked, the choice SHALL show at once in the user's open tabs and to the people who can
see them, and the menu SHALL work by keyboard and screen reader in the three languages.

#### Scenario: Choosing a status
- **WHEN** Ana opens the menu and chooses Away
- **THEN** the foot of her sidebar says Away, and Bruno's sidebar shows her as away without a reload

#### Scenario: Do not disturb for a while
- **WHEN** Ana chooses Do not disturb for 30 minutes
- **THEN** her status is `dnd` with an end 30 minutes ahead, and the menu marks that entry

#### Scenario: Another tab
- **GIVEN** Ana has two tabs open and is in do not disturb for 30 minutes
- **WHEN** she chooses do not disturb for 2 hours in one
- **THEN** the other shows the new end without a reload

#### Scenario: Arrow keys
- **GIVEN** the menu is open with the focus on Online
- **WHEN** Ana presses the down arrow
- **THEN** the focus is on Away, and Enter chooses it

#### Scenario: Closed by the keyboard
- **GIVEN** the menu is open
- **WHEN** Ana presses Escape
- **THEN** it closes and the focus is back on the button that opened it
