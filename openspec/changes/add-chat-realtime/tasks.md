# Tasks

Dependency order: 1 → 2 → 3 → 4 → 5 → 6 → 7 → 8 → 9. Every Go task runs its tests with `-race` on SQLite, MariaDB and
PostgreSQL (`make test-db`) unless it says otherwise.

## 1. Schema and building blocks

- [x] 1.1 Write migration 00002 (three dialects): username columns, `teams`, `team_members`, `channels` (with
  `revision`), `channel_members` (with `visible`), `posts` (with `revision`), `reactions`, keys and indexes of D2, down
  steps; verify the same-versions test and up/down/up on the three dialects
- [x] 1.2 Add usernames to `internal/identity` (validation, reserved words, normalization, derivation with unique
  suffixes, `PATCH /me` `username`, public profile projection without e-mail); verify table-driven tests including
  collisions and concurrent derivation
- [x] 1.3 Create `internal/ratelimit` (move the failed-login reservation unchanged; add the Redis Lua sliding-window log with
  memory fallback); verify the auth tests still pass and window tests (never above the limit in any interval), across two replicas and with
  Redis down
- [x] 1.4 Port the Mattermost model helpers (DM/GM names, name rules, limits) with `NOTICE` entries and add the payload
  fixtures directory; verify unit tests against Mattermost's documented examples

## 2. Teams, channels, conversations

- [x] 2.1 Implement `internal/chat/policy.go` (D4 access matrix, guests, 404 rules) and `create_team` in `internal/org`;
  verify a table-driven test of every matrix cell
- [x] 2.2 Implement the per-channel lock and revision helper (D3) with the fixed lock order; verify a barrier test where
  a removal and a post race on all three dialects (post stored only if committed first)
- [ ] 2.3 Implement teams: create, update, archive/restore (not the default team), join open teams, add/remove members
  (removal drops all team channel memberships in the same transaction), team roles (no self-change), lists, audit;
  verify every teams scenario including private-channel access after team removal
- [ ] 2.4 Implement channels: create, update (name, display name, purpose 250, header 1024), archive/restore, join/leave,
  add/remove, roles (no self-change), default channel rules, paginated lists, audit (rollback on audit failure); verify
  every channels scenario including archive racing a post
- [ ] 2.5 Implement direct and group conversations (atomic creation with all members via `ON CONFLICT DO NOTHING`,
  fixed membership, symmetric guest rule, inactive users, visibility flag, conversation list); verify concurrent DM, GM
  and self-DM creation on the three dialects
- [ ] 2.6 Implement `chat.EnsureMemberships` inside `Initialize`, `org.Users.Create` and the CLI, and `chat.Backfill`
  under its own lock run by `server` and the CLI before serving (D11); verify an upgrade test from a foundation-era
  database, concurrent starts and an interrupted backfill resumed

## 3. Presence

- [ ] 3.1 Implement `internal/presence` (Lua transitions, heartbeats, activity, `dnd` with or without expiry, the leased
  sweeper, stale statuses during outages, re-registration on recovery, one visibility predicate for push, batch and
  socket queries); verify crash detection within 75 s (with a test clock), reconnect on another replica, simultaneous
  tab close, lease expiry and a Redis outage

## 4. Messages, reactions and read state

- [ ] 4.1 Implement posting under the channel lock (validation, `pending_post_id` rules and 409 conflict, portable
  conflict handling, counters, root re-stamp, author marked read, rate limit after the idempotency check); verify
  concurrent identical retries across replicas, changed payloads, retries after access loss
- [ ] 4.2 Implement edit and soft delete (tombstone projection without text, reply count/last reply recompute, thread
  cascade with one event, moderator audit) and history (cursor) plus catch-up (`since_revision`, `next_revision`,
  `has_more`); verify equal timestamps, > 200 changes paged during concurrent writes, and the 1 000-message ordering test
- [ ] 4.3 Implement reactions (gemoji names, idempotent add, archived channels, revision bump, grouped output with at
  most 25 users per emoji); verify the reactions scenarios
- [ ] 4.4 Implement mentions (code stripped, `@username`, `@channel`, `@all`, `@here` via presence, single count per
  user, DM/GM rules) and read state (monotonic view, new members start read, team totals); verify concurrent view and
  mention, double mention and mention-in-code scenarios

