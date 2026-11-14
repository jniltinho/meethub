# Tasks

## 1. Configuration and deployment

- [ ] 1.1 Add `[livekit]` (`url`, `api_url`, secret `api_key` and `api_secret`) to `internal/config` with validation
  (all or none, secret ≥ 32 bytes, `wss://` in production, `api_url` default) and table-driven tests; document it in
  `config.toml.example`
- [ ] 1.2 Add the LiveKit origins to the CSP `connect-src` when meetings are enabled (`internal/httpserver`), with a test
- [ ] 1.3 Add the `livekit` service (pinned image, `LIVEKIT_CONFIG`, webhook to MeetHub, ports) to
  `deployments/compose/compose.yml` and the e2e overrides to `compose.e2e.yml`; make `make init` append a generated key
  and secret to `.env` when missing

## 2. Server

- [ ] 2.1 Migration `00005` (`meeting_rooms`, `meeting_participants`) for SQLite, MariaDB and PostgreSQL, checked by the
  migration tests on the three databases
- [ ] 2.2 Export `chat.Service.ReadableChannel` and `chat.Service.MemberIDs` (new file in `internal/chat`)
- [ ] 2.3 `internal/meetings`: room creation with a random name (concurrent-safe) and token issuing with the read rule,
  archived refusal and the D3 grants; tests on the three databases decode the token and check its claims
- [ ] 2.4 Webhook handling (signature, joined/left/aborted with the sid rule, room finished, unknown room) and
  `meeting_updated` to channel members; tests
- [ ] 2.5 Reconciliation loop: eviction query (removed, deactivated, archived, left the team) and `ListParticipants`
  refresh through a small room-service interface with a LiveKit implementation; tests with a fake
- [ ] 2.6 Meetings list and single-room queries; tests (only readable rooms, empty room)
- [ ] 2.7 Handlers (`meetings.go`: list, room, token, webhook with its body limit, `meetings_unavailable`), wiring in
  `cmd/` (service, loop in the errgroup) and handler tests; document the endpoints and the event in `docs/api.md`

## 3. Web app

- [ ] 3.1 Add `livekit-client`; `stores/meetings.ts` (enabled, rooms, load, event, resync hook in `stores/chat.ts`)
  with unit tests
- [ ] 3.2 `components/meetings/`: pre-join screen (preview, toggles, device errors), room overlay (grid, tiles, bottom
  controls, screen share, raise hand, reactions, more, leave), chat and people panel, focus trap and announcements
- [ ] 3.3 Channel header "Start meeting" / "Join meeting (n)", the details panel's room card and "In the room" marks,
  the sidebar's "Meeting rooms" section
- [ ] 3.4 Meetings page replacing the placeholder route
- [ ] 3.5 Theme tokens and styles for the room (shared radii, AA everywhere, AAA in the MeetHub themes) and strings in
  `pt-BR.ts` and `es.ts`

## 4. Tests and validation

- [ ] 4.1 Playwright `08-meetings.spec.ts`: two users join with fake devices, tiles, controls, presence in the details
  panel and the sidebar through the webhook, leave; contrast recipes in `02-themes.spec.ts`; update screenshots that
  change
- [ ] 4.2 `make check && make test && make test-db && make test-e2e && make build` pass
- [ ] 4.3 Codex review of the diff; fix its findings
