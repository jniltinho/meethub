# organizations Specification

## Purpose
Defines the organization that owns an install's data, how users belong to it, and the role and permission rules the
server enforces for every action (modelled on Mattermost's system roles).

## Requirements

### Requirement: Single organization per install
In this release an install SHALL have exactly one organization, created at initialization, and every user SHALL be a
member of it with one role.

#### Scenario: Organization in the profile
- **WHEN** a logged-in user requests `/api/v1/me`
- **THEN** the response includes the organization's id, slug, name and the user's role

### Requirement: Roles and permissions
Roles SHALL be ranked `owner` > `admin` > `member` > `guest`, and each role SHALL grant named permissions as follows:

| Permission | owner | admin | member | guest |
| --- | --- | --- | --- | --- |
| `manage_system` (organization settings) | ✓ | | | |
| `manage_owners` (grant/remove owner, edit owners) | ✓ | | | |
| `manage_users` (create, disable, enable, set role ≤ admin, reset password of non-owners) | ✓ | ✓ | | |
| `read_audits` | ✓ | ✓ | | |
| `edit_own_profile` | ✓ | ✓ | ✓ | ✓ |

#### Scenario: Owner passes admin checks
- **GIVEN** a logged-in `owner`
- **WHEN** they call any endpoint requiring `manage_users`
- **THEN** the request is authorized

#### Scenario: Member calls an admin endpoint
- **GIVEN** a logged-in `member`
- **WHEN** they call `POST /api/v1/admin/users`
- **THEN** the response is 403 with code `forbidden`

#### Scenario: Admin edits an owner
- **GIVEN** a logged-in `admin`
- **WHEN** they disable, demote or reset the password of an `owner`
- **THEN** the response is 403 with code `forbidden`

### Requirement: Server-side authorization
Every API action SHALL be authorized on the server from the session's user and permissions, regardless of what the UI
shows, and `/api/v1/me` SHALL list the caller's permissions so the UI can hide what is not allowed.

#### Scenario: Hidden button, direct call
- **GIVEN** a `member` who sees no Administration menu
- **WHEN** they call `GET /api/v1/admin/audit` directly
- **THEN** the response is 403 with code `forbidden`

### Requirement: At least one active owner
There SHALL always be at least one active `owner`; changes to owners SHALL be serialized in a transaction so
concurrent changes cannot leave zero owners.

#### Scenario: Last owner cannot be demoted
- **GIVEN** exactly one active owner
- **WHEN** an attempt is made to change their role or disable them
- **THEN** it fails with code `last_owner`

#### Scenario: Two owners disable each other at once
- **GIVEN** exactly two active owners
- **WHEN** each disables the other at the same time
- **THEN** one change succeeds and the other fails with `last_owner`

### Requirement: User management API
Users with `manage_users` SHALL use `GET /api/v1/admin/users` (cursor-paginated, ordered by name then id, filter by
role, status and text), `POST /api/v1/admin/users` (e-mail, display name, role, initial password) and
`PATCH /api/v1/admin/users/:id` (only `display_name`, `role`, `status`); unknown fields SHALL be rejected.

#### Scenario: Admin creates a member
- **WHEN** an admin creates a user with role `member` and an initial password
- **THEN** the user can log in and appears in the user list

#### Scenario: Unknown field
- **WHEN** an admin sends `PATCH /api/v1/admin/users/:id` with `{"email": "x@example.com"}`
- **THEN** the response is 400 with code `invalid_input` and nothing changes

#### Scenario: Disabling a user ends their sessions
- **GIVEN** a user with an active session
- **WHEN** an admin disables them
- **THEN** their next request gets 401

### Requirement: Own profile and preferences
Every user SHALL update their own display name, password (with the current password) and preferences (theme,
language) through `/api/v1/me`.

#### Scenario: Change own preferences
- **WHEN** a user sets the theme `classic-blue` and the language `es`
- **THEN** `/api/v1/me` returns those preferences afterwards
