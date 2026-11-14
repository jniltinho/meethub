# chat-ui Specification

## Purpose
Defines the conversation screens of the web app, following the mockup: team switcher, sidebar of channels and
conversations, message timeline, composer, thread panel, reactions and presence — themed, translated and accessible.

## Requirements

### Requirement: Team switcher and sidebar
The sidebar SHALL show the current team in a switcher (with unread and mention badges for the other teams), then the
user's favorite channels and conversations, the team's channels (public and private, unread in bold with mention
counts) and the user's direct and group conversations with presence, each in a section that collapses and stays as the
user left it in that browser; it SHALL offer creating and browsing channels and starting a direct or group message.

#### Scenario: Unread channel
- **GIVEN** `#devops` has 4 unread messages, one mentioning the user
- **WHEN** the sidebar renders
- **THEN** `#devops` is bold with a badge "1", and the team in the switcher shows a mention badge

#### Scenario: Favorite first
- **GIVEN** the user marked `#projects` as favorite
- **WHEN** the sidebar renders
- **THEN** `#projects` is listed under Favorites and no longer under Channels

#### Scenario: Collapsed section
- **GIVEN** the user collapsed Direct messages
- **WHEN** they reload the page
- **THEN** the section is still collapsed, and still shows a conversation with unread messages

### Requirement: Routes and fallbacks
Channels SHALL open at `/teams/:team/channels/:channel`, conversations at `/messages/@:username` and
`/messages/:conversationId` (not tied to a team); Conversations SHALL reopen the last visited channel or conversation
that is still readable, else the default channel of the user's first team; a user without teams SHALL see their
conversations and an empty state offering to browse teams.

#### Scenario: Deep link
- **WHEN** a user opens `/teams/engineering/channels/devops`
- **THEN** that channel is shown and its URL survives a reload

#### Scenario: Remembered channel no longer readable
- **GIVEN** the user's last channel was archived team `ops`'s `#finance` and they left `ops`
- **WHEN** they open Conversations
- **THEN** the default channel of their first team opens

#### Scenario: Not a member of a private channel
- **WHEN** a user opens the URL of a private channel they do not belong to
- **THEN** a themed "channel not found" state is shown

### Requirement: Channel view
Opening a channel SHALL show its header (name, favorite star, purpose, and the actions Search, Members with their
count and a "more" menu), the timeline (author, picture or initials, time, Markdown text, edited mark, pinned mark,
reactions followed by an "add reaction" button, reply count with last reply time, day separators carrying the date,
consecutive messages of one author within 5 minutes grouped as in Mattermost) and the composer, and SHALL mark the
channel as read; older messages SHALL load when scrolling up.

#### Scenario: Grouped messages
- **WHEN** Ana posts three messages within two minutes
- **THEN** only the first shows her name and picture

#### Scenario: Dated separator
- **GIVEN** today is January 21
- **WHEN** a channel with messages from yesterday and today opens in English
- **THEN** the separators read "Yesterday, Jan 20" and "Today, Jan 21"

#### Scenario: Star
- **WHEN** the user presses the star in the header of `#projects`
- **THEN** the star is filled and `#projects` moves to Favorites in the sidebar

### Requirement: Composer
The composer SHALL send with Enter and add a line with Shift+Enter, show a message at once as pending and replace it
with the stored one (same `pending_post_id`), offer a formatting toolbar (bold, italic, code, link, bulleted list,
numbered list), an emoji picker, a mention button and `@username` autocomplete, send typing notifications, and keep a
failed message in the timeline marked as failed with Retry (same key).

#### Scenario: Send offline
- **GIVEN** the connection is lost
- **WHEN** the user sends a message
- **THEN** it is shown as failed with Retry, and retrying after reconnecting stores it exactly once

#### Scenario: Numbered list
- **GIVEN** two lines are selected in the composer
- **WHEN** the user presses the numbered list button
- **THEN** the lines start with "1. " and "2. " and the sent message renders as a numbered list

### Requirement: Threads in the side panel
Reply and reply counts SHALL open the thread in the 320 px side panel (full screen on phones) with the root, its
replies and its own composer, updated live.

#### Scenario: Live reply
- **GIVEN** Ana has a thread open
- **WHEN** Bruno replies in it
- **THEN** the reply appears in Ana's panel and the root's reply count grows in her timeline

### Requirement: Message actions
Each message SHALL offer, according to permissions, reply, add reaction, edit (author, in place, Escape cancels) and
delete (author or channel administrators, after a themed confirmation).

#### Scenario: Delete needs confirmation
- **WHEN** the author chooses Delete
- **THEN** a themed confirm dialog asks first and the message becomes a tombstone for everyone only after OK

### Requirement: Live updates and recovery
The chat SHALL apply WebSocket events live, ignore updates older than the revision it holds, show a themed
"reconnecting" state while offline, and resynchronize through the API after `resync`, a `seq` gap, a reconnection and
every 60 seconds, without duplicates; logging out or a session ending SHALL close the socket and clear all chat state.

