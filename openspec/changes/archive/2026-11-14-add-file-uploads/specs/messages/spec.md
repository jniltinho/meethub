# Spec Delta

## ADDED Requirements

### Requirement: Messages with files
`POST /api/v1/posts` SHALL accept `file_ids`: at most `files.max_per_post` (default 10) files uploaded by the
author to that channel and not attached to another message; anything else SHALL answer `400 invalid_input`. A
message with files MAY have an empty text. The message's projection SHALL carry `file_ids` and `metadata.files`
(the file infos, in the given order), in the API and in the `posted` event; a deleted message SHALL carry neither.

#### Scenario: Files alone
- **WHEN** a member posts a message with one file and no text
- **THEN** the answer is `201` and every member receives `posted` with that file's info

#### Scenario: Someone else's upload
- **WHEN** Bruno posts a message with the id of a file Ana uploaded
- **THEN** the answer is `400` with code `invalid_input` and the file stays unattached

### Requirement: Repeating and editing messages with files
A repeat of a `pending_post_id` with the same files in the same order SHALL return the stored message without
attaching anything again; with other files, or the same in another order, it SHALL answer
`409 pending_post_conflict`. Editing MAY empty the text of a message that has files, never of one without.

#### Scenario: Retry with other files
- **GIVEN** a message with two files was stored
- **WHEN** the same `pending_post_id` is sent again with only one of them
- **THEN** the answer is `409` with code `pending_post_conflict` and nothing changes

#### Scenario: Retry with the same key
- **GIVEN** a message with two files was stored
- **WHEN** the same `pending_post_id` is sent again with the same files
- **THEN** the stored message is returned and the files are attached once

#### Scenario: Text removed from a message with a file
- **GIVEN** a message with a text and one file
- **WHEN** its author edits it to an empty text
- **THEN** the answer is `200` and the message keeps its file
