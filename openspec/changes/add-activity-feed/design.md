# Design

## Context

Mentions are parsed when a message is stored (`internal/chat/mentions.go`) and only counted on the member's row
(`mention_count`); which message mentioned whom is not kept. Thread replies bump the root's counters. Events carry
the list of mentioned user ids in `posted`.

## Goals / Non-Goals

**Goals:** a list that is cheap to read, exact about access, and live. **Non-Goals:** a notification system, an
inbox with states per item, anything about `@channel`.

## Decisions

### D1. Materialize items at write time

`activity_items` (`user_id`, `post_id`, `channel_id`, `kind`: `mention` or `reply`, `create_at`), primary key
(`user_id`, `post_id`), written inside the posting transaction: one row per directly mentioned member, and one per
distinct earlier author of the thread (the root's author included) for a reply, never for the message's own author.
A person both mentioned and in the thread gets one row, `mention`. Editing a message does not create items;
deleting it removes them.

*Alternative considered:* computing the list from posts at read time — it needs the mentions re-parsed per row or a
mentions table anyway, and the thread-participation join is expensive on every page.

### D2. Access is checked when reading

The list joins `channel_members` on the reader: an item of a channel they left is not listed (the row stays until
retention). Deleted messages have no items.

### D3. Unseen count

`users.activity_seen_at`; unseen = items newer than it that the reader can still read. `POST
/api/v1/users/me/activity/seen` sets it to now. The server sends `activity_updated {unseen}` to the person's
connections when an item is created for them and when they mark seen (so other tabs clear).

## Risks / Trade-offs

- [A thread with hundreds of participants] → one row each per reply; bounded by the thread's distinct authors and
  acceptable at the scale budget; the retention task keeps the table small.
- [Items of channels someone left linger] → never listed; removed at 90 days.

## Migration Plan

Additive migration; no backfill (Activity starts empty). Rollback drops the table and the column.
