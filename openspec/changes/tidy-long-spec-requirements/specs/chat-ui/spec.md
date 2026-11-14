# Spec Delta

## MODIFIED Requirements

### Requirement: Channel view
Opening a channel SHALL show its header (name, favorite star, purpose, the actions Search and Members with their
count, a "more" menu), the timeline (author, picture or initials, time, Markdown text, edited and pinned marks,
reactions with an "add reaction" button, reply count with last reply time, dated day separators, one author's
consecutive messages within 5 minutes grouped) and the composer, and SHALL mark the channel as read; older messages
SHALL load when scrolling up.

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
