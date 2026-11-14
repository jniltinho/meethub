# message-search Specification

## Purpose
Lets a user find messages by their text across the channels and conversations they belong to, following the query
habits of Mattermost's search, without ever revealing a message they could not open.

## Requirements

### Requirement: Search scope
`POST /api/v1/posts/search` with `{terms}` SHALL search only messages of channels and conversations the caller is a
member of at that moment, in every team; deleted messages and messages of archived channels SHALL never be returned.

#### Scenario: Private channel of others
- **GIVEN** private `#finance`, of which Carla is not a member, has a message "budget approved"
- **WHEN** Carla searches `budget`
- **THEN** that message is not in the results

#### Scenario: After leaving
- **GIVEN** Bruno left `#devops`
- **WHEN** he searches a word used only there
- **THEN** there are no results

### Requirement: Query syntax
A message SHALL match when its text contains every term, ignoring letter case; text between double quotes is one term
with its spaces. `from:<username>` SHALL keep only that author's messages and `in:<channel name>` only that channel's;
several of the same modifier SHALL be alternatives.

#### Scenario: All terms
- **GIVEN** messages "deploy at 17h" and "deploy tomorrow"
- **WHEN** the user searches `deploy 17h`
- **THEN** only the first is returned

#### Scenario: Phrase and author
- **WHEN** the user searches `"release notes" from:carla.pereira`
- **THEN** only Carla's messages containing "release notes" are returned

#### Scenario: Case
- **WHEN** the user searches `REDIS`
- **THEN** a message containing "Redis" is returned

### Requirement: Invalid queries
A query without a term of at least 2 characters, with more than 10 terms, longer than 256 characters, or naming an
unknown user or a channel the caller is not a member of SHALL answer `400 invalid_input` with a message that says
which part is wrong.

#### Scenario: Too short
- **WHEN** the user searches `a`
- **THEN** the answer is `400` with code `invalid_input`

#### Scenario: Channel of others
- **WHEN** Carla searches `budget in:finance` without being a member of `#finance`
- **THEN** the answer is `400 invalid_input`, the same as for a channel that does not exist

### Requirement: Results
Results SHALL come newest first as `{posts, has_more}` in the history's projection, paged with `page` (from 0) and
`per_page` (default 20, max 100), together with `channels`: for each channel of the page, its id, type, name, display
name and team id.

#### Scenario: Paging
- **GIVEN** 45 messages match
- **WHEN** the user requests page 0 and then page 2 with the default size
- **THEN** page 0 has the 20 newest with `has_more: true` and page 2 has the 5 oldest with `has_more: false`

### Requirement: Search rate limit
Searches SHALL be limited to 10 per user in any 10 seconds, shared by the replicas; above it the answer SHALL be
`429 rate_limited` with `Retry-After`.

#### Scenario: Burst
- **WHEN** a user sends 11 searches in two seconds
- **THEN** the eleventh answers `429` with a `Retry-After` header
