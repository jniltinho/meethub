# Activity: mentions and thread replies in one place

## Why

Activity is the last empty section of the navigation. People who return after a few hours have unread badges on
channels but no single place that answers "what was said to me?": the mentions of them and the replies in the
threads they take part in. Slack, Teams and Mattermost all have it, and it is what the bell-like entry in the
navigation promises.

## What Changes

- The server records an activity item for a person when a message mentions them by name, and when someone replies
  in a thread they wrote in or started.
- `GET /api/v1/users/me/activity` lists those items, newest first, with the message and its channel; items of
  channels the person can no longer read, and of deleted messages, are not listed.
- An unseen count shows on the Activity entry of the navigation, live; opening Activity marks everything seen.
- The Activity page lists each item with who, where, the start of the message and when; choosing one opens the
  channel at that message (the thread, for a reply).
- Items older than 90 days are removed by a daily task.

Not in this change: `@channel` and `@here` (they already raise the channel's mention badge, and one row per member
of a large channel per message is not worth it), reactions to my messages, direct messages (they have their own
unread badges), notifications outside the app (`add-notifications`).

## Capabilities

### New Capabilities

- `activity`: what creates an activity item, the list, the unseen count, retention.

### Modified Capabilities

- `web-shell`: Activity is no longer a placeholder.
- `chat-ui`: the Activity page and the navigation badge.
- `realtime-events`: the event that keeps the unseen count live.
- `background-jobs`: the retention task.

## Impact

- **Server**: `internal/chat` writes `activity_items` in the posting transaction (new migration); list, count and
  "seen" routes; one event; one periodic task.
- **Web app**: `ActivityView.vue` replaces the placeholder route; a badge on the navigation entry (rail, sidebar,
  bottom bar); strings in three languages.
- **Data**: one row per person addressed per message, bounded by the direct mentions of a message and the
  participants of a thread.
