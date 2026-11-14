# Spec Delta

## Purpose

Gives each person one list of what was addressed to them: messages that mention them and replies in threads they
take part in.

## ADDED Requirements

### Requirement: What creates an activity item
Storing a message SHALL create an activity item for each member it mentions by name, and, for a reply, for each
earlier author of its thread; never for the message's own author, never for `@channel` or `@here`, and at most one
item per person per message.

#### Scenario: Mention
- **WHEN** Bruno posts "@ana.silva can you review?" in `#projects`
- **THEN** Ana has a new activity item of kind `mention` for that message and Bruno has none

#### Scenario: Reply in my thread
- **GIVEN** Ana started a thread and Carla replied in it
- **WHEN** Bruno replies in that thread
- **THEN** Ana and Carla each have a new item of kind `reply`

#### Scenario: Channel-wide mention
- **WHEN** Bruno posts "@channel deploy at 17h"
- **THEN** nobody gets an activity item

### Requirement: Activity list
`GET /api/v1/users/me/activity` SHALL list the caller's items newest first, paged by `cursor`, each with its kind,
the message and its channel; items of channels the caller cannot read at that moment and of deleted messages SHALL
NOT be listed.

#### Scenario: After leaving the channel
- **GIVEN** Ana was mentioned in `#devops` and then left it
- **WHEN** she lists her activity
- **THEN** that mention is not in the list

### Requirement: Unseen count
The list SHALL carry the number of items newer than the caller's last visit, `POST /api/v1/users/me/activity/seen`
SHALL reset it, and the server SHALL send `activity_updated` with the count to the person's connections whenever it
changes.

#### Scenario: Live badge
- **GIVEN** Ana has the app open on another channel
- **WHEN** Bruno mentions her
- **THEN** her connections receive `activity_updated` with `unseen: 1`

#### Scenario: Seen in one tab
- **WHEN** Ana opens Activity in one tab
- **THEN** her other tabs receive `activity_updated` with `unseen: 0`

### Requirement: Activity retention
Items older than 90 days SHALL be removed.

#### Scenario: Old item
- **GIVEN** an item created 91 days ago
- **WHEN** the daily task runs
- **THEN** it is gone
