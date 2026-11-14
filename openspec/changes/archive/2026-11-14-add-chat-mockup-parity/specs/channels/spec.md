# Spec Delta

## ADDED Requirements

### Requirement: Favorite channels
`PUT /api/v1/users/me/channels/{id}/favorite` with `{favorite}` SHALL mark or unmark a channel or conversation as the
caller's favorite, for members only (`404` otherwise), answering `204`; the caller's channel and conversation lists
SHALL carry `favorite`, and their other connections SHALL receive `preferences_changed` with the channel id and the new
value. A favorite is personal: no other member sees it.

#### Scenario: Mark a favorite
- **WHEN** Ana marks `#projects` as favorite
- **THEN** her channel list has `favorite: true` for `#projects` and Bruno's list is unchanged

#### Scenario: Two browsers
- **GIVEN** Ana has two browsers open
- **WHEN** she unmarks `#projects` in one
- **THEN** the other moves `#projects` out of Favorites within a second

#### Scenario: Not a member
- **WHEN** Carla marks private `#finance`, of which she is not a member, as favorite
- **THEN** the answer is `404`