#### Scenario: Two browsers
- **GIVEN** Ana and Bruno have `#projects` open in two browsers
- **WHEN** Bruno sends a message, edits it, adds a reaction and deletes it
- **THEN** Ana sees each change within a second

#### Scenario: Another user logs in on the same browser
- **WHEN** Ana logs out and Bruno logs in on the same tab
- **THEN** none of Ana's channels, messages or unread counts remain visible

### Requirement: Themed, translated and accessible
The chat SHALL use only theme roles (square corners; all 22 themes pass the contrast recipes for mentions, unread badges
and presence), translate every string into Portuguese and Spanish, announce new messages of the open channel politely to
screen readers, and support Alt+Up/Down to change channel and Up to edit the last own message.

#### Scenario: Keyboard navigation
- **WHEN** a keyboard user presses Alt+Down in a channel
- **THEN** the next channel of the sidebar opens with focus in its composer

### Requirement: Team rail
The rail SHALL show one tile per team of the user with the initials of its display name, mark the current team, show
on each other team a dot for unread messages or a count for mentions, and open a team at the channel last visited in
it, else its default channel.

#### Scenario: Mention in another team
- **GIVEN** the user is in the team Technology and is mentioned in a channel of Product
- **WHEN** the event arrives
- **THEN** the Product tile shows "1" within a second, without reloading

#### Scenario: Switching teams
- **GIVEN** the user last read `#roadmap` in Product
- **WHEN** they press the Product tile
- **THEN** `#roadmap` opens with focus in its composer

### Requirement: Channel details panel
While no thread or search is open, the side panel SHALL show the open channel's details: its purpose and header,
its participants with picture, presence and channel role, online first, and its pinned messages; the header's Members
action SHALL open the panel at the participants.

#### Scenario: Members button
- **GIVEN** the side panel is closed
- **WHEN** the user presses "Members (12)" in the header
- **THEN** the panel opens showing 12 participants, those online first

#### Scenario: Thread takes the panel
- **GIVEN** the details are showing
- **WHEN** the user opens a thread and later closes it
- **THEN** the thread replaces the details and closing it brings them back

### Requirement: Pinning in the channel
Message actions SHALL include Pin and Unpin for members who may post; a pinned message SHALL carry a pinned mark, and
the channel SHALL show above the timeline a banner with the most recently pinned message, who pinned it and a way to
dismiss it in that browser until another message is pinned.

#### Scenario: Banner
- **WHEN** Ana pins the deploy checklist in `#projects`
- **THEN** every member with the channel open sees a banner "Pinned by Ana Silva" with the start of the message

#### Scenario: Dismissed banner
- **GIVEN** Bruno dismissed the banner
- **WHEN** he reloads the page
- **THEN** the banner stays hidden until a newer message is pinned

### Requirement: Search in the side panel
The header's Search SHALL open a search field in the side panel and list the results with channel, author, picture,
time and the text with the matched terms highlighted, newest first, loading more on scroll; it SHALL show themed states
for no results, an invalid query and too many searches.

#### Scenario: Results
- **WHEN** the user searches `deploy`
- **THEN** the panel lists the matching messages of their channels, each naming its channel, with "deploy" highlighted

#### Scenario: Nothing found
- **WHEN** a search matches no message
- **THEN** the panel shows an empty state that repeats the query and suggests the `from:` and `in:` filters

### Requirement: Jumping to a message
Choosing a search result, a pinned message or the pinned banner SHALL open the message's channel at that message,
loaded with the messages around it, highlighted for a moment and announced to screen readers; scrolling in either
direction SHALL load the rest of the history, and a "Jump to recent" button SHALL return to the newest messages.

#### Scenario: Old message
- **GIVEN** a search result from three months ago in a channel with thousands of newer messages
- **WHEN** the user chooses it
- **THEN** the channel opens with that message visible and highlighted, and scrolling down loads the newer ones

#### Scenario: Back to the present
- **GIVEN** the user jumped to an old message
- **WHEN** they press "Jump to recent"
- **THEN** the timeline shows the newest messages and new ones arrive live again

### Requirement: Channel menu
The header's "more" menu SHALL offer Channel details and Leave channel (not in the default channel) to every member,
and Edit channel and Archive channel to channel administrators, the destructive ones after a themed confirmation.

#### Scenario: Member
- **WHEN** a member without the administrator role opens the menu of `#projects`
- **THEN** it has Channel details and Leave channel and neither Edit nor Archive

#### Scenario: Archive needs confirmation
- **WHEN** a channel administrator chooses Archive channel
- **THEN** a themed confirm dialog asks first and the channel is archived only after OK

### Requirement: Profile picture in Settings
Settings → Profile SHALL let the user choose a picture, preview it, save it and remove it, with themed messages for a
file that is not an image or is too large; everywhere a person is shown the picture SHALL replace the initials, which
remain the fallback.

#### Scenario: New picture
- **WHEN** the user picks a JPEG in Settings and saves
- **THEN** their picture shows in the sidebar's foot and beside their messages at once

#### Scenario: Not an image
- **WHEN** the user picks a PDF
- **THEN** a themed error says the file is not a supported image and nothing is uploaded

