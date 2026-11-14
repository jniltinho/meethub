# Spec Delta

## MODIFIED Requirements

### Requirement: Server events
The server SHALL emit `hello`, `posted`, `post_edited`, `post_deleted`, `reaction_added`, `reaction_removed`,
`typing`, `status_change`, `channel_viewed`, `channel_created`, `channel_updated`, `channel_deleted`,
`channel_restored`, `channel_member_updated`, `direct_added`, `group_added`, `user_added`, `user_removed`,
`added_to_team`, `leave_team`, `update_team`, `delete_team`, `restore_team`, `user_role_updated`, `user_updated`,
`preferences_changed` and `resync`.

#### Scenario: Channel renamed
- **WHEN** a channel admin renames `#devops`
- **THEN** its members receive `channel_updated` with the new name

#### Scenario: Profile changed
- **WHEN** Ana changes her profile picture
- **THEN** the users who may see her receive `user_updated` with her public profile, and nobody else does

#### Scenario: Own preference
- **WHEN** Ana marks a channel as favorite
- **THEN** only Ana's connections receive `preferences_changed`
