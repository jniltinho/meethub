# file-attachments Specification

## Purpose
Lets members share files in channels and conversations: uploading, attaching to messages, downloading under the
channel's access rules, and finding a channel's or one's own files — following Mattermost's file model.

## Requirements

### Requirement: Uploading files
`POST /api/v1/files` (multipart: `channel_id` and one or more `files`) SHALL store each file for a member who may
post in that channel and answer `201` with `{file_infos: [{id, name, extension, size, mime_type, create_at,
has_preview_image, width?, height?}]}`. The media type SHALL be read from the bytes, never from the client's
declaration, and the name SHALL be kept as given, limited to 255 characters and without path separators.

#### Scenario: Upload to a channel
- **WHEN** a member of `#projects` uploads `plan.xlsx`
- **THEN** the answer is `201` with one file info named `plan.xlsx` and its size

#### Scenario: Not a member
- **WHEN** Carla uploads a file to private `#finance`, of which she is not a member
- **THEN** the answer is `404` and nothing is stored

#### Scenario: Declared type ignored
- **WHEN** a member uploads an HTML file declared as `image/png`
- **THEN** its `mime_type` is `text/html` and `has_preview_image` is false

### Requirement: Upload limits
A file larger than `files.max_size_bytes` (default 50 MiB) SHALL answer `413 file_too_large` without being stored;
an upload with more files than a message may carry SHALL answer `400 too_many_files`; an empty file SHALL answer
`400 invalid_input`; and uploads SHALL be limited to 60 files per user in any minute (`429 rate_limited`).

#### Scenario: Too large
- **GIVEN** the default limit
- **WHEN** a member uploads a 60 MiB file
- **THEN** the answer is `413` with code `file_too_large` and no object is left in the storage

#### Scenario: Archived channel
- **WHEN** a member uploads to an archived channel
- **THEN** the answer is `403` with code `channel_archived`

### Requirement: Downloading files
`GET /api/v1/files/{id}` SHALL return the bytes only to callers who may read the file's channel — or, for a file
not attached yet, only to its uploader — and answer `404` to everyone else. It SHALL be served as an attachment
(`Content-Disposition: attachment` with the original name) with `X-Content-Type-Options: nosniff` and a sandboxing
`Content-Security-Policy`, whatever its type.

#### Scenario: Member downloads
- **GIVEN** a file attached to a message of `#projects`
- **WHEN** a member of `#projects` requests it
- **THEN** the original bytes arrive as an attachment named as it was uploaded

#### Scenario: After leaving the channel
- **GIVEN** Bruno left `#projects`
- **WHEN** he requests a file of that channel by its id
- **THEN** the answer is `404`

#### Scenario: HTML never renders
- **WHEN** a member opens the URL of an uploaded HTML file in a browser tab
- **THEN** it is downloaded, not rendered

### Requirement: File lifecycle
Deleting a message SHALL delete its files (no longer listed or downloadable), and deleting a thread's root SHALL
delete the files of its replies. A file never attached to a message SHALL be removed 24 hours after its upload.

#### Scenario: Message deleted
- **GIVEN** a message with two files
- **WHEN** its author deletes it
- **THEN** both files answer `404` and are not in the channel's file list

#### Scenario: Abandoned upload
- **GIVEN** a file uploaded 25 hours ago and never attached
- **WHEN** the cleanup task runs
- **THEN** the file's metadata and bytes are gone

### Requirement: Channel and user file lists
`GET /api/v1/channels/{id}/files` SHALL list the files attached to messages of a channel to its members, newest
first, paged by `cursor`; `GET /api/v1/users/me/files` SHALL list the files of every channel and conversation the
caller belongs to, with `q` matching the name, ignoring case (accented letters included, on every database). Each entry SHALL carry the file info, the channel id,
the message id and the uploader.

#### Scenario: Channel files
- **GIVEN** three files were attached to messages of `#projects`
- **WHEN** a member lists the channel's files
- **THEN** the three come newest first, each with its message id

#### Scenario: Accented name
- **GIVEN** `RELATÓRIO.xlsx` was attached in a channel the user belongs to
- **WHEN** they request their files with `q=relatório`
- **THEN** the file is returned

#### Scenario: Search by name
- **WHEN** a user requests their files with `q=PLAN`
- **THEN** `plan.xlsx` of a channel they belong to is returned and files of channels they do not belong to are not