### Requirement: Attaching files in the composer
The composer SHALL attach files through an attach button, by dropping them on the conversation and by pasting
images, show each with its name, size, upload progress and a way to remove it, keep a failed one with Retry, and
send the message once every upload has finished; it SHALL refuse, with a themed message, a file above the size
limit or beyond the number a message may carry.

#### Scenario: Drop a file
- **WHEN** the user drops `plan.xlsx` on the conversation and presses Enter
- **THEN** the message appears with the file and the other members see it within a second

#### Scenario: Too many files
- **WHEN** the user selects 12 files at once
- **THEN** 10 are attached and a themed message says a message carries at most 10 files

#### Scenario: Paste a screenshot
- **WHEN** the user pastes an image from the clipboard into the composer
- **THEN** it is attached with a preview before sending

### Requirement: Sending while files upload
A message submitted while its files upload SHALL leave with the text and the files it had when it was submitted,
whatever is typed or attached meanwhile; when one of those uploads fails nothing SHALL be sent and the text SHALL
return to the composer. Dragging text or links over the conversation SHALL keep its usual behavior.

#### Scenario: Typing while the upload finishes
- **GIVEN** the user pressed Enter on "Plan attached" while `plan.xlsx` was uploading
- **WHEN** they type another draft before the upload finishes
- **THEN** the message sent is "Plan attached" with the file, and the new draft stays in the composer

#### Scenario: Upload fails after Enter
- **GIVEN** the user pressed Enter while a file was uploading
- **WHEN** that upload fails
- **THEN** no message is sent, the file shows Retry and the text is back in the composer

### Requirement: Files inside messages
A message SHALL show its images as thumbnails that open enlarged (with a way to download the original, closed by
Escape, focus returning to the thumbnail) and its other files as cards with a type icon, name, size and a download
action; a tombstone SHALL show none.

#### Scenario: Image opens enlarged
- **WHEN** the user activates a thumbnail
- **THEN** the preview opens in a themed dialog and Escape closes it

#### Scenario: Download a document
- **WHEN** the user activates the download action of `plan.xlsx`
- **THEN** the browser downloads the file under that name

### Requirement: Channel files
The channel header SHALL have a Files action that lists the channel's files in the side panel, newest first, with
uploader and date, each opening its message in the channel; the channel details SHALL show the five most recent
files. The lists SHALL follow the channel: a new message's files appear and a deleted message's files leave
without reloading.

#### Scenario: Files panel
- **WHEN** the user presses Files in the header of `#projects`
- **THEN** the side panel lists the channel's files, and choosing one opens the channel at its message

### Requirement: Files page
The Files section SHALL list the files of the user's channels and conversations with name, channel, uploader, date
and size, filter them by name as the user types, load more on scroll, and offer download and "go to message" for
each.

#### Scenario: Find a file
- **WHEN** the user opens Files and types `plan`
- **THEN** `plan.xlsx` is listed with its channel, and "go to message" opens that channel at the message

### Requirement: Start meeting in the channel header
When meetings are enabled, the channel header SHALL show, left of the "more" menu, a primary button that reads "Start
meeting" when nobody is in the channel's room and "Join meeting (n)" when n people are, opening the room's pre-join
screen. It SHALL be absent in archived channels and when meetings are disabled.

#### Scenario: Nobody in the room
- **GIVEN** nobody is in the room of `#projects`
- **WHEN** Ana opens `#projects`
- **THEN** the header shows "Start meeting" left of the "more" menu

#### Scenario: People in the room
- **GIVEN** Bruno and Carla are in the room of `#projects`
- **WHEN** Ana opens `#projects`
- **THEN** the button reads "Join meeting (2)" and updates live when someone joins or leaves

#### Scenario: Meetings disabled
- **GIVEN** the server has no LiveKit settings
- **WHEN** Ana opens `#projects`
- **THEN** no meeting button is shown

### Requirement: Room card in the details panel
When meetings are enabled, the channel details panel SHALL start with a room card ("Room of #channel", "Available now"
or "n in the room" with their pictures, and "Join room"), and each participant in the list who is in the room SHALL
be marked "In the room".

#### Scenario: Room card with people
- **GIVEN** Bruno is in the room of `#projects`
- **WHEN** Ana opens the details of `#projects`
- **THEN** the room card reads "1 in the room" with Bruno's picture, and Bruno's row in the participants is marked
  "In the room"

### Requirement: Meeting rooms in the sidebar
When meetings are enabled and at least one room among the current team's channels and the user's conversations has
people in it, the chat sidebar SHALL show a "Meeting rooms" section listing those rooms with the number of people in
each; selecting one SHALL open the room's pre-join screen.

#### Scenario: Active room listed
- **GIVEN** 4 people are in the room of `#sync`
- **WHEN** Ana looks at the sidebar of the team
- **THEN** the "Meeting rooms" section lists "sync" with "4 in the room"

#### Scenario: No active room
- **GIVEN** nobody is in any room of the team
- **WHEN** Ana looks at the sidebar
- **THEN** no "Meeting rooms" section is shown
