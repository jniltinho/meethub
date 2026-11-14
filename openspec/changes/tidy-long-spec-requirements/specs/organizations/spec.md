# Spec Delta

## MODIFIED Requirements

### Requirement: Roles and permissions
Roles SHALL be ranked `owner` > `admin` > `member` > `guest`, and each role SHALL grant named permissions as follows:

| Permission | owner | admin | member | guest |
| --- | --- | --- | --- | --- |
| `manage_system` | ✓ | | | |
| `manage_owners` | ✓ | | | |
| `manage_users` | ✓ | ✓ | | |
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

### Requirement: What each permission covers
`manage_system` SHALL cover the organization's settings; `manage_owners` granting and removing the owner role and
editing owners; `manage_users` creating, disabling and enabling users, setting roles up to `admin` and resetting the
passwords of users who are not owners.

#### Scenario: Admin promotes to admin, not to owner
- **GIVEN** a logged-in `admin`
- **WHEN** they set a member's role to `admin`, and then another member's to `owner`
- **THEN** the first is applied and the second answers 403 with code `forbidden`
