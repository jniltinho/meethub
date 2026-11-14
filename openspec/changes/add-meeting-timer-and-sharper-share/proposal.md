# Meeting timer and a sharper shared screen

## Why

The meeting bar shows the time of day but not how long the meeting has run, which Teams and Meet show. And a shared
screen is blurry on monitors above 1080p: the capture is shrunk to 1920×1080 and sent at 2.5 Mbit/s, so small text
loses its edges.

## What Changes

- The bar shows, after the clock, the time since the meeting began (`mm:ss`, `h:mm:ss` from one hour on), counted
  from the join of the earliest person in the room.
- A shared screen is captured at its own resolution and sent with up to 5 Mbit/s at 15 frames a second, favouring
  detail over motion.

Not in scope: keeping a room's start on the server (the count ignores people who came and left before the user
joined) and a quality setting.

## Capabilities

### New Capabilities

None.

### Modified Capabilities

- `meeting-room-ui`: two new requirements, the elapsed time and the quality of a shared screen.

## Impact

- `frontend/src/components/meetings/` (`call.ts`, `MeetingRoom.vue`, `meetings.css`), `frontend/src/chat/format.ts`,
  one string in the pt-BR and es catalogs, unit and browser tests. A presenter may upload up to twice the bitrate
  of before. No server, API or database change.
