# Design

## Context

The chat (`internal/chat`) owns channels, memberships and the read rule (`CanRead` in `policy.go`); its writes publish
events through `chat.Publisher`, which the realtime bus (`internal/realtime`) fans out over Redis to the WebSocket hub.
Long-running loops (hub revalidation, bus, presence) are owned by the server's errgroup in `cmd/server.go` and
`cmd/realtime.go`. Secrets come only from the environment (`secret:"true"` fields in `internal/config`). The SPA keeps
chat state in `stores/chat.ts`, which applies socket events in one `apply` switch and reloads through the API after a
gap (`resync`). Design D11 of `add-chat-mockup-parity` fixed the look: a full-screen room as in Google Meet, "Start
meeting" in the header, a room card in the details panel. LiveKit is the SFU (server SDK Go v2, `livekit-client`).

## Goals / Non-Goals

**Goals:** one persistent room per channel and conversation; tokens only for readers, decided in the server; live
room presence for the channel's members; removal from the room when access is lost; the room UI, header button,
details card, sidebar section and Meetings page; a LiveKit service in Compose; tests without a real camera.

**Non-Goals:** scheduled/upcoming meetings, recording, dial-in, breakout rooms, E2EE, external guests, TURN/TLS
automation, server-side participant moderation (mute others, kick from the UI).

## Decisions

### D1. Configuration and the "enabled" switch
`[livekit]` with `url`, `api_url` (default: `url` with `ws`→`http`, `wss`→`https`), `api_key` and `api_secret` (both
`secret:"true"`, so only `MEETHUB_LIVEKIT_API_KEY[_FILE]` / `MEETHUB_LIVEKIT_API_SECRET[_FILE]`). All empty → meetings
disabled; partially set → startup error. LiveKit itself rejects secrets under 32 bytes, so validation does too.
Alternative considered: a separate `enabled` flag — rejected, the credentials already say it.

