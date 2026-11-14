# Spec Delta

## Purpose

Defines conversations outside teams: 1:1 direct messages (Mattermost type `D`) and group messages of 3 to 8 people
(type `G`), who may start them, how they are created exactly once and how each member's list of conversations works.

## ADDED Requirements

### Requirement: One direct conversation per pair
Starting a direct message with a user SHALL return the existing 1:1 conversation of that pair or create it with both
members in one transaction; there SHALL be at most one per pair, including a user's conversation with themselves.

#### Scenario: Concurrent creation
- **WHEN** Ana and Bruno start a direct message with each other at the same time
- **THEN** both get the same conversation, already holding both members

#### Scenario: Self conversation
- **WHEN** Ana starts a direct message with herself twice
- **THEN** the same conversation with one member is returned

### Requirement: Group conversations
Starting a group message with 3 to 8 distinct users (including the caller) SHALL return the existing conversation of
exactly that set or create it with all members in one transaction; members SHALL be fixed: the generic membership
endpoints SHALL refuse direct and group conversations with 400 `direct_channel`.

#### Scenario: Too many people
- **WHEN** a user starts a group message with 9 people
- **THEN** the response is 400 with code `invalid_input`

#### Scenario: Same set, any order, concurrently
- **WHEN** Carla and Bruno start a group message with the same three people at the same time
- **THEN** both get the same conversation with all three members

#### Scenario: Adding a third person to a DM
- **WHEN** someone calls the channel member endpoint on a direct conversation
- **THEN** the response is 400 with code `direct_channel`

### Requirement: Who may message whom
A direct or group conversation SHALL be created only between active users, and, when any participant is a guest, only
if every guest shares at least one team channel (not a direct or group conversation) with each other participant; an
existing conversation SHALL stay readable and usable after that condition stops holding.

#### Scenario: Member messaging a guest stranger
- **GIVEN** a member and a guest who share no team channel
- **WHEN** the member starts a direct message with the guest
- **THEN** the response is 403 with code `forbidden`

#### Scenario: Disabled user
- **WHEN** a user starts a direct message with a disabled user
- **THEN** the response is 400 with code `user_inactive`

### Requirement: Conversation list and visibility
`GET /api/v1/users/me/conversations` SHALL list, ordered by last message and cursor-paginated (at most 200), the direct
and group conversations visible to the user; a conversation SHALL become visible to a member when they open it or when
it receives a message, and each member SHALL be able to hide it until its next message; the first time it becomes
visible to another member by a message, that member SHALL receive `direct_added` (1:1) or `group_added` (group).

#### Scenario: Empty conversation opened
- **WHEN** Ana opens a direct message with Carla without writing and reloads the page
- **THEN** the conversation is still in Ana's list and not in Carla's

#### Scenario: New message surfaces the conversation
- **WHEN** Ana sends Carla a first direct message
- **THEN** Carla receives `direct_added` and `posted`, and the conversation appears in her list with one unread

### Requirement: Conversations behave like channels
Direct and group conversations SHALL support messages, threads, reactions, read state, revisions and real-time events
as channels do, readable only by their members (404 for others).

#### Scenario: Outsider reading a DM
- **WHEN** a user requests the messages of a conversation they do not belong to
- **THEN** the response is 404 with code `not_found`
