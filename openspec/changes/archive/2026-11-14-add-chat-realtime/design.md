# Design

## Context

The foundation (archived `2026-11-13-add-meethub-foundation`) provides the Echo v5 API with JSON errors, CSRF and
Origin checks, database sessions, the org permission matrix, the audit log (written in the caller's transaction), Asynq
on a required Redis, goose migrations per dialect with connection-scoped locks, row-lock helpers
(`identity.Store.LockByID`, `org.lockOrganization`), the Vue shell with themes, toasts and dialogs, and Playwright e2e on
three databases. Requirements are in this change's specs; motivation in proposal.md.

**Mattermost baseline: v12.0.2.** Its `server/public/model` and `webapp/` are Apache-2.0 and may be ported with
attribution; `server/channels` (app, store, hub) is AGPL and only read.

## Goals / Non-Goals

**Goals:** Mattermost behavior for teams, channels, DMs/GMs, threads, reactions, unread and mentions, presence;
correct delivery (stored before announced, authorized at commit, lossless recovery); the mockup's UI; tests that
exercise races and outages, not only happy paths.

**Non-Goals:** collapsed-threads inbox, search, pins, saved items, notifications, files, custom emoji, bots, channel
favorites, guaranteed WebSocket delivery.

## Decisions

### D1. Packages and runtime lifecycle

```text
internal/chat/        team.go channel.go direct.go post.go reaction.go readstate.go mention.go policy.go bootstrap.go
internal/chat/emoji/  emoji short names (generated from gemoji, MIT)
internal/realtime/    hub.go conn.go bus.go revalidate.go events.go
internal/presence/    presence.go sweeper.go typing.go (Lua scripts for atomic transitions)
internal/ratelimit/   failed-login reservation and sliding-window log (Redis Lua) with memory fallback
internal/httpserver/handlers/  teams.go channels.go conversations.go posts.go users.go websocket.go
```

`chat` depends on `database`, `identity`, `org`, `audit` and a `Publisher` interface implemented by `realtime`.
`cmd/server.go` owns everything in its errgroup: the hub (and its revalidation loop), the bus subscriber, the presence
sweeper, Asynq; on shutdown it stops accepting upgrades, closes sockets with status 1001 within the shutdown timeout,
then stops loops in reverse order.

### D1b. Reusing Mattermost

Ported with attribution (`NOTICE` + file headers): DM/GM channel naming (`GetDMNameFromIds`,
`GetGroupNameFromUserIds`), name validation and limits from `server/public/model`; from `webapp/` (logic ported to
TypeScript, not React components): post grouping (same author within 5 minutes), date separators, mention/channel
highlighting and autocomplete matching, emoji aliases, WebSocket reconnect/backoff and `seq` gap handling, sidebar
ordering.

### D2. Schema (migration 00002, three dialects)

- `users.username`, `users.username_normalized` — nullable unique columns (byte-wise collation as the e-mail), filled
  by the bootstrap (D11).
- `teams(id, name UNIQUE, display_name, description, type, is_default, archived_at, created_at, updated_at)`;
  `team_members(team_id, user_id, role, created_at)` PK `(team_id, user_id)`, index `(user_id)`.
- `channels(id, team_id, type, name, display_name, purpose, header, creator_id, is_default, revision,
  total_msg_count, last_post_at, archived_at, created_at, updated_at)`, unique `(team_id, name)`; DM/GM use
  `team_id = ''` so the unique index behaves the same everywhere.
- `channel_members(channel_id, user_id, role, msg_count, mention_count, last_viewed_at, visible, created_at)` PK
  `(channel_id, user_id)`, index `(user_id, visible)`. `visible` drives the conversation list (D/G); always true for
  O/P.
- `posts(id, channel_id, user_id, root_id, message, type, pending_post_id, create_at, update_at, edit_at, delete_at,
  reply_count, last_reply_at, revision)`; indexes `(channel_id, create_at, id)`, `(channel_id, revision)`,
  `(root_id, create_at, id)`; unique `(user_id, pending_post_id)`.
- `reactions(post_id, user_id, emoji_name, create_at)` PK `(post_id, user_id, emoji_name)`.
- Times are Unix milliseconds (Mattermost shape). **Ordering for recovery uses `revision`, never time.**

### D3. Per-channel serialization and the revision

Every write that touches a channel's content or membership starts its transaction with
`UPDATE channels SET revision = revision + 1 … WHERE id = ? RETURNING revision` (MariaDB: `UPDATE` then
`SELECT revision`; SQLite: the write transaction is already exclusive). That row lock serializes, per channel: posts,
edits, deletes, reactions, joins, leaves, removals, role changes and archive/restore. Inside it, under the lock: re-read
the caller's membership and role, the channel's and team's archive state, and the thread root; then write; the touched
posts get `revision = new revision` (a reply also re-stamps its root). Commit order therefore equals revision order,
which makes `since_revision` catch-up lossless and gives every edit a distinct version.

Lock order, everywhere: organization row → user row → team row → channel row → member/post rows. Team-wide operations
(team archive, team removal) lock the team row, then each affected channel row in id order.

Posting: check the idempotency key first (`SELECT` by `(user_id, pending_post_id)` under the channel lock); if absent,
`INSERT … ON CONFLICT DO NOTHING` through GORM's `OnConflict{DoNothing: true}` (rendered per dialect, never aborting a
PostgreSQL transaction); zero rows → the concurrent winner exists: re-read it, compare channel/root/text (409
`pending_post_conflict` if different), re-check read access, return it without an event. Then `total_msg_count + 1`,
`last_post_at`, root counters, the author's read position, mentions (D6).

