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

### D5. Decided while implementing

- **Translations of the e-mails live in the frontend catalogs** (AGENTS.md: translations exist only there). The
  e-mail texts are listed in `frontend/src/i18n/email.ts` (the catalog test requires their translations), the build
  writes them to `i18n/email.json` in the embedded SPA, and `internal/notify` reads it; a Go test checks that every
  text the server uses is in that list. A binary built without the SPA sends English.
- **Unsubscribing is a POST** (D4 said `GET …?token=` answering a page): mail security scanners open links, and a GET
  that changes a preference would turn e-mails off for people who never asked. `POST
  /api/v1/notifications/unsubscribe?token=…` needs no session (one-click from mail clients, RFC 8058, through
  `List-Unsubscribe-Post`); the link in the e-mail opens the SPA page `/unsubscribe`, themed and translated, which asks
  before posting.
- **One token per e-mail**, stored as its SHA-256 and kept 90 days, instead of one per person: an earlier e-mail's
  link keeps working; every token of a person ends when they turn e-mails on again (as D4).
- **The link to a message** is the SPA route `/pl/:post`, which opens the channel at the message or its thread.
- **Who is "not online"**: presence `online` only; `away`, `dnd` and `offline` get e-mails. Presence that is stale
  (Redis down) skips the run.
- **Group conversations count as direct messages**; in channels only mentions by name are e-mailed (not `@channel` or
  `@here`); desktop notifications use the mentions of the `posted` event, which include `@channel` and `@here`.
- **One e-mail per 15 minutes** is enforced by claiming the person's slot in `notification_state` before sending
  (two overlapping runs cannot both send) and giving it back on a failed send.
- The e-mail lists who, where, the start of the message and the link, without the time (the person's time zone is
  unknown to the server).
- Migration `00008_notifications` (the next free number).
- After the Codex review: the messages are read again once the person's slot is claimed (a removal or deletion while
  the run waited is seen); the slot starts when the e-mail went out, not when the run began; an e-mail accepted at the
  end of DATA counts as sent even if QUIT fails (no second copy); errors name the SMTP step and reply code, never the
  reply's text (it may quote the address); tokens older than 90 days are refused at lookup; `List-Unsubscribe` holds
  one HTTPS URI (RFC 8058); queue deletions go 500 at a time.

## Risks / Trade-offs

- [Message text in mailboxes] → on by default because a notification without content sends people to the app for
  nothing; `notifications.email_excerpt = false` for organizations that cannot accept it.
- [E-mails marked as spam] → one every 15 minutes at most, a List-Unsubscribe header, the organization's own SMTP.
- [Presence wrong during a Redis outage] → the task skips the run rather than send to people who may be online.

## Migration Plan

Additive migration; defaults give desktop `mentions` and, with SMTP configured, e-mail `mentions`. Rollback drops
the queue and the columns.
