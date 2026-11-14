# MeetHub API (`/api/v1`)

JSON over HTTPS. Errors always have the shape `{"error": {"code": "<machine code>", "message": "<English text>"}}`;
the web app translates by `code`. Timestamps are RFC 3339 in UTC; ids are ULIDs.

## Conventions

- **Session:** login and setup set an `HttpOnly; SameSite=Lax; Path=/` cookie — `__Host-meethub_session` (with
  `Secure`) when the public URL is HTTPS, `meethub_session` otherwise.
- **State-changing requests** (`POST`, `PATCH`, `PUT`, `DELETE`) must send `Content-Type: application/json` and an
  `Origin` equal to the public URL's origin (`403 csrf_failed` / `415 unsupported_media_type` otherwise).
  Authenticated ones also send the session's `X-CSRF-Token` (returned by login, setup and `GET /me` as `csrf_token`).
- **Unknown JSON fields are rejected** with `400 invalid_input`; malformed JSON is `400 bad_request`; bodies above
  `http.body_limit_bytes` (1 MiB) are `413 payload_too_large`.
- **Lists** are cursor-paginated: pass `limit` (default 50, max 200) and the previous page's `next_cursor`.

## Common error codes

| Status | Code | When |
| --- | --- | --- |
| 400 | `bad_request`, `invalid_input`, `weak_password`, `invalid_theme`, `invalid_language`, `invalid_username`, `not_team_member`, `direct_channel`, `user_inactive`, `too_many_files` | invalid request |
| 401 | `unauthenticated`, `session_expired`, `invalid_credentials`, `invalid_signature` | no or ended session, wrong login, bad LiveKit webhook signature |
| 403 | `forbidden`, `csrf_failed`, `invalid_setup_token`, `channel_archived` | not allowed |
| 404 | `not_found` | unknown route or resource |
| 405 | `method_not_allowed` | route exists with another method |
| 409 | `already_initialized`, `email_taken`, `last_owner`, `username_taken`, `name_taken`, `default_channel`, `default_team`, `pending_post_conflict` | conflicts |
| 413 | `payload_too_large`, `image_too_large`, `file_too_large` | body, picture or file too large |
| 415 | `unsupported_media_type` | not JSON |
| 429 | `rate_limited` | too many failed logins, messages, searches or uploads (`Retry-After`) |
| 503 | `busy`, `unavailable`, `storage_unavailable`, `meetings_unavailable` | password hashing saturated (`Retry-After`), dependency down, file storage down, LiveKit not configured |

## Probes and public data

| Route | Auth | Description |
| --- | --- | --- |
| `GET /api/v1/health` | — | Liveness: `200 {"status":"ok"}` while the process runs. |
| `GET /api/v1/ready` | — | Readiness: `200` or `503` with `{"status", "checks": {"database", "redis"}}` (`migrations pending`, `database unreachable`, `redis unavailable`). |
| `GET /api/v1/themes` | — | Theme catalog: `{"themes": [{id, label, scheme, default}], "default"}`. |
| `GET /api/v1/bootstrap` | — | `{"setup_required", "default_theme", "languages", "files": {"max_size_bytes", "max_per_post"}}` — nothing about users or the organization. |

## Authentication

| Route | Auth | Description |
| --- | --- | --- |
| `POST /api/v1/auth/setup` | setup token | First run only. Body `{setup_token, organization_name, display_name, email, password}`. Creates the organization and its owner atomically and logs in: `201` + profile. `403 invalid_setup_token`, `409 already_initialized`. |
| `POST /api/v1/auth/login` | — | Body `{email, password}`. Always a new session: `200` + profile. Unknown e-mail, wrong password and disabled account all get the same `401 invalid_credentials`. Limits: 5 failures per e-mail and 20 per client address per 15 minutes (`429`). |
| `POST /api/v1/auth/logout` | session | Revokes the session and clears the cookie: `204`. |

## Current user

