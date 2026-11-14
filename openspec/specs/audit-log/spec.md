# audit-log Specification

## Purpose
Keeps a transactional application history of security-relevant actions so administrators can see who did what and
when, without storing sensitive content (not a tamper-proof log).

## Requirements

### Requirement: Security events are recorded
The system SHALL record an audit event with actor, action, target type, target id, client address and UTC time for:
setup (`system.initialized`), login success, login failure, logout, password change, user creation, role change, user disable/enable,
organization settings changes, team creation, update and archive, team membership and team role changes, channel
creation, update, archive and unarchive, private channel membership and channel role changes, and messages deleted by
someone other than their author.

#### Scenario: Role change recorded
- **WHEN** an admin changes a member's role to `admin`
- **THEN** an audit event `user.role_changed` names the admin as actor, the member as target and the old and new
  roles

#### Scenario: Failed login recorded
- **WHEN** a login fails
- **THEN** an event `auth.login_failed` is recorded with the submitted e-mail and client address, without the password

#### Scenario: Moderator deletes a message
- **WHEN** a channel admin deletes another user's message
- **THEN** an event `post.deleted_by_moderator` names the admin, the channel and the author, without the message text

#### Scenario: Channel archived
- **WHEN** a team admin archives `#finance`
- **THEN** an event `channel.archived` names the admin and the channel

### Requirement: No sensitive content in audit events
Audit events SHALL NOT contain passwords, hashes, session tokens, CSRF tokens or message bodies.

#### Scenario: Password change event
- **WHEN** a user changes their password
- **THEN** the `user.password_changed` event contains no password or hash

### Requirement: Administrators read the audit log
Owners and admins SHALL list audit events through `/api/v1/admin/audit`, ordered by `(created_at, id)` descending,
paginated by an opaque cursor and filterable by actor, action and time range; other roles SHALL get 403.

#### Scenario: Filter by action
- **WHEN** an admin lists audit events with `action=auth.login_failed`
- **THEN** only failed-login events are returned, newest first

### Requirement: Audit write failure does not leak
A failure to write an audit event SHALL be logged as an error, and the action that triggered it SHALL fail when it is
a privileged change (roles, users, settings).

#### Scenario: Database error on role change
- **GIVEN** the audit insert fails
- **WHEN** an admin changes a role
- **THEN** the role change is rolled back and the response is 500

#### Scenario: Stable pages
- **GIVEN** several events share the same `created_at`
- **WHEN** an admin pages through the log 2 events at a time
- **THEN** every event appears exactly once
