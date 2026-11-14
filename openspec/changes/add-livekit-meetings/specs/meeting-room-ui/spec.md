# Spec Delta

## Purpose

Defines the web app's full-screen meeting room in the style of Google Meet: joining with a preview, the video grid,
the round controls at the bottom, and the channel's messages and people beside the call.

## ADDED Requirements

### Requirement: Pre-join screen
Opening a channel's room SHALL show a full-screen pre-join screen with the room's title, a preview of the camera, the
microphone and camera toggles, who is already in the room and a "Join now" button. When the browser has no camera or
microphone, or refuses them, the screen SHALL say so in a themed alert and still allow joining without them.

#### Scenario: Preview before joining
- **GIVEN** a browser with a camera and microphone
- **WHEN** Ana presses "Start meeting" in `#projects`
- **THEN** a full-screen pre-join screen titled with the room of `#projects` shows her camera preview and "Join now"

#### Scenario: No devices
- **GIVEN** a browser whose media devices are refused
- **WHEN** Ana opens the pre-join screen
- **THEN** a themed alert says the camera and microphone are unavailable and "Join now" still joins without them

### Requirement: Room layout
After joining, the room SHALL fill the window: a grid of tiles (one per participant, camera or picture with the
person's name, a muted-microphone mark, a raised-hand mark and a highlight while speaking), a bottom bar with the
clock and room title on the left, round controls centered and panel toggles on the right.

#### Scenario: Two people
- **GIVEN** Ana and Bruno joined the room of `#projects`
- **WHEN** Ana looks at the room
- **THEN** two tiles named "Ana Silva" and "Bruno Martins" are shown and her own tile is marked as hers

#### Scenario: Camera off
- **WHEN** Bruno turns his camera off
- **THEN** his tile shows his picture or initials instead of video for everyone

### Requirement: Call controls
The bottom bar SHALL offer, as round buttons with accessible names and pressed states: microphone, camera, share
screen, raise hand, reactions, more and leave (in the danger color). Raising a hand and reactions SHALL be seen by
everyone in the room; a shared screen SHALL get the largest tile.

#### Scenario: Mute
- **WHEN** Ana presses the microphone button
- **THEN** her microphone stops publishing, the button reads "Turn on microphone" and her tile shows the muted mark to
  everyone

#### Scenario: Raise hand
- **WHEN** Bruno presses "Raise hand"
- **THEN** a raised-hand mark shows on his tile for everyone until he lowers it

#### Scenario: Reaction
- **WHEN** Ana sends the 👍 reaction
- **THEN** a 👍 appears briefly over her tile for everyone in the room

#### Scenario: Share screen
- **WHEN** Ana shares her screen
- **THEN** the shared screen is shown as the largest tile for everyone and the button reads "Stop sharing"

### Requirement: Channel chat and people beside the call
A side panel on the right SHALL show, in two tabs, the channel's messages live (sending a message posts it to the
channel) and the people in the room. It SHALL open and close from the bottom bar and under 768 px SHALL cover the grid.

#### Scenario: Message during a meeting
- **GIVEN** Ana is in the room of `#projects` with the chat tab open
- **WHEN** she sends "Starting the demo"
- **THEN** the message is posted to `#projects` and appears in the panel and in the channel for its members

### Requirement: Leaving the room
Pressing leave SHALL disconnect from the room, stop the camera, microphone and screen share, and return to the page
the user was on. Losing access to the channel, the session ending or being removed by the server SHALL also end the
call with a themed notice.

#### Scenario: Leave
- **WHEN** Ana presses leave
- **THEN** she is disconnected, her camera light turns off and the channel she was in is shown

#### Scenario: Removed from the channel
- **GIVEN** Bruno is in the room of `#projects`
- **WHEN** he is removed from `#projects`
- **THEN** his call ends and a themed notice says he no longer has access to the channel

### Requirement: Accessible and themed room
The room SHALL be a modal dialog that traps focus and is labelled by the room's title, every control SHALL be
reachable by keyboard with a visible focus indicator, joins and leaves SHALL be announced politely, all text SHALL be
translated, and its colors SHALL come from the theme's role tokens with WCAG AA contrast (AAA in the MeetHub themes).

#### Scenario: Keyboard only
- **WHEN** a keyboard user tabs through the room
- **THEN** focus stays in the room and reaches every control with a visible indicator

#### Scenario: Contrast
- **WHEN** the room renders in `meethub-dark`
- **THEN** the tile labels and the bottom bar's text meet 7:1 contrast
