# Presenting banner with "Share something else"

## Why

The browser draws the screen picker, so MeetHub cannot add previews or labels to it. A wrong pick is only noticed
afterwards, and the only way to fix it is to stop sharing and start again, which empties everyone's stage twice.

## What Changes

- While the user shares their screen, the meeting room shows a "You are presenting." banner above the stage (the
  stage already shows what is shared) with two actions: "Share something else" and "Stop sharing".
- "Share something else" opens the browser's picker again and swaps the shared surface; cancelling the picker keeps
  the current share.

Not in scope: hints for the browser's picker (Chromium only) and any change to what other participants see.

## Capabilities

### New Capabilities

None.

### Modified Capabilities

- `meeting-room-ui`: a new requirement for the presenting banner.

## Impact

- `frontend/src/components/meetings/` (`call.ts`, `MeetingRoom.vue`, `meetings.css`), one string in the pt-BR and es
  catalogs, the meetings browser test. No server, API or database change.
