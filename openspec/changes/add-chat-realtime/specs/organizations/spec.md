# Spec Delta

## MODIFIED Requirements

### Requirement: Roles and permissions
Roles SHALL be ranked `owner` > `admin` > `member` > `guest`, and each role SHALL grant named permissions as follows:

| Permission | owner | admin | member | guest |
| --- | --- | --- | --- | --- |
| `manage_system` (organization settings) | ✓ | | | |
| `manage_owners` (grant/remove owner, edit owners) | ✓ | | | |
| `manage_users` (create, disable, enable, set role ≤ admin, reset password of non-owners) | ✓ | ✓ | | |
| `read_audits` | ✓ | ✓ | | |
| `create_team` | ✓ | ✓ | | |
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

#### Scenario: Admin creates a team
- **GIVEN** a logged-in `admin`
- **WHEN** they call `POST /api/v1/teams`
- **THEN** the team is created

## ADDED Requirements

### Requirement: Usernames
Every user SHALL have a unique username (Mattermost rules: 3–22 characters, lowercase letters, digits, `.`, `-` and
`_`, starting with a letter, compared case-insensitively, reserved words `all`, `channel`, `here` and `system` refused);
it SHALL be given at creation or derived from the e-mail's local part (made unique with a numeric suffix), and users
SHALL change their own.

#### Scenario: Derived from the e-mail
- **WHEN** an administrator creates `ana.silva@example.com` without a username
- **THEN** the user's username is `ana.silva`, or `ana.silva2` if `ana.silva` is taken

#### Scenario: Reserved username
- **WHEN** a user changes their username to `here`
- **THEN** the response is 400 with code `invalid_username`

#### Scenario: Existing users after the upgrade
- **GIVEN** users created before usernames existed
- **WHEN** the server first starts after the upgrade (with migrations applied)
- **THEN** each of them has a unique username derived from their e-mail before the server accepts requests