### D2. Rooms and their names
Migration `00005` adds `meeting_rooms(channel_id PK → channels ON DELETE CASCADE, name UNIQUE, revision,
created_at)` and
`meeting_participants(channel_id → meeting_rooms ON DELETE CASCADE, user_id, sid, joined_at, PK(channel_id,
user_id))`. A room row is created on the first token with `"mh_" + 32 hex chars` from `crypto/rand` (128 bits), using
`INSERT … ON CONFLICT DO NOTHING` (GORM's `OnConflict{DoNothing}` on the three dialects) and re-reading, so concurrent
first tokens agree. The token is the only way into a room and its `room` claim is signed, so the random name is
defense in depth: a leaked API key aside, nobody can address another channel's room, and the name reveals nothing.
Alternative: name = channel id — rejected by the task (guessable) and it would leak ids to LiveKit logs.

### D3. Token issuing
`internal/meetings.Service.Token(ctx, sub, channelID)` calls `chat.Service.ReadableChannel` (new, exported wrapper of
the existing `readableChannel`, so the read rule and its 404/403 split stay in one place), refuses archived channels
with `chat.ErrArchived`, ensures the room, and signs with `livekit/protocol/auth`: identity = user id, name = display
name, TTL 2 minutes (only needed to connect; LiveKit refreshes tokens of connected participants itself), grants
`roomJoin`, `room`, `canPublish`, `canSubscribe`, `canPublishData`, `canUpdateOwnMetadata`. No admin, record, create
or hidden grants. The response is `{url, token, expires_at}`; neither the token nor the secret is logged. The user's
active status is already guaranteed by the session middleware.

### D4. Webhooks and presence
`POST /api/v1/meetings/webhook` is registered outside the same-origin/CSRF/session groups (LiveKit is not a browser),
with a 64 KiB body limit, and verified with `webhook.ReceiveWebhookEvent` (JWT signed with the API secret carrying the
body's SHA-256). Handled events: `participant_joined` → upsert (channel, user, sid, joined_at); `participant_left` and
`participant_connection_aborted` → delete only when the stored sid matches (a replaced session's late "left" does not
remove the new one); `room_finished` → delete all. Every change runs in a transaction that first bumps the room's `revision` (taking
its row lock), so changes to one room are serialized and numbered; after the commit, `meeting_updated` with the full
list and that revision goes to the channel's members (`chat.Service.MemberIDs`, new) with `ChannelID` set so the
hub's purge on removal covers it. Clients keep the highest revision per room and drop older snapshots, which replicas
may publish out of order; a change that changes nothing rolls back and announces nothing. Unknown rooms → 200 and
ignored.

### D5. Reconciliation loop (eviction and lost webhooks)
One loop per replica, started with the realtime loops, every 3 s, each step with a 3-second deadline: first the
refresh — `ListRooms` plus the rooms we list as occupied, and for each `ListParticipants` replaces the rows (room not
found → empty), so a lost join, leave or a replayed webhook heals within a pass — then the eviction: one SQL query finds
participants who may no longer read the channel (no `channel_members` row, user not active, channel archived, or a
team channel whose team membership is gone), calls LiveKit's `RemoveParticipant` (404 ignored) and deletes the rows.
A removal therefore takes effect within 3 + 3 + 3 = 9 seconds even when a webhook was lost. Replicas doing the same
work is harmless (idempotent calls). Alternative: hook the chat's removal paths — rejected,
there are many (leave, removal, team leave, archive, deactivation in `internal/org`) and a missed one is an
authorization hole; one query covers them all, within the 10 s the spec promises. The LiveKit room service is behind a
small consumer-side interface so tests use a fake.

### D6. API
`GET /api/v1/meetings` → `{enabled, rooms: [{channel_id, team_id, participants: [{user_id, joined_at}]}]}`, rooms with
people among channels where the caller is a member (and, for team channels, still in the team) — one SQL join.
`GET /api/v1/channels/:id/meeting` → one room (read rule). `POST /api/v1/channels/:id/meeting/token` → D3. Routes live
in `internal/httpserver/handlers/meetings.go`; errors reuse the chat mappers plus `meetings_unavailable` (503).

### D7. CSP
`connect-src` gains the LiveKit origin in both schemes (`wss://host` and `https://host`, the client also calls
`/rtc/validate` over HTTP) when meetings are enabled; otherwise the policy is unchanged.

### D8. SPA structure
`stores/meetings.ts` (Pinia): `enabled`, `rooms` (channel id → participants), `load()`, `apply(event)`, and the call
state (`openChannel`, `phase: 'prejoin' | 'joining' | 'joined'`). `stores/chat.ts` gains only an `extraHandlers` hook
called from `apply` and `resync` so meeting code stays out of it. The LiveKit `Room` object lives in a module variable
of `components/meetings/useCall.ts` (not reactive: it is large and mutable); reactive tiles are derived from its
events. The room is an overlay (`MeetingRoom.vue`, `position: fixed; inset: 0`) mounted once in `AppLayout.vue`, a
modal dialog — not a route: the chat store, socket and panels stay alive under it and leaving returns to the same
page. Header button, details card, sidebar section and the Meetings page (`views/MeetingsView.vue`, replacing the
placeholder route) call `meetings.open(channelId)`.

### D9. Room UI details
Grid: CSS grid with columns `ceil(sqrt(n))`, a screen share spans the first row (spotlight). Tiles show the camera or
`UserPicture`, the name from the chat store (identity = user id), a muted mark, a raised hand (participant attribute
`hand=1`) and a speaking outline (`ActiveSpeakersChanged`). Reactions: a reliable data message on topic `reaction`
with one of five fixed emoji (others are dropped on receipt), shown for 3 s over the sender's tile. "More" menu:
participants panel and full screen (Fullscreen API). The chat panel reuses the chat store (`rootsOf`, `send`) with a
plain input, not the full composer. Leave → `room.disconnect()` and stop local tracks. `Disconnected` with reason
`PARTICIPANT_REMOVED` or a lost channel → themed toast "You no longer have access to this channel."

### D10. Compose and credentials
`livekit/livekit-server:v1.13.10` (pinned by digest) in `compose.yml` with `LIVEKIT_CONFIG` holding ports (7880
signalling, 7881/tcp, 7882/udp), `keys`, the webhook URL `http://meethub:8080/api/v1/meetings/webhook` and
`rtc.node_ip` (`LIVEKIT_NODE_IP`, default `127.0.0.1` for a local browser). MeetHub gets `MEETHUB_LIVEKIT_URL=ws://
localhost:7880` and `MEETHUB_LIVEKIT_API_URL=http://livekit:7880`. `make init` appends `MEETHUB_LIVEKIT_API_KEY` and a
`/dev/urandom` secret to `.env` when absent (`.env.example` is not touched; the example config documents them).
`compose.e2e.yml` points both at `livekit:7880`, clears the node IP (LiveKit then advertises its container address,
reachable from the runner) and unpublishes ports.

### D11. Tests
Go: table-driven tests with `dbtest.Each` for the token rule (member, non-member public, private, conversation,
archived, disabled), token claims (decoded with the secret), room-name uniqueness and concurrent creation, webhook
signature and handling (sid rule, room finished, unknown room), the eviction query (removed, deactivated, archived,
team left) with a fake room service, the meetings list, the `[livekit]` config validation and the CSP. Playwright:
Chromium with `--use-fake-device-for-media-stream`, `--use-fake-ui-for-media-stream` and
`--unsafely-treat-insecure-origin-as-secure=http://meethub:8080`; two users join the same room, see each other's tile,
the second user's details panel shows "In the room" through the real webhook path, and leaving clears it. Contrast
recipes cover the bottom bar, tile labels and the "Join meeting" button.

## Risks / Trade-offs

- [Webhooks are lost while MeetHub restarts] → the 3 s `ListParticipants` refresh corrects the list.
- [A signed webhook is replayed within its validity] → it can only change the presence list (never media access), and
  the next refresh replaces the list with LiveKit's; no event-id store is kept.
- [A removed member keeps their media for a few seconds] → bounded by the reconciliation pass (at most 9 s; spec:
  10 s); LiveKit's `RemoveParticipant` disconnects them and their next token request fails. A token issued just before
  the removal can reconnect until it expires (2 minutes) and is evicted again on the next pass.
- [LiveKit API load] → one `ListRooms` and one `ListParticipants` per active room every 3 s per replica: fine for a
  self-hosted team; a larger install would move refresh to webhooks plus a slower cadence.
- [Two tabs of the same user] → LiveKit replaces the older session (same identity); the sid rule keeps the list right.
- [Media through NAT/firewalls] → Compose works for a browser on the same host; production needs a public IP, TLS and
  usually TURN (documented in `docs/`, not automated).
- [Chromium's fake devices differ from real ones] → real audio/video quality between two machines is not verified by
  the suite; noted in the final report.
- [Two changes edit the same shell files] → edits are additive and small; meeting code lives in its own files.

## Migration Plan

Migration `00005` is additive (two new tables); `meethub migrate down` drops them (room names and presence are
lost, nothing else). Meetings stay off until LiveKit settings exist; `make up` adds them to `.env` and starts LiveKit.
When both this change and `add-file-uploads` are archived, the web-shell "Placeholder sections" requirement must end
with only Activity as a placeholder: whichever change archives second reconciles the text.

## Open Questions

None blocking. Assumptions recorded above: 2-minute tokens, snapshot events, eviction by polling, overlay instead of
route, five fixed reaction emoji, and scheduled meetings left for a later change.

### D12. The AAA+ bar for the meeting screens

The user asked for "padrão de qualidade AAA+" because people must like using the meetings. Read as: the core flow
(join, talk, share, chat, leave) finished before any extra feature.

- Pre-join: the microphone level comes from a Web Audio `AnalyserNode` on the preview's audio track (a `meter` with an
  accessible name, about 10 updates a second); camera and microphone pickers list `Room.getLocalDevices` and switch the
  preview with `restartTrack`, and the chosen devices are used on joining. The preview distinguishes a refused device
  (`NotAllowedError`: how to allow it, and "Try again") from a missing one (`NotFoundError`).
- The pre-join screen stays during connecting (busy "Join now") and shows a themed alert with a retry when the token is
  refused or LiveKit cannot be reached, instead of a toast and a blank room.
- Grid: tile width and height from the count (columns `ceil(sqrt(n))`, two at most on a phone) in a wrapping flex box,
  so the last row is centered; marks are absolutely positioned and the speaking outline is an `outline`, so nothing
  moves. A shared screen switches to a stage with the people in a strip (right, or below on a phone).
- Feedback: polite announcements for muting, camera, hand, joins and leaves; themed toasts for joins and leaves after
  one's own join, for changes made by a shortcut, for joining muted and for reconnecting/reconnected
  (`RoomEvent.Reconnecting`/`Reconnected`), with a status banner while reconnecting. Ctrl+D and Ctrl+E as in Meet.
  Leaving asks only while one's screen is shared (Meet does not ask otherwise).
- Tooltips: a themed CSS tooltip on the controls (`--color-tooltip` / `--color-on-tooltip`, naming the shortcut);
  the accessible name stays on `aria-label`.
- Verified by tests, not by eye: contrast recipes for every new pair (AAA enforced for the MeetHub themes by
  `02-themes.spec.ts`), bounding-box checks at 1920×1080, 2560×1440, 3840×2160 and 375×812, axe on the pre-join screen
  and the room, keyboard (Tab stays in, focus visible, shortcuts) and the states above.
- Left out in favour of the finished core: in-call device switching, background blur, noise suppression controls,
  speaker selection, pinning a tile, a "you are muted" detector while talking (LiveKit gives no level for a muted
  microphone; the joined-muted toast and the shortcut toasts cover the common case).
