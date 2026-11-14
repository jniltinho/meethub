# Design

## Context

The chat store already knows, for every `posted` event, whether it mentions the person and whether the channel is
open and the tab visible. The server knows presence (`internal/presence`), unread state per member
(`last_viewed_at`, `mention_count`) and runs periodic tasks once across replicas. There is no mail code yet.

## Goals / Non-Goals

**Goals:** nobody misses a message addressed to them; nobody is flooded. **Non-Goals:** push to closed browsers,
per-channel rules.

## Decisions

### D1. Desktop notifications are the browser's, decided in the app

On `posted` for a direct message, or a message that mentions the person, the app shows a `Notification` when the
preference allows, permission is granted and either the document is hidden or another conversation is open. The
tag is the channel id (a newer one replaces the older). No server work.

### D2. E-mail from a queue, sent by a periodic task

Storing a direct message or a direct mention inserts a row in `notification_queue` (`user_id`, `post_id`,
`create_at`) for each addressee whose e-mail preference allows it. Every minute one replica takes, per person, the
rows older than 10 minutes when: the person is not online, the last e-mail to them was sent more than 15 minutes
ago, and the message is still unread and readable by them. It sends one e-mail with those messages (at most 20, the
rest counted) and deletes the rows; rows of messages already read are deleted without sending.

### D3. SMTP with the standard library

`net/smtp` with STARTTLS or implicit TLS, PLAIN or LOGIN authentication, a 30-second timeout, multipart
text + HTML built from embedded templates in the person's language. No dependency. A send failure leaves the rows
for the next run and is logged without the message's content.

### D4. Unsubscribe without login

Each person has a random unsubscribe token stored as a hash; `GET /api/v1/notifications/unsubscribe?token=…` sets
their e-mail preference to `none` and answers a small themed page. The token changes when the preference is turned
back on.

## Risks / Trade-offs

- [Message text in mailboxes] → on by default because a notification without content sends people to the app for
  nothing; `notifications.email_excerpt = false` for organizations that cannot accept it.
- [E-mails marked as spam] → one every 15 minutes at most, a List-Unsubscribe header, the organization's own SMTP.
- [Presence wrong during a Redis outage] → the task skips the run rather than send to people who may be online.

## Migration Plan

Additive migration; defaults give desktop `mentions` and, with SMTP configured, e-mail `mentions`. Rollback drops
the queue and the columns.
