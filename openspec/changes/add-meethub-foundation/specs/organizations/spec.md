# Spec Delta

## Purpose

Defines the organization that owns an install's data, how users belong to it, and the role rules the server enforces
for every action.

## ADDED Requirements

### Requirement: Single organization per install
In this release an install SHALL have exactly one organization, created at first-run setup, and every user SHALL be a
member of it with one role.

#### Scenario: Organization in the profile
- **WHEN** a logged-in user requests `/api/v1/me`
- **THEN** the response includes the organization's id, slug, name and the user's role

### Requirement: Roles
Membership roles SHALL be `owner`, `admin`, `member` and `guest`; there SHALL always be at least one active `owner`.

#### Scenario: Last owner cannot be demoted
- **GIVEN** exactly one active owner
- **WHEN** an attempt is made to change their role or disable them
- **THEN** it fails with code `last_owner`

### Requirement: Server-side authorization
Every API action SHALL be authorized on the server from the session's user and role, regardless of what the UI shows.

#### Scenario: Member calls an admin endpoint
- **GIVEN** a logged-in `member`
- **WHEN** they call `POST /api/v1/admin/users`
- **THEN** the response is 403 with code `forbidden`

### Requirement: User management by administrators
Owners and admins SHALL list, create, disable, re-enable and change the role of users through `/api/v1/admin/users`;
only owners SHALL grant or remove the `owner` role.

#### Scenario: Admin creates a member
- **WHEN** an admin creates a user with role `member` and an initial password
- **THEN** the user can log in and appears in the user list

#### Scenario: Admin cannot create an owner
- **WHEN** an admin creates or promotes a user to `owner`
- **THEN** the response is 403 with code `forbidden`

#### Scenario: Disabling a user ends their sessions
- **GIVEN** a user with an active session
- **WHEN** an admin disables them
- **THEN** their next request gets 401

### Requirement: Own profile and preferences
Every user SHALL update their own display name, password (with the current password) and preferences, including the
theme and the language.

#### Scenario: Change own password
- **WHEN** a user submits their current password and a valid new one
- **THEN** the password changes and their other sessions are revoked
