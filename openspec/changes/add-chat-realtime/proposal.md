# Proposal

## Why

The foundation is in place (server, sessions, roles, audit, themed shell), but MeetHub still has no chat: the
Conversations and Teams sections are placeholders. Persistent team chat is the product's core and the base the LiveKit
meetings attach to (a meeting is started from a channel), so it comes next. Mattermost is the functional base, so the
model, API shapes and real-time events mirror it instead of being invented.

## What Changes

- **Usernames** for every user (Mattermost style, `@ana.silva`), required for mentions and direct messages; existing
  users get one derived from their e-mail.
- **Mattermost baseline v12.0.2**: entity names, limits, WebSocket event names and envelopes follow it; deliberate
  deviations are listed in design.md.
- **Teams** inside the organization (Mattermost teams: the mockup's "Workspace" switcher), with team roles
  (`team_admin`, `team_user`), open teams anyone may join and invite-only teams; a default team is created for the
  existing organization.
- **Channels** per team: public (`O`) and private (`P`), with name, display name, purpose and header, channel roles
  (`channel_admin`, `channel_user`), join/leave, members, archive; every team has a default `general` channel.
- **Direct and group messages**: 1:1 (`D`) and group conversations of 3–8 people (`G`) outside teams, as in Mattermost.
- **Messages** (Mattermost posts): create, edit (own), delete (own, or by channel/team/system admins, soft delete),
  threads (`root_id`, reply count, last reply), idempotent creation (`pending_post_id`), Markdown rendered safely in the
  browser, paginated history and "since" catch-up.
- **Reactions** with emoji names, added and removed per user.
- **Read state**: per-channel unread message and mention counts (`@username`, `@channel`, `@here`), mark as read,
  totals per team.
- **Presence and typing**: online / away / offline / do-not-disturb with automatic away and expiry, "is typing"
  indicators.
- **Real-time events** over one authenticated WebSocket (`/api/v1/websocket`) with Mattermost event names (`posted`,
  `post_edited`, `post_deleted`, `reaction_added`, `typing`, `status_change`, `channel_viewed`…), fanned out across
  server replicas through Redis Pub/Sub, sequence numbers and reconnection catch-up over the REST API.
- **User directory** with privacy rules: public profile fields only, guests see only people they share channels with.
- **Chat UI** from the mockup: team switcher, channel and direct-message sidebar with unread badges, message timeline
  with day separators, composer, thread side panel, reactions, presence dots, channel header with members; works in all
  22 themes and in English, Portuguese and Spanish.
- Audit events for team and channel administration; per-user rate limits on messages.

Out of scope (later changes): file attachments (`add-file-uploads`), LiveKit meetings (`add-livekit-meetings`), search,
pinned and saved messages, push/e-mail notifications, custom emoji, message retention policies, bots and webhooks.

## Capabilities

### New Capabilities

- `teams`: teams, team membership and team roles, open versus invite-only teams, the default team.
- `channels`: public and private channels inside a team, channel roles, membership, archive, the default channel.
- `direct-messages`: 1:1 and group conversations outside teams and who may start them.
- `messages`: posting, editing, deleting, threads, idempotency, history pagination and catch-up, rendering rules.
- `reactions`: adding and removing emoji reactions on messages.
- `read-state`: unread and mention counts, mention rules and marking channels as read.
- `presence`: user status (online, away, offline, do not disturb) and typing indicators.
- `realtime-events`: the authenticated WebSocket stream, event catalog, delivery rules across replicas and reconnection.
- `user-directory`: which users a caller may see, the public profile fields and directory queries.
- `chat-ui`: the conversation screens: sidebar, timeline, composer, thread panel, reactions and presence in the SPA.

### Modified Capabilities

- `organizations`: users gain a unique username; the permission matrix gains `create_team`.
- `audit-log`: team and channel administration events are recorded.
- `web-shell`: Conversations and Teams stop being placeholders; opening a channel focuses the composer.

## Impact

- Backend: new packages `internal/chat` (teams, channels, posts, reactions, read state), `internal/realtime` (WebSocket
  hub, Redis Pub/Sub), `internal/presence`; new migrations on the three dialects; new `/api/v1` routes documented in
  `docs/api.md`.
- Dependencies: a WebSocket library for Go (`github.com/coder/websocket`), Redis Pub/Sub (already required),
  `markdown-it` and `DOMPurify` in the SPA.
- Mattermost: model code adapted only from its Apache-2.0 `server/public/model` (channel naming, DM names, mention
  parsing) with attribution in `NOTICE`.
- Tests: Go tests on the three databases and Redis, a WebSocket test client, and Playwright two-browser chat flows in
  `make test-e2e`.
