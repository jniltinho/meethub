# Tasks

Dependency order: 1 → 2 → 3 → 4 → 5 → 6. Every Go task runs its tests with `-race` on SQLite, MariaDB and PostgreSQL
(`make test-db`) unless it says otherwise.

## 1. Configuration, schema and storage

- [x] 1.1 Add the `files` settings with validation and environment-only S3 secrets, and document them in
  `config.toml.example`; verify table-driven config tests (unknown driver, non-positive limits, `s3` without
  endpoint, bucket or credentials, a secret in the file)
- [x] 1.2 Write migration 00004 for the three dialects (`file_infos` with its indexes) and its down step; verify
  the same-versions test and up/down/up on the three dialects
- [x] 1.3 Implement the storage interface with the `local` driver (temporary name then rename, keys never from
  client text) and the `s3` driver, plus the readiness check; verify one driver test suite run against both (the S3
  one against the RustFS service of the test stack), including a hostile name and an unreachable storage

## 2. Files service and API

- [x] 2.1 Move the image helpers of `internal/identity/picture.go` to `internal/imaging` and keep the picture tests
  green; verify `go test ./internal/identity ./internal/imaging`
- [x] 2.2 Implement uploads (streamed, size limit with the partial object removed, media type sniffed, name
  sanitized, access and archived checks before storing, rate limit) and `POST /files` on the multipart group;
  verify the `file-attachments` upload and limit scenarios, including a 60 MiB stream that leaves no object
- [x] 2.3 Implement derived images at upload (24 MP header limit, thumbnail and preview as JPEG without metadata,
  bounded concurrent decodes, failures never failing the upload); verify the `file-previews` scenarios with a
  photo carrying EXIF, a PDF and a decompression bomb
- [x] 2.4 Implement downloads, thumbnails, previews and info with the access rule (channel readers, or the uploader
  while unattached) and the response headers; verify member, former member, non-member and logged-out callers, the
  attachment disposition of an HTML file, and a range request
- [x] 2.5 Document the routes in `docs/api.md`, the storage and proxy notes in `docs/deployment.md` and the
  serving rules in `docs/security.md`; verify the route documentation test

## 3. Messages with files

- [x] 3.1 Attach files in the posting transaction (conditional claim, empty text allowed with files, idempotent
  retry) and carry `file_ids` and `metadata.files` in the projection, the history and the `posted` event; verify
  the `messages` scenarios, another user's file, a file of another channel and a retry that claims once
- [x] 3.2 Delete a message's files with it (thread cascade included) and implement the channel and user file lists
  with the name filter; verify the lifecycle and list scenarios, including a user who left the channel
- [x] 3.3 Add the hourly cleanup task (unattached for 24 hours, bytes of deleted files, one failing object not
  stopping the rest); verify the `background-jobs` scenarios with two replicas and a failing storage

## 4. Composer and messages in the SPA

- [x] 4.1 Implement the uploads composable and the attachment tray (button, progress, remove, retry, limits with
  themed messages) and send messages with `file_ids`; verify unit tests of the tray's states and Playwright:
  attach, send, the other browser sees the file within a second
- [x] 4.2 Add dropping files on the conversation and pasting images; verify Playwright for a dropped file and a
  pasted image
- [x] 4.3 Render files inside messages (thumbnails, the enlarged image dialog with download and focus return, file
  cards with download); verify Playwright: thumbnail opens and closes with Escape, a document downloads under its
  name, a tombstone shows no files

## 5. Channel files and the Files page

- [x] 5.1 Add the Files action of the channel header with the files state of the side panel and the recent files
  of the channel details; verify Playwright: the panel lists the channel's files and one opens its message
- [x] 5.2 Replace the Files placeholder with the files page (name filter, load more, download, go to message);
  verify Playwright for the `chat-ui` and `web-shell` scenarios
- [x] 5.3 Add the pt-BR and es strings, the contrast recipes and gallery entries of the new elements; verify the
  catalog test and the contrast test at each theme's level

## 6. Integration

- [x] 6.1 Extend `scripts/demo/seed_chat.py` with a few attachments (an image, a document) in `#projetos`; verify
  `make demo` twice and by opening the channel as Ana
- [ ] 6.2 Run `make check`, `make test`, `make test-db`, `make test-e2e`, `make test-load`, `make build`, CI green,
  and a Codex review of the implementation with its findings fixed; verify the CI run and a new `dist/meethub`

## Workflow follow-up

- Archive with `openspec archive add-file-uploads` after review.
- Merge order with `add-livekit-meetings` (developed in parallel on its own branch): whichever lands second rebases
  onto `main` and resolves the shared layout files.
