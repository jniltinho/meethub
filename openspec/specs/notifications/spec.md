# notifications Specification

## Purpose
Tells people about messages addressed to them when they are not looking: in the browser while the app is in the
background, by e-mail while they are away.

## Requirements

### Requirement: Notification preferences
Each person SHALL have a desktop preference (`all`, `mentions`, `none`; default `mentions`) and an e-mail
preference (`mentions`, `none`; default `mentions` when e-mail is configured), read and changed through their own
preferences.

#### Scenario: Turn e-mail off
- **WHEN** Ana sets her e-mail preference to `none`
- **THEN** no notification e-mail is sent to her afterwards

### Requirement: Desktop notifications
With the browser's permission, the app SHALL show a desktop notification for a direct message or a message that
mentions the person (any message of their conversations with the preference `all`) when the app is not the visible
tab or another conversation is open; choosing it SHALL open that message. Permission SHALL be requested only from
Settings.

#### Scenario: Mention in the background
- **GIVEN** Ana allowed notifications and is on another browser tab
- **WHEN** Bruno mentions her in `#projects`
- **THEN** a notification shows Bruno, `#projects` and the start of the message, and choosing it opens it

#### Scenario: Looking at the conversation
- **GIVEN** Ana has `#projects` open and visible
- **WHEN** Bruno mentions her there
- **THEN** no desktop notification is shown

### Requirement: E-mail notifications
A direct message or a direct mention still unread after 10 minutes, for a person who is not online and whose e-mail
preference allows it, SHALL be sent by e-mail with who wrote it, where, the start of the message and a link; a
person SHALL receive at most one such e-mail every 15 minutes, gathering the messages since the previous one.

#### Scenario: Away
- **GIVEN** Ana is offline and e-mail is configured
- **WHEN** Bruno sends her a direct message that stays unread for 10 minutes
- **THEN** Ana receives one e-mail with Bruno's name, the start of the message and a link to it

#### Scenario: Read in time
- **WHEN** Ana reads the message after 5 minutes
- **THEN** no e-mail is sent

#### Scenario: Several messages
- **GIVEN** Ana is away for an hour and receives 12 direct messages in the first five minutes
- **WHEN** the e-mails are sent
- **THEN** she receives one e-mail listing them, not twelve

### Requirement: Nothing unreadable is sent
An e-mail SHALL contain only messages the person can read at the moment it is written, and no excerpt when
`notifications.email_excerpt` is false.

#### Scenario: Removed from the channel
- **GIVEN** Ana was mentioned in `#finance` and removed from it five minutes later
- **WHEN** the e-mail task runs
- **THEN** that mention is not e-mailed

### Requirement: Unsubscribing
Every notification e-mail SHALL carry a link that sets the person's e-mail preference to `none` without logging
in, and a `List-Unsubscribe` header.

#### Scenario: One click
- **WHEN** Ana follows the link of an e-mail
- **THEN** a page confirms it and no further notification e-mail is sent to her
