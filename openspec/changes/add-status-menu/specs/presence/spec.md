# Delta for presence

## MODIFIED Requirements

### Requirement: Status values
Each user SHALL have a status `online`, `away`, `offline` or `dnd`. Without a choice, `online`, `away` and
`offline` SHALL follow activity automatically. The user MAY choose (`PUT /api/v1/users/me/status`) `away` or
`offline`, kept while they are connected until they choose another status, or `dnd`, until a chosen time or until
they change it; choosing `online` SHALL return to following activity.

#### Scenario: Do not disturb kept while active
- **GIVEN** Bruno set `dnd` until 18:00
- **WHEN** he keeps using the app
- **THEN** his status stays `dnd` until 18:00, then follows activity again

#### Scenario: Away by choice
- **GIVEN** Ana chose `away`
- **WHEN** she keeps writing messages
- **THEN** the people who can see her see `away`, until she chooses another status

#### Scenario: Appearing offline
- **GIVEN** Ana chose `offline` and is connected
- **WHEN** Bruno loads her status and sends her a direct message
- **THEN** he sees `offline`, and the message reaches her at once

#### Scenario: Back to following activity
- **GIVEN** Ana chose `away`
- **WHEN** she chooses `online`
- **THEN** her status is `online` while she is active and `away` after five minutes without activity

## ADDED Requirements

### Requirement: Status menu
The foot of the sidebar SHALL show the user's own status beside their name and open a menu to choose Online,
Away, Do not disturb — for 30 minutes, 1 hour, 2 hours, until tomorrow or until changed — or Appear offline. The
entry in force SHALL be marked, the choice SHALL show at once in the user's open tabs and to the people who can
see them, and the menu SHALL work by keyboard and screen reader in the three languages.

#### Scenario: Choosing a status
- **WHEN** Ana opens the menu and chooses Away
- **THEN** the foot of her sidebar says Away, and Bruno's sidebar shows her as away without a reload

#### Scenario: Do not disturb for a while
- **WHEN** Ana chooses Do not disturb for 30 minutes
- **THEN** her status is `dnd` with an end 30 minutes ahead, and the menu marks that entry

#### Scenario: Closed by the keyboard
- **GIVEN** the menu is open
- **WHEN** Ana presses Escape
- **THEN** it closes and the focus is back on the button that opened it
