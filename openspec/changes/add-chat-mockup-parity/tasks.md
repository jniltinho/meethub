# Tasks

Dependency order: 1 → 2 → (3, 4, 5, 6 in any order) → 7 → 8 → 9. Every Go task runs its tests with `-race` on SQLite,
MariaDB and PostgreSQL (`make test-db`) unless it says otherwise. The reference for every screen is study A of
`base/layouts/` (private, never committed).

## 1. Visual contract

- [x] 1.1 Add the radius tokens (`--radius-sm/md/lg` = 4/6/8 px) to the contract defaults and use them in the shared
  classes (buttons, inputs, panels, toasts, dialogs, popovers, chat items); verify the contract test reads 6 px on
  controls and 8 px on containers in all 22 themes and the lockstep test fails a theme file that declares a radius
- [x] 1.2 Retune `meethub-dark` to the palette of the `ui-themes` delta and derive `meethub-light`; adjust
  `high-contrast` where it fails; give the contrast test a level per theme (AAA for those three, AA for the rest);
  verify the contrast test passes on all 22 themes at their level, muted text and primary buttons included
- [x] 1.3 Update rule 7 of `AGENTS.md`, `docs/themes.md` and the palette table of `docs/brand.md`, and regenerate the
  22 gallery screenshots; verify the themes documentation test and `make test-e2e E2E_DBS=sqlite` pass with the new
  baselines

## 2. Schema

- [x] 2.1 Write migration 00003 for the three dialects (pin columns and index on `posts`, `favorite` on
  `channel_members`, `last_picture_update` on `users`, `user_images`) with down steps; verify the same-versions test
  and up/down/up on the three dialects

## 3. Pinned messages

- [x] 3.1 Implement pin and unpin under the channel lock (revision bump, idempotence keeping the first author,
  archived channel, deleted message, non-member as 404), the pinned list and the pin cleared by deletes, with
  `is_pinned`, `pinned_by`, `pinned_at` in the projection and `post_edited`; verify every scenario of the
  `pinned-messages` spec, including catch-up with `since_revision`
- [x] 3.2 Add the routes with their error mappings and document them in `docs/api.md`; verify `httptest` tests of the
  main and error paths and the route documentation test

## 4. Favorites, history around a message, events

- [x] 4.1 Implement the favorite flag (endpoint, `favorite` in the channel and conversation lists,
  `preferences_changed` to the caller's other connections only); verify the `channels` scenarios with two connections
  of one user and a second user who receives nothing
- [x] 4.2 Implement `around` and `after` in the history with both cursors; verify the `messages` scenarios, equal
  timestamps at the window's edges and a walk forward that ends at the newest message
- [x] 4.3 Add `user_updated` and `preferences_changed` to the event catalog with payload fixtures in `docs/api.md`;
  verify the fixture test

## 5. Message search

- [x] 5.1 Implement the query parser (words, quoted phrases, `from:`, `in:`, limits) as a pure function; verify a
  table-driven test of valid and invalid queries, wildcard characters included
- [x] 5.2 Implement the search query scoped by the caller's memberships (not deleted, not archived, case-insensitive,
  newest first, `has_more`, `channels`) with the `shortcut:` comment of design D5 and the 10-per-10-s limit; verify
  the `message-search` scenarios on the three dialects, including a term with `%` and `_`, and the limit across two
  replicas
- [x] 5.3 Add `POST /posts/search`, document it, and add a search step to `make test-load`; verify `httptest` tests
  and that the load test prints the search latency with its threshold

## 6. Profile pictures

- [x] 6.1 Implement the image pipeline in `internal/identity` (sniff, header limits before decoding, center crop,
  256 px, PNG) with the standard library only (a box filter scales the picture; no new dependency); verify table-driven tests
  with PNG, JPEG and GIF inputs, EXIF removed, a renamed HTML file, an oversized file and a decompression bomb
- [x] 6.2 Implement upload, removal (own and by `manage_users`, audited), serving with visibility, cache headers and
  ETag, `last_picture_update` in the public profile and `user_updated`; verify the `profile-images` and
  `user-directory` scenarios, including a guest who gets 404 for a stranger
- [x] 6.3 Document the routes and the security notes (limits, re-encoding, visibility) in `docs/api.md` and
  `docs/security.md`; verify the route documentation test

## 7. Shell layout

- [x] 7.1 Turn the rail into the team rail (tiles with initials, current mark, unread dot and mention count, the
  browse-or-create button) and move the section navigation, Settings and the user into the sidebar; verify Playwright:
  tiles for two teams, a mention in another team reaching its tile live, switching teams reopening the last channel
- [x] 7.2 Adapt the phone layout (team tiles in the drawer, section links in the bottom bar) and update every e2e
  suite that addressed the navigation rail; verify the responsive suite, the Full HD/QHD/4K test and the axe run
- [x] 7.3 Add the quick theme switcher to the sidebar's foot; verify Playwright: one click changes the theme and the
  preference, and the link opens Appearance

## 8. Chat screens

- [x] 8.1 Add `UserPicture` and use it in messages, the sidebar, the shell's foot and member lists; add the picture
  controls to Settings → Profile; verify Playwright with two browsers (a new picture shows to the other within a
  second) and the themed error for a non-image
- [x] 8.2 Rework the channel header (star, Search, Members with count, "more" menu with details, leave, edit and
  archive) and the sidebar (Favorites section, collapsible sections kept per browser); verify Playwright for the
  star, the collapsed section after a reload and the menu of a member versus an administrator
- [x] 8.3 Implement the side panel states and the channel details (about, participants with presence and role,
  pinned list); verify Playwright: the Members button, a thread replacing and restoring the details
- [x] 8.4 Implement pin and unpin in the message actions, the pinned mark and the banner with dismissal; verify the
  two-browser pin scenario and the dismissed banner after a reload
- [x] 8.5 Implement the search panel (results with highlight, paging, empty, invalid and rate-limited states) and
  jumping to a message with the detached timeline and "Jump to recent"; verify Playwright against the demo data: a
  result of `#backend` opens at the message, scrolling down reaches the present
- [x] 8.6 Add the dated day separators, the "add reaction" button after reactions and the composer's link, list and
  mention buttons; verify unit tests of the text transforms and of the separator text in the three languages
- [x] 8.7 Add the pt-BR and es strings, the contrast recipes and gallery entries of the new elements; verify the
  catalog test and the contrast test at each theme's level

## 9. Integration

- [x] 9.1 Extend `scripts/demo/seed_chat.py` with pins, favorites and a few profile pictures; verify `make demo`
  twice (idempotent) and by comparing `#projetos` as Ana with study A side by side
- [ ] 9.2 Run `make check`, `make test`, `make test-db`, `make test-e2e`, `make test-load`, `make build`, CI green,
  and a Codex review of the implementation with its findings fixed; verify the CI run and a new `dist/meethub`

## Workflow follow-up

- Archive with `openspec archive add-chat-mockup-parity` after review.
- Next changes: `add-file-uploads` (attachments, the Files button and recent files), `add-livekit-meetings` (the
  full-screen room of design D11, "Start meeting", the room card and upcoming meetings).