Deleting a reply: `reply_count - 1` and `last_reply_at` recomputed from remaining replies. Deleting a root: root and
replies get `delete_at` and the same revision; one `post_deleted` with `delete_thread: true`. Tombstones in API/events
carry no text; the text column is kept (for a future retention/compliance change) and never selected by user-facing
queries (a dedicated projection).

### D4. Authorization matrices

Access (system owners act through "administer"):

| Action | Public channel | Private channel | DM / GM |
| --- | --- | --- | --- |
| Discover (name, purpose, member count) | team members except guests | its members | its members |
| Read history, threads, reactions, members | its members | its members | its members |
| Join | team members except guests | — | — |
| Post, react, type | its members | its members | its members |
| Administer (edit, archive, members, roles) | channel admins, team admins, `manage_system` | same | — (fixed) |

All channel rules also require membership of the channel's team (DM/GM: none). Not discoverable → 404.

Operations → permission → events (each privileged one audited in its transaction; an audit failure rolls it back):

| Operation | Permission | Events (recipients) |
| --- | --- | --- |
| Create team | `create_team` | `added_to_team` (creator) |
| Update / archive / restore team | team admin, `manage_system`; not the default team | `update_team` / `delete_team` / `restore_team` (members) |
| Add / remove team member | team admin (join: self, open teams, non-guests) | `added_to_team` (user), `leave_team` (user), `user_removed` per channel (remaining members) |
| Change team role | team admin, not self | `user_role_updated` (user + team admins) |
| Create channel | team member, not guest | `channel_created` (creator; public: team members except guests) |
| Update / archive / restore channel | channel admin, team admin, `manage_system` | `channel_updated` / `channel_deleted` / `channel_restored` (members) |
| Join / leave / add / remove channel member | per D4 access; default channel cannot be left | `user_added` (members), `user_removed` (members + removed user) |
| Change channel role | channel admin, not self | `channel_member_updated` (user + channel admins) |
| Post / edit / delete / react | member; edit: author; delete: author or channel admin | `posted` / `post_edited` / `post_deleted` / `reaction_*` (members) |
| Delete someone else's post | channel admin | as above + audit `post.deleted_by_moderator` |

Guests: never discover or join public channels; DMs per the symmetric rule of direct-messages (a shared *team channel*
between each guest and each other participant at creation; existing conversations keep working).

### D5. Real-time delivery

- **Library and handshake:** `github.com/coder/websocket`; cookie session via `auth.Authenticate`; `Origin` must equal
  the public origin; at most 10 connections per user (oldest closed); `hello` first.
- **Pumps and limits:** read pump (64 KiB frames, 20 actions/s); write pump with a buffer of 256 events / 1 MiB;
  overflow closes the connection; ping every 30 s.
- **Revalidation (blocker fix):** every 5 s the hub validates all its sessions in one query
  (`sessions JOIN users` by session ids: not revoked, not expired, idle not exceeded, user active) and closes the
  failures; if the query cannot run for 15 s the hub closes all its connections (fail closed). Control messages on the bus
  (`session_revoked`, `user_disabled`) remain a faster path. Activity on a socket refreshes the session idle time at
  most once a minute (the `session.Store.Touch` rule).
- **Recipients and the authorization boundary:** recipients are resolved **inside the writing transaction, after the
  channel row lock** (D3), so they are exactly the members at commit time; membership changes take the same lock, so no
  event about a change committed after a removal can carry the removed user. On `user_removed` for a user, each hub
  drops that user's queued events of the channel. Events in a connection buffer that committed before the removal may
  still be delivered (they were authorized at commit).
- **Bus:** after commit an event is delivered to local connections and published on Redis channel `meethub:events`
  with the replica id; other replicas deliver to their connections. The subscriber tracks health; after a lost
  subscription it resubscribes with backoff and sends `resync` to all local connections.
