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

#### Scenario: Retry with the same key
- **GIVEN** a message with two files was stored
- **WHEN** the same `pending_post_id` is sent again with the same files
- **THEN** the stored message is returned and the files are attached once
