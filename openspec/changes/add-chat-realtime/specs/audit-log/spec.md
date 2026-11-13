# Spec Delta

## MODIFIED Requirements

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
