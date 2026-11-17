# Proposal

## Why

People cannot say how they are available. The server follows activity (online, away, offline) and has a route for
"do not disturb", but the interface never calls it: there is no way to choose a status, and no way to be away or
to appear offline by choice, as in Mattermost.

## What Changes

- **A status menu** on the user's own name at the foot of the sidebar: it shows their status and offers
  **Online** (follows activity), **Away**, **Do not disturb** — for 30 minutes, 1 hour, 2 hours, until tomorrow or
  until changed — and **Appear offline**.
- **Away and offline can be chosen**: a chosen status stays, while the user is connected, until they choose
  another; Online returns to following activity. Do not disturb works as before.
- `PUT /api/v1/users/me/status` sets `away` and `offline` by choice (it used to treat them as "follow activity"),
  and a status says whether it was chosen (`manual`).

## Capabilities

### New Capabilities

None.

### Modified Capabilities

- `presence`: which statuses a user may choose, and the menu that chooses them.

## Impact

- Code: `internal/presence` (a `manual` field in the state and its script), the status route in
  `internal/httpserver/handlers/chat.go`, a `StatusMenu.vue` in the sidebar's foot, the chat store, the i18n
  catalogs.
- API: `PUT /api/v1/users/me/status` — `away` and `offline` are now kept; the answer gains `manual`. **BREAKING**
  for a client that sent `away` or `offline` only to leave do not disturb: `online` does that.
- Docs: `docs/api.md`, `README.md`, `CHANGELOG.md`. No migration: the state is in Redis.
