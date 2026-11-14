# Spec Delta

## ADDED Requirements

### Requirement: Start meeting in the channel header
When meetings are enabled, the channel header SHALL show, left of the "more" menu, a primary button that reads "Start
meeting" when nobody is in the channel's room and "Join meeting (n)" when n people are, opening the room's pre-join
screen. It SHALL be absent in archived channels and when meetings are disabled.

#### Scenario: Nobody in the room
- **GIVEN** nobody is in the room of `#projects`
- **WHEN** Ana opens `#projects`
- **THEN** the header shows "Start meeting" left of the "more" menu

#### Scenario: People in the room
- **GIVEN** Bruno and Carla are in the room of `#projects`
- **WHEN** Ana opens `#projects`
- **THEN** the button reads "Join meeting (2)" and updates live when someone joins or leaves

#### Scenario: Meetings disabled
- **GIVEN** the server has no LiveKit settings
- **WHEN** Ana opens `#projects`
- **THEN** no meeting button is shown

### Requirement: Room card in the details panel
When meetings are enabled, the channel details panel SHALL start with a room card ("Room of #channel", "Available now"
or "n in the room" with their pictures, and "Join room"), and each participant in the list who is in the room SHALL
be marked "In the room".

#### Scenario: Room card with people
- **GIVEN** Bruno is in the room of `#projects`
- **WHEN** Ana opens the details of `#projects`
- **THEN** the room card reads "1 in the room" with Bruno's picture, and Bruno's row in the participants is marked
  "In the room"

### Requirement: Meeting rooms in the sidebar
When meetings are enabled and at least one room among the current team's channels and the user's conversations has
people in it, the chat sidebar SHALL show a "Meeting rooms" section listing those rooms with the number of people in
each; selecting one SHALL open the room's pre-join screen.

#### Scenario: Active room listed
- **GIVEN** 4 people are in the room of `#sync`
- **WHEN** Ana looks at the sidebar of the team
- **THEN** the "Meeting rooms" section lists "sync" with "4 in the room"

#### Scenario: No active room
- **GIVEN** nobody is in any room of the team
- **WHEN** Ana looks at the sidebar
- **THEN** no "Meeting rooms" section is shown
