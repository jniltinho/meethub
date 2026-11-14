# Spec Delta

## Purpose

Defines the conversation screens of the web app, following the mockup: team switcher, sidebar of channels and
conversations, message timeline, composer, thread panel, reactions and presence — themed, translated and accessible.

## ADDED Requirements

### Requirement: Team switcher and sidebar
The sidebar SHALL show the current team in a switcher (with unread and mention badges for the other teams), then the
team's channels (public and private, unread in bold with mention counts) and the user's direct and group conversations
with presence; it SHALL offer creating and browsing channels and starting a direct or group message.

#### Scenario: Unread channel
- **GIVEN** `#devops` has 4 unread messages, one mentioning the user
- **WHEN** the sidebar renders
- **THEN** `#devops` is bold with a badge "1", and the team in the switcher shows a mention badge

### Requirement: Routes and fallbacks
Channels SHALL open at `/teams/:team/channels/:channel`, conversations at `/messages/@:username` and
`/messages/:conversationId` (not tied to a team); Conversations SHALL reopen the last visited channel or conversation
that is still readable, else the default channel of the user's first team; a user without teams SHALL see their
conversations and an empty state offering to browse teams.

#### Scenario: Deep link
- **WHEN** a user opens `/teams/engineering/channels/devops`
- **THEN** that channel is shown and its URL survives a reload

#### Scenario: Remembered channel no longer readable
- **GIVEN** the user's last channel was archived team `ops`'s `#finance` and they left `ops`
- **WHEN** they open Conversations
- **THEN** the default channel of their first team opens

#### Scenario: Not a member of a private channel
- **WHEN** a user opens the URL of a private channel they do not belong to
- **THEN** a themed "channel not found" state is shown

### Requirement: Channel view
Opening a channel SHALL show its header (name, purpose, member count, header text), the timeline (author, avatar
initials, time, Markdown text, edited mark, reactions, reply count with last reply time, day separators, consecutive
messages of one author within 5 minutes grouped as in Mattermost) and the composer, and SHALL mark the channel as read;
older messages SHALL load when scrolling up.

#### Scenario: Grouped messages
- **WHEN** Ana posts three messages within two minutes
- **THEN** only the first shows her name and avatar

### Requirement: Composer
The composer SHALL send with Enter and add a line with Shift+Enter, show a message at once as pending and replace it
with the stored one (same `pending_post_id`), offer an emoji picker and `@username` autocomplete, send typing
notifications, and keep a failed message in the timeline marked as failed with Retry (same key).

#### Scenario: Send offline
- **GIVEN** the connection is lost
- **WHEN** the user sends a message
- **THEN** it is shown as failed with Retry, and retrying after reconnecting stores it exactly once

### Requirement: Threads in the side panel
Reply and reply counts SHALL open the thread in the 320 px side panel (full screen on phones) with the root, its
replies and its own composer, updated live.

#### Scenario: Live reply
- **GIVEN** Ana has a thread open
- **WHEN** Bruno replies in it
- **THEN** the reply appears in Ana's panel and the root's reply count grows in her timeline

### Requirement: Message actions
Each message SHALL offer, according to permissions, reply, add reaction, edit (author, in place, Escape cancels) and
delete (author or channel administrators, after a themed confirmation).

#### Scenario: Delete needs confirmation
- **WHEN** the author chooses Delete
- **THEN** a themed confirm dialog asks first and the message becomes a tombstone for everyone only after OK

### Requirement: Live updates and recovery
The chat SHALL apply WebSocket events live, ignore updates older than the revision it holds, show a themed
"reconnecting" state while offline, and resynchronize through the API after `resync`, a `seq` gap, a reconnection and
every 60 seconds, without duplicates; logging out or a session ending SHALL close the socket and clear all chat state.

#### Scenario: Two browsers
- **GIVEN** Ana and Bruno have `#projects` open in two browsers
- **WHEN** Bruno sends a message, edits it, adds a reaction and deletes it
- **THEN** Ana sees each change within a second

#### Scenario: Another user logs in on the same browser
- **WHEN** Ana logs out and Bruno logs in on the same tab
- **THEN** none of Ana's channels, messages or unread counts remain visible

### Requirement: Themed, translated and accessible
The chat SHALL use only theme roles (square corners; all 22 themes pass the contrast recipes for mentions, unread badges
and presence), translate every string into Portuguese and Spanish, announce new messages of the open channel politely to
screen readers, and support Alt+Up/Down to change channel and Up to edit the last own message.

#### Scenario: Keyboard navigation
- **WHEN** a keyboard user presses Alt+Down in a channel
- **THEN** the next channel of the sidebar opens with focus in its composer
