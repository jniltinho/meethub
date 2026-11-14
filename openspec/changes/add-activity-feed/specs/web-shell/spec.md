# Spec Delta

## MODIFIED Requirements

### Requirement: Placeholder sections
Conversations SHALL open the chat of the user's last team and channel, Teams SHALL list the user's teams and the
open teams they may join, Files SHALL list the files of the user's channels, Meetings SHALL list the meeting rooms
with people in them among the user's channels and conversations, and Activity SHALL list the mentions and thread
replies addressed to the user; no section of the navigation SHALL be an empty placeholder.

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

#### Scenario: Files route
- **WHEN** the user selects Files in the sidebar's section navigation
- **THEN** the URL is `/files` and the files of their channels are listed, or an empty state when there are none

#### Scenario: Activity route
- **WHEN** the user selects Activity in the sidebar's section navigation
- **THEN** the URL is `/activity` and their mentions and thread replies are listed, or an empty state when there
  are none
