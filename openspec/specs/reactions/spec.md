# reactions Specification

## Purpose
Defines emoji reactions on messages: who may react, which emoji names are accepted, and how reactions are counted and
announced.

## Requirements

### Requirement: Adding and removing reactions
Members of a channel SHALL add and remove their own reactions to its messages; each user SHALL have at most one
reaction per emoji per message, and adding an existing one SHALL change nothing.

#### Scenario: Same reaction twice
- **GIVEN** Ana reacted `+1` to a message
- **WHEN** she adds `+1` again
- **THEN** the message still has one `+1` from Ana and no new event is sent

#### Scenario: Reaction announced
- **WHEN** Bruno adds `white_check_mark` to a message
- **THEN** members receive `reaction_added` with the message id, Bruno and the emoji name

#### Scenario: Reaction in an archived channel
- **WHEN** a member reacts in an archived channel
- **THEN** the response is 403 with code `channel_archived`

### Requirement: Emoji names
Reactions SHALL use emoji short names from the built-in emoji set (Mattermost/GitHub style names such as `+1`,
`rocket`, `tada`), and unknown names SHALL be rejected.

#### Scenario: Unknown emoji
- **WHEN** a user reacts with `not_an_emoji`
- **THEN** the response is 400 with code `invalid_input`

### Requirement: Reactions with messages
Messages returned by the API SHALL include their reactions grouped by emoji with the users who reacted, and a deleted
message SHALL lose its reactions.

#### Scenario: Grouped counts
- **GIVEN** three users reacted `rocket` to a message
- **WHEN** the history is loaded
- **THEN** that message shows `rocket` with a count of 3 and the three users
