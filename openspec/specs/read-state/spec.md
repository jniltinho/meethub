# read-state Specification

## Purpose
Defines what a user has not read yet: unread message and mention counts per channel and conversation, how mentions are
detected and how a channel becomes read.

## Requirements

### Requirement: Unread counts
For each channel or conversation a user belongs to, the system SHALL keep the number of messages and the number of
mentions of that user since they last read it; the read position SHALL only move forward; joining a channel SHALL start
with nothing unread; deleting or editing a message SHALL NOT change counts; direct and group messages SHALL count every
message from others as a mention.

#### Scenario: Messages while away
- **GIVEN** Carla last read `#devops` before 4 new messages, one mentioning `@carla.pereira`
- **WHEN** her channel list loads
- **THEN** `#devops` shows 4 unread messages and 1 mention

#### Scenario: Concurrent view and mention
- **WHEN** Carla opens `#devops` while a message mentioning her is being posted
- **THEN** the message is either counted as read or as one unread mention, never lost and never counted twice

### Requirement: Posting marks the channel as read
As in Mattermost, posting a message SHALL mark that channel or conversation as read for its author.

#### Scenario: Reply from another device
- **GIVEN** Carla has 4 unread messages in `#devops`
- **WHEN** she posts there from her phone
- **THEN** `#devops` shows nothing unread on all her devices

### Requirement: Mentions
A message SHALL mention a user when it contains `@username` of a member of the channel, `@channel` or `@all` (every
member), or `@here` (members whose status is `online` when it is posted); mentions inside code spans or blocks SHALL NOT
count; a user mentioned several ways in one message SHALL count once; the author SHALL never count.

#### Scenario: Mention of a non-member
- **WHEN** a message in `#devops` contains `@diego.rocha` and Diego is not a member
- **THEN** no mention is counted for Diego

#### Scenario: Mention in code
- **WHEN** a message contains `` `@ana.silva` `` in a code span
- **THEN** Ana's mention count does not change

#### Scenario: Double mention
- **WHEN** a message contains `@channel` and `@ana.silva`
- **THEN** Ana's mention count grows by one

### Requirement: Marking as read
Opening a channel or conversation SHALL mark it as read for that user (`POST /api/v1/channels/{id}/view`), reset its
counts and send `channel_viewed` to that user's other connections.

#### Scenario: Read on one device
- **GIVEN** Ana is connected in two browsers with 3 unread in `#projects`
- **WHEN** she opens `#projects` in one of them
- **THEN** the other browser clears the badge of `#projects`

### Requirement: Team totals
The team list SHALL include, per team, whether it has unread messages and its total mention count.

#### Scenario: Mention in another team
- **WHEN** Bruno is mentioned in a channel of a team other than the one he is viewing
- **THEN** that team shows a mention badge in his team switcher
