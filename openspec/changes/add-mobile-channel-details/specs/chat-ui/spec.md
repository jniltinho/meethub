# Spec Delta

## ADDED Requirements

### Requirement: Channel details on small screens
Below 1024 px the Members action and the "Channel details" menu item SHALL open the channel's details (about,
meeting room, participants, recent files, pinned messages) as a full-screen panel with a close button; focus SHALL
move into it and return to the control that opened it, and Escape and the browser's Back SHALL close it.

#### Scenario: Phone
- **GIVEN** a 390 px wide viewport on `#projects`
- **WHEN** the user presses the Members action
- **THEN** the participants of `#projects` fill the screen under a header with a close button

#### Scenario: Tablet
- **GIVEN** an 820 px wide viewport
- **WHEN** the user chooses "Channel details" in the channel menu
- **THEN** the details open full screen and the conversation is behind them

#### Scenario: Closing returns the focus
- **WHEN** the user closes the details with Escape
- **THEN** the conversation shows again and the Members action has the focus