- **Commit-then-crash gap:** a replica dying between commit and publish loses that event; clients bound this by
  resynchronizing the open channel and unread counts every 60 s (spec), and on any `resync`/gap/reconnect.

### D6. Mentions and read state

Parsed on the server from the stored text with code spans/blocks removed: `@username`, `@channel`, `@all`, `@here`
(word-bounded, case-insensitive, trailing `.`, `-`, `_` trimmed as Mattermost does); a set of user ids, minus the author.
`@here` uses presence `online`. DM/GM: every other member. Increments are `UPDATE channel_members SET mention_count =
mention_count + 1 WHERE channel_id = ? AND user_id IN (…)`, inside the channel lock. Viewing: `msg_count =
GREATEST(msg_count, total)` (portable `CASE`), `mention_count = 0`, also under the channel lock, so view and mention
order is the revision order. Posting marks the author read (Mattermost behavior, stated in the spec). New members start
with `msg_count = total_msg_count`.

### D7. Presence

Redis per user: a sorted set of `replica:conn` ids scored by heartbeat, `last_activity`, optional `dnd` (with or
without expiry), last published status. Heartbeat on `ping` (30 s) and on connect; `user_activity` (≤ 1 per 30 s) and
posts update activity. Transitions run in Lua scripts (atomic connect/heartbeat/disconnect). The sweeper runs every 15 s
on whichever replica holds a 20 s Redis lease, drops entries older than 60 s, recomputes statuses and publishes changes:
worst case 60 + 15 = 75 s (the spec's bound). During a Redis outage lookups return the hub's last known statuses with
`stale: true` and nothing is published; on recovery each replica re-registers its live connections. Status audience
(push, batch, `get_statuses`) is one predicate: shares a team (guests: a channel or conversation), resolved in SQL with
an index on `team_members(user_id)` and batched per sweep.

### D8. Rate limits

`internal/ratelimit` offers (a) the existing failed-login reservation (moved from `internal/auth`, unchanged behavior,
success releases), and (b) a sliding-window log (a Redis sorted set of the last 30 accepted timestamps per user, in a
Lua script on the Redis clock) for posts: exact "at most 30 in any 10 s" — a fixed window or a token bucket would allow
up to twice that within one interval. Only accepted new posts are recorded (idempotent repeats and rejected posts are
checked first); typing uses a 3 s per-user+channel `SET NX PX`. During Redis failure, per-replica memory windows with
the same parameters apply (documented as degraded).

### D9. Frontend

- Pinia `chat` store normalized by id; every entity keeps its `revision` and updates older than the stored one are
  ignored. Logout or a session end closes the socket and resets the store.
- `src/realtime/socket.ts`: backoff 1 s → 30 s with jitter; `resync`, `seq` gaps, reconnection and a 60 s timer trigger
  catch-up: `since_revision` per loaded channel (paged until `has_more` is false), unread counts, conversation list,
  memberships, statuses.
- **Rendering pipeline (fixed order):** `markdown-it` (`html: false`, image rule replaced by a link rule, link validation
  allowing `http`, `https`, `mailto` after decoding entities) → token-level transforms for `@mentions`, `~channels`,
  `:emoji:` (text inserted as escaped tokens, never HTML strings) → render → `DOMPurify` with an explicit allowlist (`p`,
  `br`, `strong`, `em`, `del`, `code`, `pre`, `blockquote`, `ul`, `ol`, `li`, `a[href,target,rel]`, `span[class]`,
  `h1`–`h6`, `table` family) → `v-html` of that string only; nothing is concatenated afterwards. Display names and other
  user data go through text bindings. Headers and purposes use the inline variant. No inline styles (CSP).
- Routes per the chat-ui spec; the last location is stored per user in localStorage and validated before use.
- Focus: chat routes focus the composer and announce the channel name in a polite live region (the web-shell delta).

### D10. API surface (under `/api/v1`)

Teams: `GET/POST /teams`, `GET/PATCH /teams/{id}`, `POST /teams/{id}/archive|restore`, `GET/POST /teams/{id}/members`,
`DELETE /teams/{id}/members/{user}`, `PUT /teams/{id}/members/{user}/role`, `GET /users/me/teams`.
Channels: `GET /users/me/teams/{id}/channels`, `GET /teams/{id}/channels` (joinable), `POST /channels`,
`GET/PATCH /channels/{id}`, `POST /channels/{id}/archive|restore`, `GET/POST /channels/{id}/members`,
`DELETE /channels/{id}/members/{user}`, `PUT /channels/{id}/members/{user}/role`, `POST /channels/{id}/view`.
Conversations: `POST /channels/direct`, `POST /channels/group`, `GET /users/me/conversations`,
`PUT /users/me/conversations/{id}/visibility`.
Posts: `GET /channels/{id}/posts?cursor|since_revision&per_page`, `POST /posts`, `PATCH/DELETE /posts/{id}`,
`GET /posts/{id}/thread?cursor`, `POST /reactions`, `DELETE /users/me/posts/{id}/reactions/{emoji}`.
Users: `GET /users?q&in_team&in_channel&cursor`, `POST /users/status/ids`, `PUT /users/me/status`; `PATCH /me` gains
`username`. Socket: `GET /websocket`. All documented in `docs/api.md` with payload fixtures.

### D11. Bootstrap (transaction-aware)

`chat.EnsureMemberships(ctx, tx, user)` adds a new non-guest user to the default team and its `general` inside the
caller's transaction (used by `auth.Initialize`, `org.Users.Create`, `meethub user create`), creating the default team
and channel when absent (singleton-guarded like the organization). Existing installs: after `migrate up`, `server`
runs `chat.Backfill` under a dedicated session lock (the migration locker with its own lock name) before serving:
usernames for users without one, the default team, and missing default memberships; idempotent and resumable after
interruption. With `--no-migrate` and pending migrations the server refuses to start; with no pending migrations the
backfill still runs. The CLI user commands run the same backfill first.

### D12. Compatibility with Mattermost v12.0.2

| Topic | Mattermost | MeetHub | Why |
| --- | --- | --- | --- |
| API prefix | `/api/v4` | `/api/v1` | MeetHub's own versioning |
| Times | ms | ms | same |
| Channel limits | name 64, display 64, purpose 250, header 1024 | same | same |
| Post length | 16383 runes | 16383 | same |
| WebSocket path | `/api/v4/websocket` | `/api/v1/websocket` | prefix |
| Event / response envelopes | `{event,data,broadcast,seq}` / `{status,seq_reply,data,error}` | same | same |
| Catch-up | `since` timestamp | `since_revision` (+ cursor) | lossless ordering (D3) |
| Conversations routes | `/:team/messages/@user` | `/messages/@user` | users without teams |
| Usernames | 3–22, `[a-z0-9.\-_]`, starts with a letter, reserved list | same rules, reserved `all`, `channel`, `here`, `system` | subset of reserved names |
| Collapsed threads, favorites, categories | yes | no (later) | scope |

Payload fixtures for each event and main response live in `internal/chat/testdata/` and are checked by tests.

### D14. Parity reference

`scripts/reference/mattermost.sh` runs the baseline (v12.0.2) on `localhost:8065` with the mockup's data; screenshots
live in `base/mattermost-reference/` (private). Observed behaviors the UI tasks follow: a hover action bar per message
(three recent quick reactions, add reaction, reply, more); reply count with participants' avatars and "Last reply …";
the thread side panel titled "Thread · <channel>" with a "New Messages" line and its own composer; the channel header
with member count and info toggle; "Today" day separators; a Browse Channels dialog with search, type filter, "Hide
Archived", "Hide Joined", a "Joined" mark and member counts; DMs appear in the sidebar only once opened or messaged
(our `visible` flag). Deliberate gaps for later changes: thread following, saved items, system join messages, user
avatars (initials only), search.

### D13. Scale budget

Target per install: 2 000 users, 5 000 concurrent connections across replicas (≤ 10 per user), channels up to 1 000
members, 50 posts/s sustained. Bounded collections: lists ≤ 200 per page, reaction users ≤ 25 per emoji inline (counts
exact), member lists paginated. Recipient lists in bus messages are acceptable at this size (≤ 1 000 ids); beyond it,
channel-scoped broadcasts with per-replica membership caches (Mattermost's approach) behind the same `Publisher`.
Load test (task 8.2): 1 000 connections, a 500-member channel with `@channel`, slow consumers and a Redis restart.

## Risks / Trade-offs

- [Every channel write takes the channel row lock] → short transactions; the hot spot is per channel, not global; the
  load test measures it at the budget.
- [Revalidation query every 5 s per replica] → one batched query by session ids, indexed; ~1 000 ids per replica.
- [Presence is eventually consistent (≤ 75 s)] → stated in the spec; stale flag during Redis outages.
- [Markdown and XSS] → fixed pipeline with final sanitizer, no images, CSP without inline scripts, e2e payloads.
- [Proxies and WebSockets] → `docs/deployment.md` gains upgrade headers and timeouts.

## Migration Plan

Migration 00002 adds the chat tables and nullable username columns (down drops them). The first `server` start after
the upgrade runs the backfill (D11) before serving. Rollback: previous image + `meethub migrate down` (chat data lost).

## Open Questions

None that change the specs or tasks.
