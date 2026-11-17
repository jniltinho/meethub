# Design

## Context

`internal/presence` keeps, per user in Redis, the live connections, the last activity and an optional `dnd` end
time; one Lua function computes the status from them on every event and the sweeper publishes the changes.
Mattermost — the functional base — lets a user choose online, away, do not disturb or offline, a chosen status
overriding activity.

## Goals / Non-Goals

**Goals:** the four statuses of Mattermost, chosen from one menu, seen at once by everyone who can see the user.

**Non-Goals:** custom status text and emoji; a status chosen by an administrator for someone else; changing
what do not disturb does to notifications.

## Decisions

### D1. A chosen status is one more field of the state

The state hash gains `manual` (`away` or `offline`). The computation becomes: do not disturb if set and not
ended; else offline without live connections; else the chosen status; else by activity. Choosing Online clears
both `dnd` and `manual`; choosing Away or Appear offline clears `dnd`; choosing do not disturb clears `manual`,
so when it ends the status follows activity again. Everything else — the sweeper, the publication, the outage
behavior — is unchanged, since they all go through the same function.

A user who appears offline is not "online" for notification e-mails either: they are told by e-mail what they
missed, as someone offline is.

### D2. The route

`PUT /api/v1/users/me/status` keeps its body. `away` and `offline` are now stored. The answer, and the status
lookup, gain `manual: true` when the status was chosen (do not disturb included), so the menu marks the right
entry and "Online" reads as "follows your activity".

### D3. The menu

`StatusMenu.vue` replaces the picture and name at the sidebar's foot with one button (picture, presence dot,
name, status) that opens a popover of the house style (`.select-pop`): a radio group of the four statuses, the
durations of do not disturb as a second group shown under it. Escape and a click outside close it and return the
focus; the choice is announced. The user's own status comes from the same store as everyone's, updated by the
answer of the route and by `status_change`.

## Risks / Trade-offs

- "Appear offline" hides that someone is reading → accepted, it is the point; messages still arrive.
- A chosen Away is forgotten by nobody: it stays until the user changes it → the foot always shows it.

## Review (Codex, 2026-11-17) and what changed

- **Appearing offline hides the activity behind it.** Someone else is told neither that a status was chosen
  (`manual` is the user's own to see) nor, for a chosen offline, when the user was last active (`Status.Public`).
- **A choice is always announced**, even when the status word stays the same (a longer do not disturb, an away
  that is now chosen): the user's other tabs read the details again on every status event about themself.
- **A lookup that started earlier never puts an older status back** over a later choice.
- **The popover is a menu** (`menu`, `menuitemradio`): one tab stop, the arrow keys, Home and End move among its
  entries; the dots inside it are decorative, the entry's words say the status.

