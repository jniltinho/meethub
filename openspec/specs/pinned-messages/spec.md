# pinned-messages Specification

## Purpose
Lets the members of a channel or conversation pin its important messages, so everyone finds them at the top of the
channel and in its details, following Mattermost's pinned posts.

## Requirements

### Requirement: Pinning and unpinning
`POST /api/v1/posts/{id}/pin` and `POST /api/v1/posts/{id}/unpin` SHALL set or clear the pin of a message for every
member of its channel and answer `200` with the message, whose projection carries `is_pinned` and, while pinned,
`pinned_by` (user id) and `pinned_at` (ms). Repeating a call SHALL change nothing and keep the first pin's author and
time.

#### Scenario: Pin a message
- **GIVEN** Ana and Bruno are members of `#projects`
- **WHEN** Ana pins Bruno's message
- **THEN** the message has `is_pinned: true`, `pinned_by` is Ana and Bruno sees it pinned

#### Scenario: Pin twice
- **GIVEN** Ana pinned a message
- **WHEN** Bruno pins the same message
- **THEN** the answer is `200` and `pinned_by` is still Ana

### Requirement: Who may pin
Any member who may post in the channel SHALL be able to pin and unpin its messages, replies included. A caller who
cannot read the channel SHALL get `404`, an archived channel `403 channel_archived` and a deleted message `404`.

#### Scenario: Not a member
- **WHEN** Carla, who is not a member of private `#finance`, pins one of its messages by id
- **THEN** the answer is `404` and nothing changes

#### Scenario: Archived channel
- **WHEN** a member pins a message of an archived channel
- **THEN** the answer is `403` with code `channel_archived`

### Requirement: Pinned list
`GET /api/v1/channels/{id}/pinned` SHALL return the channel's pinned messages to its members, most recently pinned
first, paged by `cursor` with `per_page` (default 60, max 200), in the same projection as the history.

#### Scenario: Order
- **GIVEN** message A was pinned yesterday and message B today
- **WHEN** a member lists the pinned messages
- **THEN** B comes before A

### Requirement: Live updates of pins
Pinning and unpinning SHALL advance the channel's revision and the message's revision and send `post_edited` with the
message to the channel's members, so catching up with `since_revision` also returns the change.

#### Scenario: Two browsers
- **GIVEN** Ana and Bruno have `#projects` open
- **WHEN** Ana pins a message
- **THEN** Bruno sees it pinned within a second, without reloading

#### Scenario: Catch up
- **GIVEN** Bruno was offline while Ana unpinned a message
- **WHEN** his client requests the changes since its last revision
- **THEN** it receives that message with `is_pinned: false`

### Requirement: Deleted messages are not pinned
Deleting a message SHALL clear its pin, and deleting a thread's root SHALL clear the pins of its replies; tombstones
SHALL never be listed as pinned.

#### Scenario: Delete a pinned message
- **GIVEN** a pinned message
- **WHEN** its author deletes it
- **THEN** the pinned list no longer has it and its tombstone has `is_pinned: false`
