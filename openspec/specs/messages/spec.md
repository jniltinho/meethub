# messages Specification

## Purpose
Defines messages (Mattermost posts) in channels and conversations: posting, threads, editing, deleting, idempotency,
the per-channel revision that orders every change, history and catch-up, and how message text is rendered.

## Requirements

### Requirement: Posting messages
Members of a channel or conversation SHALL post messages of 1 to 16 383 characters (after trimming) through
`POST /api/v1/posts`; membership, archive state (channel and team) and the thread root SHALL be checked under the same
per-channel serialization as membership changes and archiving; the message SHALL be stored before any event is sent.

#### Scenario: Message stored, then delivered
- **WHEN** Ana posts "Deploy at 17h" in `#projects`
- **THEN** the response is 201 with the stored message and every connected member receives `posted` with the same id

#### Scenario: Not a member
- **WHEN** a user posts to a public channel they have not joined
- **THEN** the response is 403 with code `forbidden`

#### Scenario: Removal racing a post
- **WHEN** Diego's removal from a channel and his message there are committed concurrently
- **THEN** the message is stored only if it committed before the removal

### Requirement: Channel revision
Every change to a channel's messages (post, edit, delete, reaction, reply count) SHALL take the next value of that
channel's revision, assigned in commit order, and the changed messages SHALL carry it; events SHALL include it so
clients ignore stale updates.

#### Scenario: Two edits in the same millisecond
- **WHEN** a message is edited twice within one millisecond
- **THEN** the two edits have different revisions and every client ends with the second text

### Requirement: Idempotent posting
Every post SHALL carry a client-generated `pending_post_id` (1–64 characters of letters, digits, `:`, `_` and `-`);
a repeat with the same author and key SHALL return the stored message (after re-checking read access) without a new
event; a repeat with the same key but a different channel, root or text SHALL get 409 `pending_post_conflict`.

#### Scenario: Retry after a lost response
- **WHEN** the client sends a post again with the same `pending_post_id` and content
- **THEN** exactly one message exists and both responses carry its id

#### Scenario: Concurrent retries
- **WHEN** two identical posts with the same key arrive at the same time on different replicas
- **THEN** one message is stored and both requests return it

#### Scenario: Reused key, different text
- **WHEN** a post reuses a key with a different text
- **THEN** the response is 409 with code `pending_post_conflict`

### Requirement: Threads
A message MAY reply to a root message of the same channel (`root_id`); replies to replies SHALL attach to the root; a
reply SHALL update the root's reply count, last reply time and revision; deleting a reply SHALL decrement the count and
recompute the last reply time; `GET /api/v1/posts/{id}/thread` SHALL return the root and its replies in order with
cursor pagination (at most 200 per page).

#### Scenario: Reply counted
- **GIVEN** a root message with 2 replies
- **WHEN** someone replies again
- **THEN** the root's reply count is 3, its last reply time is the new reply's time and its revision advanced

#### Scenario: Reply to a deleted root
- **WHEN** a reply names a root deleted concurrently
- **THEN** the reply is stored only if it committed before the deletion; otherwise the response is 400
  `invalid_input`

### Requirement: Editing
Authors SHALL edit their own messages in unarchived channels; the edit time SHALL be recorded and shown; others SHALL
NOT edit them.

#### Scenario: Editing someone else's message
- **WHEN** Bruno edits a message of Ana
- **THEN** the response is 403 with code `forbidden`

### Requirement: Deleting
Authors SHALL delete their own messages, and channel administrators SHALL delete any message of their channels;
deletion SHALL be soft: the API and events SHALL return a tombstone (id, author, times, `delete_at`) without text,
reactions or attachments, the text being kept only in storage for retention; deleting a root SHALL delete its replies
and send one `post_deleted` for the root with `delete_thread: true`.

#### Scenario: Thread deleted with its root
- **WHEN** the author deletes a root message with replies
- **THEN** the root and its replies become tombstones and members receive one `post_deleted` with `delete_thread`

#### Scenario: Deleted text never leaks
- **WHEN** a deleted message is loaded through history, catch-up or a thread
- **THEN** its text is absent

### Requirement: History and catch-up
`GET /api/v1/channels/{id}/posts` SHALL page backwards by an opaque cursor over (creation time, id) with `per_page`
(default 60, max 200); `GET …/posts?since_revision=<n>` SHALL return, ordered by revision and paginated with
`next_revision` and `has_more`, every message whose revision is greater than `n`, including tombstones and roots whose
reply counts changed.

#### Scenario: Catch up after reconnecting
- **GIVEN** a client offline while 3 messages were posted and 1 edited
- **WHEN** it requests changes since its last revision
- **THEN** it receives those 4 changes once, in revision order

#### Scenario: More than a page of changes
- **GIVEN** 450 changes since the client's revision
- **WHEN** it pages with `next_revision` until `has_more` is false
- **THEN** it receives all 450 exactly once, even while new messages are being posted

#### Scenario: A thousand messages in order
- **WHEN** 1 000 messages are posted to one channel by several clients
- **THEN** history returns all 1 000 exactly once in creation order

### Requirement: Message rate limit
Each user SHALL post at most 30 messages in any 10-second interval (a sliding window shared through Redis; rejected
posts and idempotent repeats do not count); beyond it the response SHALL be 429 with `Retry-After`; during a Redis
outage the same limit SHALL apply per replica.

#### Scenario: Flood
- **WHEN** a user posts 31 messages within 10 seconds across two replicas
- **THEN** the 31st gets 429 with code `rate_limited`

### Requirement: Safe rendering
Message text, channel headers and purposes SHALL be rendered in the browser as Markdown with raw HTML disabled,
Markdown images shown as links (no image is loaded), links only for `http`, `https` and `mailto` with
`target="_blank" rel="noopener noreferrer"`, and the final HTML (after mentions, channel links and emoji are applied)
SHALL pass an allowlist sanitizer with nothing added afterwards.

#### Scenario: Script in a message
- **WHEN** a message contains `<img src=x onerror=alert(1)>`, `[x](javascript:alert(1))`, `[x](JaVaScRiPt:…)`, an
  entity-encoded scheme or `![t](https://evil.example/pixel)`
- **THEN** no script runs, no request goes to `evil.example` and no CSP violation is reported

#### Scenario: Malicious display name in a mention
- **GIVEN** a user whose display name is `<img src=x onerror=alert(1)>`
- **WHEN** a message mentions them
- **THEN** the name is shown as text
