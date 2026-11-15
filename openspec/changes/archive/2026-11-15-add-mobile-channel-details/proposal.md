# Channel details on phones and tablets

## Why

Below 1024 px the channel's details panel does not exist: the Members action and the "Channel details" menu item do
nothing visible, so on a phone or a tablet nobody can see who is in a channel, its meeting room card, its recent
files or its pinned messages. Threads, search and files already open full screen on small screens; the details are
the one side-panel state left behind (found in the review of `add-file-uploads`).

## What Changes

- Below 1024 px, asking for the channel's details (Members action, "Channel details" in the channel menu) opens
  them as a full-screen panel, like threads and search.
- The panel has a close button; Escape and the browser's Back close it; focus moves into it and returns to the
  control that opened it.
- Nothing changes at 1024 px and above.

## Capabilities

### New Capabilities

None.

### Modified Capabilities

- `chat-ui`: the channel details on small screens.

## Impact

- `frontend/src/layouts/AppLayout.vue` (the side panel's state and classes), `ChannelDetails.vue` (the close
  button), `style.css`; strings in three languages. No server change.
- Playwright at phone (390 px) and tablet (820 px) widths; accessibility checks.
