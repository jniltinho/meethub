# Spec Delta

## MODIFIED Requirements

### Requirement: Placeholder sections
Files and Activity SHALL have routes rendering an empty state that names the section, so later changes replace
content without changing navigation; Conversations SHALL open the chat of the user's last team and channel, Teams
SHALL list the user's teams and the open teams they may join, and Meetings SHALL list the meeting rooms with people in
them among the user's channels and conversations.

#### Scenario: Meetings route
- **WHEN** the user selects Meetings in the sidebar's section navigation
- **THEN** the URL is `/meetings` and the page titled "Meetings" lists the rooms with people in them, each with its
  channel, the people's pictures and a "Join" button, or says that no meeting is in progress

#### Scenario: Conversations route
- **GIVEN** the user last viewed `#projects` in the team `engineering`
- **WHEN** they select Conversations in the sidebar's section navigation
- **THEN** the chat of `#projects` in `engineering` opens

#### Scenario: Teams page
- **WHEN** the user presses the rail's button to browse or create teams
- **THEN** the URL is `/teams` and their teams and the open teams are listed
