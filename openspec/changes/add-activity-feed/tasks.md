# Tasks

Every Go task runs its tests with `-race` on SQLite, MariaDB and PostgreSQL (`make test-db`).

## 1. Server

- [x] 1.1 Write the migration (`activity_items`, `users.activity_seen_at`) for the three dialects; verify
  up/down/up
- [x] 1.2 Create items in the posting transaction (direct mentions, earlier thread authors, never the author, one
  per person) and remove them with the message; verify the `activity` creation scenarios, a person both mentioned
  and in the thread, and a deleted message
- [x] 1.3 Add the list, the unseen count, the "seen" route and `activity_updated` (event fixture and catalog row);
  verify access after leaving a channel, paging, and the events to the right connections only
- [x] 1.4 Add the daily retention task; verify the retention scenario with two replicas

## 2. Web app

- [x] 2.1 Replace the Activity placeholder with the page (items, load on scroll, open at the message) and add the
  unseen badge to the navigation entry in the rail, the sidebar and the bottom bar; verify Playwright with two
  browsers: mention, reply in a thread, badge live and cleared in another tab
- [x] 2.2 Add the pt-BR and es strings and the contrast recipe of the badge; document the routes and the event in
  `docs/api.md`
  - The Activity entry exists in the sidebar's section navigation and in the phone's bottom bar; the team rail holds
    only teams (there is no Activity entry there), so the badge is on those two.

## 3. Validation

- [ ] 3.1 Run `make check`, `make test`, `make test-db`, `make test-e2e`, `make build`, a Codex review with its
  findings fixed, and CI green
  - Open for CI only (it runs after a push, not this step); everything else is recorded in the implementation
    report.

## Workflow follow-up

- Archive with `openspec archive add-activity-feed` after review.
