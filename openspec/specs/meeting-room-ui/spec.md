# meeting-room-ui Specification

## Purpose
Defines the web app's full-screen meeting room in the style of Google Meet: joining with a preview, the video grid,
the round controls at the bottom, and the channel's messages and people beside the call.

## Requirements

### Requirement: Pre-join screen
Opening a channel's room SHALL show a full-screen pre-join screen with the room's title, a preview of the camera, a
live microphone level, camera and microphone pickers, the microphone and camera toggles, who is already in the room
and a "Join now" button. A refused device and a missing device SHALL each get their own themed message, and joining
SHALL stay possible without them.

#### Scenario: Preview before joining
- **GIVEN** a browser with a camera and microphone
- **WHEN** Ana presses "Start meeting" in `#projects`
- **THEN** a full-screen pre-join screen titled with the room of `#projects` shows her camera preview and "Join now"

#### Scenario: Devices refused
- **GIVEN** a browser that refuses the camera and microphone
- **WHEN** Ana opens the pre-join screen
- **THEN** a themed alert says the browser blocked them and how to allow them, with "Try again", and "Join now" still
  joins without them

#### Scenario: Microphone level and pickers
- **GIVEN** a browser with a microphone and a camera
- **WHEN** Ana speaks on the pre-join screen
- **THEN** the level meter moves, and choosing another camera or microphone in the pickers changes the preview

#### Scenario: Join with the microphone off
- **WHEN** Ana turns the microphone off on the pre-join screen and joins
- **THEN** she joins muted, her tile shows the muted mark to everyone and a themed notice says how to unmute

### Requirement: Waiting, empty and error states
Every state of a call SHALL be designed and announced: connecting (the pre-join screen stays with a busy "Join now"),
alone in the room (a hint that channel members can join), reconnecting (a status banner and a themed toast, then a
confirmation), the meeting server unreachable or the meeting refused (a themed alert on the pre-join screen with a
retry), and the call ended by the server. No state SHALL show a blank area or a raw error.

#### Scenario: Meeting server unreachable
- **GIVEN** LiveKit cannot be reached
- **WHEN** Ana presses "Join now"
- **THEN** the pre-join screen stays, a themed alert says the meeting server could not be reached, and "Join now" tries
  again

#### Scenario: Alone in the room
- **WHEN** Ana is the only one in the room
- **THEN** a hint says she is the only one here and that members of the channel can join from it

#### Scenario: Reconnecting
- **GIVEN** Ana is in a call
- **WHEN** the connection drops and comes back
- **THEN** a banner reads "Reconnecting…" while it lasts and a themed toast confirms the reconnection

### Requirement: Room layout
After joining, the room SHALL fill the window: a grid of tiles (one per participant, camera or picture with the
person's name, muted and camera-off marks, a raised-hand mark, and an outline and speaking mark for the active
speaker), a bottom bar with the clock and room title on the left, round controls centered and panel toggles on the
right.

#### Scenario: Two people
- **GIVEN** Ana and Bruno joined the room of `#projects`
- **WHEN** Ana looks at the room
- **THEN** two tiles named "Ana Silva" and "Bruno Martins" are shown and her own tile is marked as hers

#### Scenario: Camera off
- **WHEN** Bruno turns his camera off
- **THEN** his tile shows his picture or initials and a camera-off mark instead of video for everyone

### Requirement: Grid arrangement
The grid SHALL rearrange from 1 to many people in centered rows without layout jumps; while someone shares a screen,
the share SHALL take the stage and the people a strip beside it (below it on a phone).

#### Scenario: Three people
- **WHEN** a third person joins a call of two
- **THEN** the tiles rearrange into two rows with the last row centered, and no tile changes size while its person
  speaks, mutes or raises a hand

#### Scenario: Screen share stage
- **WHEN** Ana shares her screen in a call of three
- **THEN** the share fills the stage and the three people's tiles move to a strip beside it

### Requirement: Keyboard shortcuts and feedback
Ctrl+D (⌘+D on macOS) SHALL toggle the microphone and Ctrl+E (⌘+E) the camera, as in Google Meet, and the controls'
themed tooltips SHALL name them. Muting, the camera, raising a hand, and people joining or leaving SHALL be announced
politely to screen readers; people joining or leaving and a change made by a shortcut SHALL also show a themed toast.
Leaving SHALL ask for confirmation only while the user shares their screen.

#### Scenario: Shortcut
- **GIVEN** Ana is in a call with the microphone on
- **WHEN** she presses Ctrl+D
- **THEN** her microphone turns off, the button shows it, and a themed toast and a polite announcement say so

#### Scenario: Leaving while sharing
- **GIVEN** Ana shares her screen
- **WHEN** she presses leave
- **THEN** a themed dialog asks whether to stop presenting and leave

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
reachable by keyboard with a visible focus indicator and an accessible name, all text SHALL be translated, and its
colors SHALL come from the theme's role tokens (AAA in the MeetHub themes). The room and the pre-join screen SHALL
have no serious or critical axe violations.

#### Scenario: Axe
- **WHEN** axe checks the pre-join screen and the room in a call
- **THEN** it reports no serious or critical violation

#### Scenario: Keyboard only
- **WHEN** a keyboard user tabs through the room
- **THEN** focus stays in the room and reaches every control with a visible indicator

#### Scenario: Contrast
- **WHEN** the room renders in `meethub-dark`
- **THEN** the tile labels and the bottom bar's text meet 7:1 contrast

### Requirement: Fits every screen
The pre-join screen and the room SHALL fit the window without overlap, clipping or page scroll at 1920×1080,
2560×1440 and 3840×2160 (with the shell's 1.25× and 1.6× zoom) and on a 375×812 phone, where the panel covers the
grid and the controls wrap.

#### Scenario: 4K
- **GIVEN** a 3840×2160 window
- **WHEN** Ana is in a call with the chat panel open
- **THEN** the grid, the panel and the control bar lie inside the window and do not overlap

#### Scenario: Phone
- **GIVEN** a 375×812 window
- **WHEN** Ana is in a call
- **THEN** every control is inside the window and none overlaps another

### Requirement: Shape and motion
The meeting screens SHALL use only the shared corner radii (round shapes only for the call controls, pictures and
presence), and their motion (reactions, the speaking mark, panels) SHALL be subtle and stop under
`prefers-reduced-motion`.

#### Scenario: Reduced motion
- **GIVEN** the browser prefers reduced motion
- **WHEN** someone sends a reaction or speaks
- **THEN** the reaction and the speaking mark appear without animation
