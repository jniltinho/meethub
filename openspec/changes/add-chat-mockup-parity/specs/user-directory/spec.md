# Spec Delta

## MODIFIED Requirements

### Requirement: Public profile
Outside administration, the API SHALL expose other users only as
`{id, username, display_name, status_visible, last_picture_update}` — never their e-mail, login times or role in the
organization; `last_picture_update` is the time (ms) of the user's current picture, `0` without one. A user's own
profile and the administration endpoints are unchanged.

#### Scenario: Member list without e-mails
- **WHEN** a member loads the members of `#projects`
- **THEN** the response has usernames and display names and no e-mail addresses

#### Scenario: Picture version
- **GIVEN** Ana uploaded a picture and Bruno never did
- **WHEN** a member loads both profiles
- **THEN** Ana's `last_picture_update` is the upload time and Bruno's is `0`