| Route | Auth | Description |
| --- | --- | --- |
| `GET /api/v1/me` | session | Profile: `{user, organization, role, permissions, preferences: {theme, language}, csrf_token}`. |
| `PATCH /api/v1/me` | session | Body `{display_name?, preferences?: {theme?, language?}}` (`""` resets to default). Languages: `en`, `pt-BR`, `es`. |
| `POST /api/v1/me/password` | session | Body `{current_password, new_password}`. Ends every session of the user and continues this browser with a new one: `200` + profile. |

## Chat

Chat routes follow Mattermost's model (teams, channels of type `O` public, `P` private, `D` direct, `G` group, posts
with threads). Times are Unix milliseconds, as in Mattermost. Lists return `{"items": [...], "next_cursor"?}` and take
`per_page` (default 60, max 200) and `cursor`. Other users always appear as public profiles
`{id, username, display_name, active, last_picture_update}` — never their e-mail (`last_picture_update` versions the
profile picture, in ms; `0` without one). A resource the caller may not even discover is `404`;
one they may see but not use is `403`.

### Teams

| Route | Auth | Description |
| --- | --- | --- |
| `GET /api/v1/users/me/teams` | session | The caller's teams with `role`, `has_unread` and `mention_count`. |
| `GET /api/v1/teams` | session | Open teams the caller may join (none for guests). |
| `POST /api/v1/teams` | `create_team` | Body `{name?, display_name, description?, type?: open\|invite}`; creates `#general` too: `201`. `409 name_taken`. |
| `GET /api/v1/teams/:id` | session | A team the caller belongs to (or may join) with their `role`. |
| `PATCH /api/v1/teams/:id` | team admin | Body `{display_name?, description?, type?}`. |
| `POST /api/v1/teams/:id/archive` | team admin | `204`; the default team cannot be archived (`409 default_team`). |
| `POST /api/v1/teams/:id/restore` | team admin | `204`. |
| `GET /api/v1/teams/:id/members` | team member | Members with their team `role`. |
| `POST /api/v1/teams/:id/members` | session | Body `{user_id?}`: absent or the caller joins an open team; otherwise a team admin adds that user. `204`. |
| `DELETE /api/v1/teams/:id/members/:user` | team admin or self | Removes the user from the team and all its channels: `204`. |
| `PUT /api/v1/teams/:id/members/:user/role` | team admin | Body `{role: team_user\|team_admin}` (not one's own role). |

### Channels and conversations

| Route | Auth | Description |
| --- | --- | --- |
| `GET /api/v1/users/me/teams/:id/channels` | team member | The caller's channels of a team with unread `msg_count`/`mention_count` state. |
| `GET /api/v1/teams/:id/channels` | team member | Public channels the caller may join (none for guests), with `member_count`. |
| `POST /api/v1/channels` | team member, not guest | Body `{team_id, name?, display_name, purpose?, header?, type: O\|P}`: `201`. `409 name_taken`. |
| `GET /api/v1/channels/:id` | may discover | Channel with `member_count` and the caller's `role`. |
| `PATCH /api/v1/channels/:id` | channel admin | Body `{name?, display_name?, purpose? (≤ 250), header? (≤ 1 024)}`. `403 channel_archived`. |
| `POST /api/v1/channels/:id/archive` | channel admin | `204`; `409 default_channel` for `#general`. |
| `POST /api/v1/channels/:id/restore` | channel admin | `204`. |
| `GET /api/v1/channels/:id/members` | member | Members with their channel `role`. |
| `POST /api/v1/channels/:id/members` | session | Body `{user_id?}`: absent or the caller joins a public channel; otherwise a channel admin adds a member of the team (`400 not_team_member`, `400 direct_channel` for conversations). |
| `DELETE /api/v1/channels/:id/members/:user` | channel admin or self | `204`; nobody leaves `#general` (`409 default_channel`). |
| `PUT /api/v1/channels/:id/members/:user/role` | channel admin | Body `{role: channel_user\|channel_admin}`. |
| `POST /api/v1/channels/:id/view` | member | Marks the channel read (counts reset, read position only moves forward): `204`; the caller's other connections get `channel_viewed`. |
| `POST /api/v1/channels/direct` | session | Body `{user_id}`: the 1:1 conversation (created once): `200` + channel. `400 user_inactive`; guests need a shared team channel (`403`). |
| `POST /api/v1/channels/group` | session | Body `{user_ids}`: with the caller, 3 to 8 distinct people (`400 invalid_input`). |
| `GET /api/v1/users/me/conversations` | session | Visible direct and group conversations, most recent first, with `members`, `unread` and `mention_count`. |
| `PUT /api/v1/users/me/conversations/:id/visibility` | member | Body `{visible}`; a hidden conversation shows again with its next message. |
| `PUT /api/v1/users/me/channels/:id/favorite` | member | Body `{favorite}`: marks or unmarks a channel or conversation as the caller's favorite: `204`. Personal: only the caller's lists carry `favorite`, and only their connections get `preferences_changed`. `404` for non-members. |

### Posts and reactions

| Route | Auth | Description |
| --- | --- | --- |
| `GET /api/v1/channels/:id/posts` | member | History, newest first, paged by `cursor`. With `since_revision=<n>` instead: `{posts, next_revision, has_more}` — every change after revision `n` in revision order (tombstones and re-stamped thread roots included); page with `next_revision` until `has_more` is false. With `around=<post id>` instead: `{items, prev_cursor?, next_cursor?}` — that message with up to `per_page / 2` messages on each side, newest first; continue backwards with `cursor=<prev_cursor>` and forwards with `after=<next_cursor>` (`{items, next_cursor?}`, oldest first, no `next_cursor` once the newest message was returned). `404` when the message is not in that channel. |
| `POST /api/v1/posts` | member | Body `{channel_id, root_id?, message (1–16 383), pending_post_id (1–64 of A-Z a-z 0-9 : _ -)}`: `201` + post. A repeat with the same key returns the stored post (no new event); the same key for a different message is `409 pending_post_conflict`. 30 messages per 10 s per user (`429 rate_limited`). `403 channel_archived`; a missing or deleted root is `400 invalid_input`. |
| `PATCH /api/v1/posts/:id` | author | Body `{message}`: `200` + post with `edit_at`. The text may be empty only when the message has files. |
| `DELETE /api/v1/posts/:id` | author or channel admin | Soft delete: `204`. A root takes its replies with it; tombstones never carry text or reactions. Deleting someone else's post is audited. |
| `GET /api/v1/posts/:id/thread` | member | The root then its replies in order, paged forward by `cursor` (any post of the thread works as `:id`). |
| `POST /api/v1/posts/:id/pin` | member | Pins the message for every member of its channel (empty JSON body): `200` + post. Pinning a pinned message changes nothing and keeps the first pin's author and time. `403 channel_archived`; `404` for a deleted message or a channel the caller cannot read. |
| `POST /api/v1/posts/:id/unpin` | member | Clears the pin: `200` + post. |
| `GET /api/v1/channels/:id/pinned` | member | The channel's pinned messages, most recently pinned first, paged by `cursor`. Deleted messages are never pinned. |
| `POST /api/v1/posts/search` | session | Body `{terms, page?, per_page?}`: the messages of the caller's channels and conversations that contain every term (case-insensitive substring; `"quoted phrase"`, `from:<username>`, `in:<channel name>`, several of one modifier are alternatives), newest first: `{posts, has_more, channels: {<id>: {id, type, name, display_name, team_id}}}`; `page` from 0, `per_page` default 20, max 100. Never deleted messages or archived channels. `400 invalid_input` with a message naming the wrong part (no word of 2 or more characters, more than 10 words, over 256 characters, an unknown user, a channel the caller is not in); `429 rate_limited` above 10 searches in 10 s. On SQLite the case folding covers ASCII letters only. |
| `POST /api/v1/reactions` | member | Body `{post_id, emoji_name}` (gemoji short names such as `+1`, `rocket`; else `400 invalid_input`): `204`; adding an existing reaction changes nothing. |
| `DELETE /api/v1/users/me/posts/:id/reactions/:emoji` | member | Removes the caller's reaction: `204`. |

Posts are `{id, channel_id, user_id, root_id, message, type, pending_post_id, create_at, update_at, edit_at, delete_at,
reply_count, last_reply_at, revision, is_pinned, pinned_by?, pinned_at?, reactions: [{emoji_name, count, user_ids (≤ 25)}],
file_ids?, metadata?: {files: [file info]}}`
(`pinned_by` and `pinned_at` only while pinned; `file_ids` and `metadata.files` only with files, in the message's
order, never on a tombstone). `revision` orders every
change of a channel's posts: clients keep the highest seen and ignore older updates.

### Files

`POST /api/v1/posts` takes `file_ids`: up to `files.max_per_post` (default 10) files the author uploaded to that
channel and that no message carries yet; anything else is `400 invalid_input` and attaches nothing. With files the
`message` may be empty. A file info is `{id, user_id, channel_id, post_id?, name, extension, size, mime_type, create_at,
has_preview_image, width?, height?}` (Mattermost's `FileInfo`; `post_id` once attached, `width` and `height` for
images with a preview). The size is the image's as shown: a photo the camera stored turned is upright in its thumbnail
and preview.

| Route | Auth | Description |
| --- | --- | --- |
| `POST /api/v1/files` | member | `multipart/form-data` (not JSON; `Origin` and `X-CSRF-Token` as usual): the field `channel_id` first, then one or more files in the field `files`, each streamed to the storage: `201 {file_infos: [file info]}`. The media type is read from the bytes (never from the declared type) and the name is kept as metadata only (last path element, at most 255 characters). `404` for a channel the caller may not post in, `403 channel_archived`, `413 file_too_large` above `files.max_size_bytes` (default 50 MiB; nothing is stored), `400 too_many_files` above `files.max_per_post`, `400 invalid_input` for an empty file or a file before `channel_id`, `429 rate_limited` above 60 files per user per minute, `503 storage_unavailable`. A file never attached to a message is removed after 24 hours. Deviation from Mattermost: multipart only, no `client_ids`. |
| `GET /api/v1/files/:id` | channel reader | The file's bytes, always as a download (`Content-Disposition: attachment` with the original name, the sniffed `Content-Type`, `X-Content-Type-Options: nosniff`, `Content-Security-Policy: sandbox`), with range requests. Only to callers who may read the file's channel, or its uploader while it is not attached; `404` otherwise (also for a deleted message's file). No public links. |
| `GET /api/v1/files/:id/thumbnail` | channel reader | A JPEG at most 240 px high, for PNG, JPEG and GIF uploads of up to 24 megapixels (`has_preview_image`); `404` for other files. Re-encoded by the server without the original's metadata; shown inline, `Cache-Control: private`. |
| `GET /api/v1/files/:id/preview` | channel reader | The same, at most 1920 px wide. |
| `GET /api/v1/files/:id/info` | channel reader | The file info. |
| `GET /api/v1/channels/:id/files` | member | The files attached to the channel's messages, newest first, paged by `cursor`. |
| `GET /api/v1/users/me/files` | session | The files of every channel and conversation the caller belongs to, newest first, paged by `cursor`; `q` filters by name (case-insensitive substring). Deviation from Mattermost: a list with a name filter instead of the file search syntax. |

### Users and statuses

| Route | Auth | Description |
| --- | --- | --- |
| `GET /api/v1/users` | session | Directory of active users the caller may see (guests: people they share a channel or conversation with). `q` searches username and display name (prefix matches first, one page); `in_team` and `in_channel` restrict (`404` when the caller cannot read them). |
| `POST /api/v1/users/ids` | session | Body: a JSON array of up to 200 user ids. Public profiles of those the caller may see, disabled users included (as authors of existing messages); guests see people they share a channel with and the authors of messages in their channels. |
| `POST /api/v1/users/status/ids` | session | Body: a JSON array of up to 200 user ids. Returns `[{user_id, status, dnd_end_time?, last_activity_at?, stale?}]` for the users whose status the caller may see; others are omitted. `stale` is set while Redis is unavailable. |
| `POST /api/v1/users/me/image` | session | The caller's profile picture: `multipart/form-data` with the file in the field `image` (not JSON, like `POST /files`; `Origin` and `X-CSRF-Token` as usual). PNG, JPEG or GIF up to 5 MiB and 4096 px a side, stored as a 256 px square PNG cropped around the center, without metadata: `200 {last_picture_update}`. `400 invalid_image` (the format is read from the bytes, never from the name), `413 image_too_large`. |
| `GET /api/v1/users/:id/image` | session | The picture as `image/png` (`:id` may be `me`) with `ETag`, `Cache-Control: private` and `nosniff`; request it as `…/image?v=<last_picture_update>`. `404` for a user without a picture and for one the caller may not see, alike. |
| `DELETE /api/v1/users/me/image` | session | Removes the caller's picture: `204`. |
| `PUT /api/v1/users/me/status` | session | Body `{status: dnd, dnd_end_time?}` (ms; absent = until changed) or `{status: online\|away\|offline}` to follow activity again: `200` + status. |

## Meetings

Meetings use LiveKit (one persistent room per channel and conversation). Without LiveKit settings they are off: the
list answers `enabled: false` and the other routes `503 meetings_unavailable`.

| Route | Auth | Description |
| --- | --- | --- |
| `GET /api/v1/meetings` | session | `{enabled, rooms: [{channel_id, team_id, revision, participants: [{user_id, joined_at}]}]}`: the rooms with people in them among the channels and conversations the caller may read. |
| `GET /api/v1/channels/:id/meeting` | member | The channel's room: `{channel_id, team_id, revision, participants}` (empty when nobody is in it). |
| `POST /api/v1/channels/:id/meeting/token` | member | A LiveKit access token for the channel's room (empty JSON body): `200 {url, token, expires_at}`, `Cache-Control: no-store`. The token expires after 2 minutes, names the caller's user id as identity and grants joining only that room (publish, subscribe, data, own attributes; no administration). `403 channel_archived`; `403`/`404` as for reading the channel. |
| `POST /api/v1/meetings/webhook` | LiveKit signature | LiveKit's webhooks (`participant_joined`, `participant_left`, `participant_connection_aborted`, `room_finished`), up to 64 KiB, signed with the API key and secret (`Authorization` JWT carrying the body's SHA-256): `200`, `401 invalid_signature`. Not for browsers: no session, `Origin` or CSRF. |

Every 3 seconds the server replaces the room lists with LiveKit's own and removes from the rooms (in LiveKit too) the
people who may no longer read the channel.

## Real-time events

| Route | Auth | Notes |
| --- | --- | --- |
| `GET /api/v1/websocket` | session | WebSocket upgrade; `Origin` must equal the public URL's origin (else `403`). The first message is `hello` (`connection_id`, `server_version`); at most 10 connections per user (the oldest is closed); `503 unavailable` during shutdown. |

Server events are `{"event", "data", "broadcast", "seq"}` with `seq` growing by one per connection (a gap means an event
was lost: resynchronize through the API). Client actions are `{"action", "seq", "data"}`, answered with
`{"status": "OK"|"FAIL", "seq_reply", "data"?, "error"?}`: `ping` (→ `pong`), `user_activity`, `user_typing`
(`channel_id`, `parent_id`) and `get_statuses` (`user_ids`, at most 200); any other action fails with `unknown_action`
and the connection stays open. Frames over 64 KiB, more than 20 actions per second or a client that cannot keep up
(256 queued events or 1 MiB) close the connection; so does the end of its session (checked every 5 seconds).
`resync` asks the client to reload its state after events may have been missed.

### Event catalog

Every event has a payload fixture in `internal/chat/testdata/events/<event>.json` (kept valid by the test suite).
Clients ignore events about posts whose `revision` is older than the one they hold.

| Event | Recipients | Data |
| --- | --- | --- |
| `hello` | the new connection | `{connection_id, server_version}`; always `seq` 0. |
| `posted` | channel members | `{post, channel_type, channel_name, team_id, mentions: [user ids]}`. |
| `post_edited` | channel members | `{post}`: an edit, or a pin or unpin (`is_pinned` changes, `edit_at` does not). |
| `post_deleted` | channel members | `{post: tombstone, delete_thread?: true}` (one event for a whole thread). |
| `reaction_added` | channel members | `{reaction: {post_id, user_id, emoji_name, create_at}, channel_id, revision}`. |
| `reaction_removed` | channel members | Same as `reaction_added`. |
| `typing` | other channel members | `{user_id, parent_id}`; at most every 3 s per user and channel; show for 5 s. |
| `status_change` | users who may see the status | `{user_id, status}`. |
| `channel_viewed` | the user's connections | `{channel_id}`. |
| `channel_created` | creator; public: the team except guests | `{channel}`. |
| `channel_updated` | channel members | `{channel}`. |
| `channel_deleted` | channel members | `{channel_id, team_id}` (archived). |
| `channel_restored` | channel members | `{channel_id, team_id}`. |
| `channel_member_updated` | the user and channel admins | `{channel_id, user_id, role}`. |
| `direct_added` | a member who was not seeing the conversation | `{channel_id, teammate_id}`. |
| `group_added` | same, for group conversations | `{channel_id, teammate_id}`. |
| `user_added` | channel members | `{channel_id, team_id, user_id}`. |
| `user_removed` | remaining members and the removed user | `{channel_id, team_id, user_id}`; the removed user's queued events of that channel are dropped. |
| `added_to_team` | the user | `{team_id, user_id}`. |
| `leave_team` | the user | `{team_id, user_id}`. |
| `update_team` | team members | `{team}`. |
| `delete_team` | team members | `{team_id}` (archived). |
| `restore_team` | team members | `{team_id}`. |
| `user_role_updated` | the user and team admins | `{team_id, user_id, role}`. |
| `user_updated` | users who may see the user | `{user}`: the public profile, after a change of the profile picture (`last_picture_update`). |
| `preferences_changed` | the user's connections | `{preferences: [{user_id, category, name, value}]}`; `category` is `favorite_channel`, `name` the channel id, `value` `"true"` or `"false"`. |
| `meeting_updated` | channel members | `{channel_id, revision, participants: [{user_id, joined_at}]}`: the full list of the channel's meeting room after a join, a leave, the room ending or a correction; keep the highest `revision` per room and ignore older snapshots (fixture in `internal/meetings/testdata`). |
| `resync` | every connection of a replica | `{}`: events may have been missed; reload through the API. |

## Administration

| Route | Permission | Description |
| --- | --- | --- |
| `GET /api/v1/admin/users` | `manage_users` | Users with their role, ordered by name then id. Query: `role`, `status`, `q` (name/e-mail), `limit`, `cursor`. |
| `POST /api/v1/admin/users` | `manage_users` | Body `{email, display_name, role, password}`: `201` + user. Only owners may create owners. |
| `PATCH /api/v1/admin/users/:id` | `manage_users` | Body with only `display_name`, `role`, `status` (`active`/`disabled`). Disabling ends the user's sessions. Admins cannot change owners; the last active owner cannot be demoted or disabled (`409 last_owner`). |
| `DELETE /api/v1/admin/users/:id/image` | `manage_users` | Removes a user's profile picture (moderation): `204`; audited as `user.picture_removed`. Owners only for owner accounts. |
| `POST /api/v1/admin/users/:id/password` | `manage_users` | Body `{password}`: sets the password and ends all of the user's sessions: `204`. Owners only for owner accounts. |
| `GET /api/v1/admin/audit` | `read_audits` | Audit events, newest first by `(created_at, id)`. Query: `actor`, `action`, `from`, `to` (RFC 3339), `limit`, `cursor`. |

## Roles and permissions

| Permission | owner | admin | member | guest |
| --- | --- | --- | --- | --- |
| `manage_system` | ✓ | | | |
| `manage_owners` | ✓ | | | |
| `manage_users` | ✓ | ✓ | | |
| `read_audits` | ✓ | ✓ | | |
| `create_team` | ✓ | ✓ | | |
| `edit_own_profile` | ✓ | ✓ | ✓ | ✓ |
