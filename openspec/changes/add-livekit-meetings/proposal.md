# Proposal

## Why

MeetHub's navigation already reserves Meetings, and the approved layout (design D11 of `add-chat-mockup-parity`) puts
a "Start meeting" button in every channel header and a room card in the details panel, but nothing behind them works:
a team that chats in MeetHub still has to leave it for video calls. LiveKit gives us a self-hosted SFU; this change
connects it to channels so the people who can read a channel can meet in it, with the same authorization as the chat.

## What Changes

- A LiveKit server joins the Compose stack; MeetHub reaches it with a browser URL, an optional server-side API URL, and
  an API key and secret accepted only from the environment. Without them meetings are off and the UI hides them.
- Every channel and conversation has one persistent meeting room. Its LiveKit room name is a random value stored by
  the server; clients never name rooms, they ask for a token for a channel.
- `POST /api/v1/channels/{id}/meeting/token` issues a short-lived (2 minutes) LiveKit access token, bound to the
  caller's user id and to that channel's room only, to members who may read the channel (not archived, user active).
- LiveKit webhooks (`POST /api/v1/meetings/webhook`, verified by LiveKit's signature) keep who is in each room; a
  `meeting_updated` WebSocket event delivers the room's participants to the channel's members only. A reconciliation
  loop corrects lost webhooks and removes from the room anyone who can no longer read the channel (removed, left the
  team, deactivated, channel archived) within 10 seconds.
- `GET /api/v1/meetings` lists the rooms with people in them among the caller's channels; `GET
  /api/v1/channels/{id}/meeting` returns one room.
- The web app: a full-screen room in the style of Google Meet (pre-join preview, video grid, round controls at the
  bottom: microphone, camera, share screen, raise hand, reactions, more, leave; the channel's messages and the people
  in a side panel), a "Start meeting" / "Join meeting" button in the channel header, the room card and "in the room"
  marks in the details panel, a "Meeting rooms" sidebar section with the rooms that have people in them, and the
  Meetings page listing those rooms.
- `make init` generates a LiveKit key and secret into `.env` when missing (never committed).

Out of scope (each would more than double the work): scheduled and upcoming meetings (the details panel's "Upcoming
meetings" block and the calendar are left for a later change), recording, dial-in (SIP), breakout rooms, end-to-end
encryption, guests from outside the organization, and TURN/TLS setup for production networks (documented, not
automated).

## Capabilities

### New Capabilities
- `meetings`: meeting rooms per channel, LiveKit access tokens and their authorization, room presence from LiveKit
  webhooks, reconciliation and eviction of people who lost access, and the meetings API.
- `meeting-room-ui`: the full-screen room in the web app: pre-join, grid, controls, screen share, raise hand,
  reactions, the channel's chat and people in a side panel, leaving.

### Modified Capabilities
- `chat-ui`: the channel header's "Start meeting" button, the room card and room presence in the details panel, and
  the sidebar's "Meeting rooms" section.
- `web-shell`: Meetings stops being a placeholder and lists the rooms with people in them.
- `realtime-events`: the `meeting_updated` event and its audience.
- `configuration`: the `[livekit]` settings, with the key and secret only from the environment.
- `build-and-deploy`: the LiveKit service in the Compose stack and its generated credentials.

## Impact

- Go: new `internal/meetings` (rooms, tokens, webhooks, reconciliation), two small exported helpers in `internal/chat`
  (`ReadableChannel`, `MemberIDs`), `internal/config` (`[livekit]`), `internal/httpserver` (the LiveKit origin in the
  CSP `connect-src`), handlers and wiring in `cmd/`. Dependency: `github.com/livekit/server-sdk-go/v2` (with
  `github.com/livekit/protocol`).
- Database: migration `00005` (`meeting_rooms`, `meeting_participants`) on SQLite, MariaDB and PostgreSQL.
- SPA: `livekit-client`; new `frontend/src/stores/meetings.ts` and `frontend/src/components/meetings/`; small
  additive edits to `AppLayout.vue`, `ChannelView.vue`, `ChannelDetails.vue`, `ChatSidebar.vue`, `stores/chat.ts`
  (an event hook), the router, `style.css` and the i18n catalogs.
- Deployment: `deployments/compose/compose.yml` and `compose.e2e.yml` gain `livekit`; the Makefile's `init`;
  `config.toml.example`, `docs/`.
- Tests: Go unit tests on the three databases; Playwright joins a room with Chromium's fake camera and microphone.
