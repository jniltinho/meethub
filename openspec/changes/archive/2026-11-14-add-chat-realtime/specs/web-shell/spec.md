# Spec Delta

## MODIFIED Requirements

### Requirement: Responsive layout
Below 768 px the context sidebar SHALL become a drawer opened from a header button and the navigation rail SHALL
become a bottom bar, without horizontal page scrolling at 360 px width. On Full HD, QHD and 4K windows (the layout is
enlarged 1.25× from 2 400 px and 1.6× from 3 200 px of width) the application SHALL fill the window exactly: the page
itself SHALL never scroll and the composer SHALL stay inside the window.

#### Scenario: Phone width
- **GIVEN** a 360 px wide viewport
- **WHEN** the user opens the app
- **THEN** the sidebar is hidden behind a menu button and the page does not scroll horizontally

#### Scenario: Full HD, QHD and 4K
- **GIVEN** a window of 1920×1080, 2560×1440 or 3840×2160
- **WHEN** the user opens a channel and focus moves to the composer
- **THEN** the layout ends at the window's edges, the page is not scrolled and the composer is visible

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
