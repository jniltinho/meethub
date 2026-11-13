# Spec Delta

## Purpose

Defines teams, the groups of people and channels inside the organization (Mattermost teams; the "Workspace" switcher
of the UI), who belongs to them, with which team role, and what leaving a team takes away.

## ADDED Requirements

### Requirement: Teams
A team SHALL have an id, a unique URL name (lowercase letters, digits and `-`, 2–64 characters), a display name
(1–64 characters), a description (up to 255 characters), a type `open` (non-guest members of the organization may join)
or `invite` (members are added), and an archive state.

#### Scenario: Duplicate team name
- **GIVEN** a team named `engineering`
- **WHEN** an owner creates another team named `Engineering`
- **THEN** creation fails with code `name_taken`

### Requirement: Creating, updating, archiving and restoring teams
Users with `create_team` SHALL create teams and become their `team_admin`; team admins and `manage_system` SHALL update,
archive (`POST /api/v1/teams/{id}/archive`) and restore (`…/restore`) a team; the default team SHALL NOT be archived; an
archived team's channels SHALL be read-only; each change SHALL be audited and announced (`update_team`, `delete_team`,
`restore_team`).

#### Scenario: Member cannot create a team
- **WHEN** a `member` calls `POST /api/v1/teams`
- **THEN** the response is 403 with code `forbidden`

#### Scenario: Archived team
- **WHEN** a team admin archives a team
- **THEN** its channels reject new messages and it disappears from its members' team switcher

#### Scenario: Archiving the default team
- **WHEN** an owner archives the default team
- **THEN** the response is 400 with code `default_team`

### Requirement: Team membership and roles
Team members SHALL have the role `team_user` or `team_admin`; team admins SHALL add and remove members and change team
roles (never their own); non-guests SHALL join `open` teams; guests SHALL only be added; any member SHALL leave a team
except the default team's last team admin.

#### Scenario: Join an open team
- **WHEN** a member joins an `open` team
- **THEN** they become a `team_user`, a member of its default channel, and receive `added_to_team`

#### Scenario: Invite-only team
- **WHEN** a member tries to join an `invite` team
- **THEN** the response is 403 with code `forbidden`

#### Scenario: Guests are added, never join
- **WHEN** a guest tries to join an `open` team
- **THEN** the response is 403 with code `forbidden`

### Requirement: Leaving a team removes its channels
Removing a user from a team (or the user leaving) SHALL, in the same transaction, remove them from every channel of that
team, so they immediately lose read access to its public and private channels; the user SHALL receive `leave_team` and
the remaining members the corresponding `user_removed` events.

#### Scenario: Private channel after team removal
- **GIVEN** Diego is a member of the private `#finance` in team `ops`
- **WHEN** a team admin removes Diego from `ops`
- **THEN** Diego gets 404 for `#finance` messages, threads, reactions and members, and receives no further events of it

### Requirement: Default team
The organization SHALL have a default team (`open`) holding every non-guest member; a new non-guest user SHALL be
added to it and its `general` channel in the same transaction that creates the user.

#### Scenario: New user lands in the default team
- **WHEN** an administrator creates a `member`
- **THEN** that user belongs to the default team and its default channel as soon as the creation commits

### Requirement: Teams visible to a user
`GET /api/v1/users/me/teams` SHALL return the user's teams with their team role and per-team unread totals, and
`GET /api/v1/teams` SHALL list, paginated, the `open` teams a non-guest may join.

#### Scenario: Invite-only team not listed
- **GIVEN** an `invite` team the member does not belong to
- **WHEN** they list joinable teams
- **THEN** that team is not in the list
