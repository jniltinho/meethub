# Proposal

## Why

The chat works (`add-chat-realtime`), but its screens are still far from the product mockup
(`base/Interface MeetHub em Modo Escuro.png`): there is no team rail, the channel header has no actions, messages
cannot be pinned or searched, people show as initials, and the side panel is empty until a thread opens. Validation
against the mockup and the Mattermost reference showed these gaps are what makes the product look unfinished, so they
come before files and meetings. Four layout studies were drawn in FigCraft (`base/layouts/`, private): study **A**
(team rail, sidebar, conversation, details panel) was chosen for the chat, and the full-screen room of study D for the
meetings that `add-livekit-meetings` will bring.

## What Changes

- **Visual contract** — **BREAKING** for the themes: subtly rounded corners (4, 6 and 8 px) replace the square
  corners in all 22 themes, and `meethub-dark`, `meethub-light` and `high-contrast` reach WCAG AAA with a retuned
  MeetHub palette; the other themes keep AA. Rule 7 of `AGENTS.md` changes with it.
- **Layout from the mockup** (study A) — **BREAKING** for the shell: the 64 px rail becomes a **team rail** (workspace logo, one
  tile per team with unread and mention badges, "+" to browse or create teams) and the section navigation
  (Conversations, Files, Meetings, Activity, Administration) moves to the top of the sidebar, with Settings at its
  foot. The sidebar's team switcher stays.
- **Sidebar**: collapsible sections (Favorites, Channels, Direct messages) and **favorite channels** (the star of the
  channel header), listed first.
- **Channel header**: star, Search, Members (count) and a "more" menu; the Files and "Start meeting" buttons of the
  mockup arrive with `add-file-uploads` and `add-livekit-meetings`.
- **Channel details in the side panel**: about (purpose and header), participants with presence and role, pinned
  messages. A thread or the search results take its place while open.
- **Pinned messages** (Mattermost pins): pin and unpin a message, list a channel's pins, the "pinned by" banner at the
  top of the channel, live for every member.
- **Message search** (Mattermost search): the header's Search runs over the channels and conversations the user
  belongs to, with `from:` and `in:` modifiers and quoted phrases; results open in the side panel and jump to the
  message in its channel, which needs loading history **around** a message.
- **Profile pictures**: upload, replace and remove one's picture; it shows in messages, the sidebar, the member list
  and the user footer, initials remaining the fallback.
- **Timeline and composer details**: day separators with the date ("Today, Jan 21"), an "add reaction" button after a
  message's reactions, and link, bulleted list, numbered list and mention buttons in the composer toolbar.
- **Quick theme switcher** in the sidebar foot: four themes one click away, plus a link to all 22.

Out of scope: file attachments and the recent-files block (`add-file-uploads`); meeting rooms, the meeting card and
upcoming meetings (`add-livekit-meetings`); saved messages, channel categories other than Favorites, custom emoji,
search across archived channels, full-text ranking.

## Capabilities

### New Capabilities

- `pinned-messages`: pinning and unpinning messages, who may do it, the pinned list of a channel and its live updates.
- `message-search`: searching the messages a user may read: query syntax, scope, limits and result shape.
- `profile-images`: a user's profile picture: upload rules, processing, serving, visibility and removal.

### Modified Capabilities

- `web-shell`: the application layout (team rail, navigation in the sidebar), its phone layout and the accessible names
  that referred to the navigation rail.
- `chat-ui`: sidebar (favorites, collapsible sections), channel view (header actions, dated separators, pictures,
  pinned banner), composer toolbar; new requirements for the team rail, channel details, search and jumping to a
  message.
- `channels`: a member may mark a channel as favorite; channel lists carry that flag.
- `messages`: history can be loaded around a given message.
- `user-directory`: the public profile carries the picture's version.
- `realtime-events`: the event catalog gains `user_updated` and `preferences_changed`.
- `ui-themes`: the theme contract (shared corner radii instead of square corners), contrast (AAA for the three
  MeetHub themes), the MeetHub palette, and a quick theme switcher beside the full picker.

## Impact

- **Database**: migration `00003` on SQLite, MariaDB and PostgreSQL (pin columns on posts, favorite on channel
  members, picture version on users, a picture table).
- **API** (`/api/v1`): pin/unpin and pinned list, search, profile image upload/get/delete, favorite, history around a
  post; `docs/api.md` and the event catalog change with them.
- **Backend**: `internal/chat` (pins, search, favorites, history), `internal/identity` (pictures),
  `internal/httpserver/handlers` and a per-route body limit in `internal/httpserver`; no new dependency.
- **Frontend**: `AppLayout.vue`, the chat components and store, `SettingsView.vue`, the translation catalogs, the
  radius tokens and the MeetHub palettes in `style.css` and the theme files, contrast recipes and gallery; the
  Playwright suites that address the navigation rail change with the layout and the 22 gallery screenshots are
  regenerated.
- **Docs and rules**: `AGENTS.md` (rule 7), `docs/themes.md`, `docs/brand.md` (palette), `docs/api.md`.
