# Spec Delta

## Purpose

Defines how users find each other in the chat (mentions, new messages, member lists) without exposing more than needed:
which users a caller may see and which profile fields are public.

## ADDED Requirements

### Requirement: Public profile
Outside administration, the API SHALL expose other users only as `{id, username, display_name, status_visible}` —
never their e-mail, login times or role in the organization; a user's own profile and the administration endpoints are
unchanged.

#### Scenario: Member list without e-mails
- **WHEN** a member loads the members of `#projects`
- **THEN** the response has usernames and display names and no e-mail addresses

### Requirement: Who may see whom
A non-guest SHALL see every active user of the organization; a guest SHALL see only users who share a team channel or
conversation with them; disabled users SHALL be visible only as authors of existing messages.

#### Scenario: Guest directory
- **GIVEN** a guest added only to `#support`
- **WHEN** they search users for a new message
- **THEN** only members of `#support` (and their existing conversations) are returned

### Requirement: Directory queries
`GET /api/v1/users` SHALL search by username or display name (`q`), optionally within a team (`in_team`, the caller
being a member) or a channel (`in_channel`, the caller able to read it, else 404), cursor-paginated with at most 200 per
page.

#### Scenario: Members of a private channel
- **WHEN** a non-member asks `in_channel` for a private channel
- **THEN** the response is 404 with code `not_found`

#### Scenario: Mention autocomplete
- **WHEN** the composer asks for users matching `an` in `#projects`
- **THEN** members whose username or display name starts with `an` come first
