# Notifications: on the desktop and by e-mail

## Why

MeetHub only tells people something happened while they are looking at it. A mention in another tab goes unnoticed,
and someone who closed the app learns about a direct message the next morning. Every chat people come from notifies
them; without it, MeetHub is the tool where messages are missed.

## What Changes

- **Desktop notifications**: with the person's permission (asked from Settings, never on page load), the browser
  shows a notification for a direct message or a mention when the app is not the visible tab or another
  conversation is open. Choosing it opens that message.
- **E-mail notifications**: when a direct message or a mention is still unread after 10 minutes and the person is
  not online, MeetHub sends an e-mail with who wrote, where, the start of the message and a link — at most one
  e-mail per person every 15 minutes, gathering what happened since the last one.
- **Preferences** per person in Settings: desktop (`all`, `mentions` — the default —, `none`) and e-mail
  (`mentions` — the default when e-mail is configured —, `none`).
- Every e-mail has a link that turns e-mail notifications off without logging in.
- `[smtp]` settings (host, port, user, the password from the environment, sender, TLS mode). Without them, e-mail
  notifications do not exist and Settings says so.
- Nothing is sent about a message the person can no longer read when the e-mail is written.

Not in this change: push notifications to closed browsers or phones (Web Push with a service worker), muting a
channel, notification sounds, digests of unread channels, e-mails for anything other than direct messages and
mentions.

## Capabilities

### New Capabilities

- `notifications`: preferences, desktop notifications, e-mail notifications, unsubscribing.

### Modified Capabilities

- `configuration`: the `smtp` settings.
- `background-jobs`: the e-mail task.
- `web-shell`: the Notifications section of Settings.

## Impact

- **Server**: a new `internal/notify` package (pending items, batching, the SMTP sender with the standard library
  and templates in three languages), a migration (preferences columns, `notification_queue`), one periodic task,
  the unsubscribe route.
- **Web app**: the permission request and the Notification API in the chat store, Settings → Notifications.
- **Privacy**: e-mails carry the start of messages by default; `notifications.email_excerpt = false` sends only who
  and where.
