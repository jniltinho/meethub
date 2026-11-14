# channels Specification

## Purpose
Defines channels inside a team (Mattermost types `O` public and `P` private): their identity, exactly who may discover,
read, join, post in and manage them, and the default channel of each team.

## Requirements

### Requirement: Channel identity
A channel SHALL belong to one team and have a type `O` or `P`, a URL name unique in the team (lowercase letters,
digits, `-` and `_`, 2–64 characters), a display name (1–64 characters), a purpose (up to 250 characters), a header
(up to 1024 characters), its creator, last message time, a revision and an archive state.

#### Scenario: Duplicate channel name in a team
- **GIVEN** a channel `devops` in a team
- **WHEN** someone creates another channel named `devops` in the same team
- **THEN** creation fails with code `name_taken`

#### Scenario: Same name in another team
- **WHEN** a channel `devops` is created in a different team
- **THEN** it is created

### Requirement: Access rules
Public channels SHALL be discoverable by non-guest team members and readable only by their members; private channels
SHALL be discoverable and readable only by their members; every access SHALL also require membership of the channel's
team; a channel the caller may not discover SHALL answer 404 `not_found`; guests SHALL only reach channels they were
added to (the full matrix is in design.md, D4).

#### Scenario: Reading a private channel without membership
- **WHEN** a non-member requests the messages of a private channel
- **THEN** the response is 404 with code `not_found`

#### Scenario: Reading a public channel without joining
- **WHEN** a team member who has not joined a public channel requests its messages
- **THEN** the response is 403 with code `forbidden`

#### Scenario: Guest and public channels
- **GIVEN** a guest in a team
- **WHEN** they list joinable channels or try to join a public channel
- **THEN** no channel is listed and the join is refused with 403

### Requirement: Creating channels
Team members except guests SHALL create public and private channels in their team and become their `channel_admin`.

#### Scenario: Guest creates a channel
- **WHEN** a guest calls `POST /api/v1/channels`
- **THEN** the response is 403 with code `forbidden`

### Requirement: Channel membership and roles
Channel members SHALL have the role `channel_user` or `channel_admin`; administrators of the channel (per the matrix)
SHALL add team members to private channels, remove members and change channel roles, never their own role; any member
SHALL leave a channel except the team's default channel; adding, removing and role changes SHALL be serialized with
the channel's other changes.

#### Scenario: Adding someone from another team
- **WHEN** a channel admin adds a user who is not a member of the channel's team
- **THEN** the response is 400 with code `not_team_member`

#### Scenario: Leaving the default channel
- **WHEN** a member leaves the default channel of their team
- **THEN** the response is 400 with code `default_channel`

#### Scenario: Self-promotion
- **WHEN** a `channel_user` sets their own role to `channel_admin`
- **THEN** the response is 403 with code `forbidden`

### Requirement: Editing, archiving and restoring channels
Channel administrators SHALL change a channel's name, display name, purpose and header, archive it and restore it; an
archived channel SHALL keep its history readable to its members and SHALL reject messages, edits, deletions, reactions
and membership changes; every such change SHALL be audited and announced (`channel_updated`, `channel_deleted`,
`channel_restored`).

#### Scenario: Posting to an archived channel
- **GIVEN** an archived channel
- **WHEN** a member posts a message
- **THEN** the response is 403 with code `channel_archived`

#### Scenario: Archive racing a post
- **WHEN** an archive and a message are committed concurrently
- **THEN** the message is stored only if it committed before the archive

### Requirement: Default channel
Every team SHALL have a public default channel named `general` that members join on joining the team and that cannot
be archived or left.

#### Scenario: Archiving the default channel
- **WHEN** a team admin archives `general`
- **THEN** the response is 400 with code `default_channel`

### Requirement: Channel lists
The API SHALL list, with cursor pagination (at most 200 per page), the channels a user belongs to in a team with their
unread state, and the public channels of the team a non-guest may join.

#### Scenario: Browse public channels
- **WHEN** a member lists joinable channels of their team
- **THEN** public, unarchived channels they do not belong to are returned and private channels are not
