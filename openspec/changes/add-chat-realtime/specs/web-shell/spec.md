# Spec Delta

## MODIFIED Requirements

### Requirement: Placeholder sections
Meetings, Files and Activity SHALL have routes rendering an empty state that names the section, so later changes
replace content without changing navigation; Conversations SHALL open the chat of the user's last team and channel, and
Teams SHALL list the user's teams and the open teams they may join.

#### Scenario: Meetings route
- **WHEN** the user selects Meetings in the rail
- **THEN** the URL is `/meetings` and an empty state titled "Meetings" is shown

#### Scenario: Conversations route
- **GIVEN** the user last viewed `#projects` in the team `engineering`
- **WHEN** they select Conversations in the rail
- **THEN** the chat of `#projects` in `engineering` opens

### Requirement: Keyboard and screen-reader access
Every interactive element SHALL be reachable by keyboard with a visible focus indicator, icon-only buttons SHALL have
accessible names, and route changes SHALL move focus to the new page's heading, except routes of a channel or
conversation, which SHALL move focus to the composer and announce the channel name to screen readers.

#### Scenario: Icon button name
- **WHEN** a screen reader reaches the navigation rail's Meetings button
- **THEN** it announces "Meetings"

#### Scenario: Opening a channel
- **WHEN** the user opens `#devops`
- **THEN** focus is in the composer and screen readers announce "devops"