## 5. Real-time

- [ ] 5.1 Implement `internal/realtime` hub and connections (handshake checks, 10 connections per user, pumps, limits,
  buffer by count and bytes, keepalive, `seq`, Mattermost envelopes, `hello`, `pong`, unknown actions); verify with a
  WebSocket test client including the slow-consumer scenario
- [ ] 5.2 Implement session revalidation every 5 s with fail-closed behavior and the bus control fast path; verify
  logout, self password change, administrator and CLI resets, disable and idle/absolute expiry closing sockets within
  5 s with Redis down
- [ ] 5.3 Implement the bus (local-first delivery, replica ids, subscription health, `resync` after resubscribe) and
  recipients resolved inside the writing transaction, dropping queued events on removal; verify two replicas, a
  removal racing an event, and a lost subscription with sockets kept open
- [ ] 5.4 Wire `cmd/server.go` (hub, revalidation, bus, sweeper and Asynq in the errgroup; ordered shutdown with
  close status 1001); verify a shutdown test with open sockets and a rolling-restart reconnect

## 6. API and documentation

- [ ] 6.1 Add the handlers of D10 with error mappings (`name_taken`, `not_team_member`, `default_channel`,
  `default_team`, `channel_archived`, `direct_channel`, `user_inactive`, `invalid_username`, `pending_post_conflict`) and
  the user-directory privacy rules; verify `httptest` tests for each route's main and error paths and that no ordinary
  response contains an e-mail of another user
- [ ] 6.2 Update `docs/api.md` (routes, envelopes, event catalog with payload fixtures), `docs/deployment.md`
  (WebSocket proxy headers, timeouts, scale budget) and `NOTICE`; verify the route documentation test and the fixture test

## 7. Chat UI

- [ ] 7.1 Add `markdown-it`, `DOMPurify` and `gemoji`; implement the fixed rendering pipeline of D9; verify unit tests for
  every XSS payload of the messages spec, images as links and malicious display names
- [ ] 7.2 Implement the socket client (backoff, `resync`, gaps, 60 s resync, logout cleanup) and the revision-aware
  `chat` store; verify Vitest tests with a fake socket including out-of-order and duplicate events
- [ ] 7.3 Implement routes, fallbacks and the sidebar (team switcher with badges, channels, conversations with presence,
  create/browse channel, new direct/group message) replacing the Conversations and Teams placeholders; verify Playwright
- [ ] 7.4 Implement the channel view (header, grouped timeline with day separators, infinite scroll, mark as read) and
  the composer (Enter/Shift+Enter, pending/failed with Retry, emoji picker, autocomplete, typing); verify Playwright
- [ ] 7.5 Implement the thread panel and message actions (reply, react, edit in place, delete with themed confirm),
  live updates and the reconnecting state; verify the two-browser Playwright scenarios
- [ ] 7.6 Add pt-BR and es strings, chat contrast recipes (mention highlight, unread badge, presence on the sidebar) and
  gallery entries; verify the catalog test and the contrast test on all 22 themes
- [ ] 7.7 Keyboard and screen readers (composer focus and channel announcement on chat routes, Alt+Up/Down, Up to edit,
  polite live region); verify behavioral Playwright keyboard tests and an axe run with no serious violations

## 8. Integration

- [ ] 8.1 Two-browser e2e on SQLite, MariaDB and PostgreSQL: post, edit, react, delete, thread reply, typing, presence,
  DM and group message, unread badges across teams, forced disconnect and Redis restart without duplicates, logout then
  another user on the same tab, XSS payloads inert; verify `make test-e2e`
- [ ] 8.2 Load test at the D13 budget (1 000 connections, a 500-member `@channel`, slow consumers, a Redis restart) in a
  Make target; verify it meets the latency and correctness thresholds it prints
- [ ] 8.3 Update the localhost example environment (`make up`) with sample teams, channels and conversations; verify by
  logging in as two sample users
- [ ] 8.4 Run `make check`, `make test`, `make test-db`, `make test-e2e`, `make build`, CI green, and a Codex review of the
  implementation with its findings fixed; verify the CI run and a new `dist/meethub`

## Workflow follow-up

- Archive with `openspec archive add-chat-realtime` after review.
- Next changes: `add-file-uploads` (attachments), `add-livekit-meetings` (meetings started from channels).
