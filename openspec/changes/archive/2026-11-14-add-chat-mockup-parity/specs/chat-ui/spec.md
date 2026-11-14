# Spec Delta

## MODIFIED Requirements

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

## ADDED Requirements

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
