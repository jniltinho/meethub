# Spec Delta

## ADDED Requirements

### Requirement: History around a message
`GET /api/v1/channels/{id}/posts?around=<post id>` SHALL return that message with up to `per_page / 2` messages before
and after it, in the history's projection, plus `prev_cursor` and `next_cursor` to continue backwards (as the history
does) and forwards with `?after=<cursor>`; a message that is not in that channel, or that the caller may not read,
SHALL answer `404`.

#### Scenario: Middle of the history
- **GIVEN** a channel with 1 000 messages
- **WHEN** a member requests the history around message number 500 with `per_page=60`
- **THEN** they receive messages 470 to 530 with cursors that continue in both directions

#### Scenario: Forward to the newest
- **GIVEN** a client holding the `next_cursor` of an `around` response
- **WHEN** it keeps requesting `?after=<cursor>`
- **THEN** it reaches the newest message and the last page has no `next_cursor`

#### Scenario: Message of another channel
- **WHEN** a member requests `#general`'s history around the id of a message of private `#finance`
- **THEN** the answer is `404`
