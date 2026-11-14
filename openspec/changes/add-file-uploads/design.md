# Design

## Context

See proposal.md for the motivation. What shapes the approach:

- Posts are written under the channel lock with a revision (`internal/chat/posts.go`); their projection (`PostView`)
  is what the API and the `posted` event carry, and the SPA's store ignores anything older than the revision it holds.
- The one multipart route so far is the profile picture (`internal/httpserver/handlers/pictures.go`): it has its own
  body limit (`httpserver.Options.BodyLimits`) and a same-origin middleware for `multipart/form-data`. Pictures live
  in the database; attachments are too large for that.
- Several replicas share one database and one Redis, not a disk. Secrets come only from the environment.
- `internal/identity/picture.go` already sniffs formats, checks dimensions from the header before decoding and scales
  with a box filter.
- `add-livekit-meetings` is implemented in parallel in its own branch and takes migration `00005`; both changes touch
  the channel header, the details panel, the chat store, `style.css` and the catalogs.

## Goals / Non-Goals

**Goals:**

- A file is never served in a way the browser renders from MeetHub's origin, and never to someone who cannot read its
  channel.
- Uploads are streamed to the storage, not buffered in memory, at any configured size.
- The same behavior on the three databases and with both drivers.

**Non-Goals:**

- Resumable or chunked uploads, deduplication, quotas, virus scanning, previews beyond images.
- Changing where profile pictures live.

## Decisions

### D1. A `files` package with a small storage interface

`internal/files` owns the model (`FileInfo`, Mattermost's name), the service and the drivers. The storage is the
consumer-side interface the service needs — `Put(ctx, key, r, size)`, `Open(ctx, key)`, `Delete(ctx, key)`,
`Check(ctx)` — with `local` (files under a root, written to a temporary name and renamed) and `s3`
(`github.com/minio/minio-go/v7`, which speaks to MinIO and AWS alike). `internal/chat` does not import the storage:
it calls a `Files` interface of two methods to attach files inside the posting transaction and to delete them with
a message.

*Alternative considered:* the AWS SDK — larger, and MinIO is the self-hosted case. *Alternative considered:* the
database — rejected with the picture precedent in mind: 50 MiB rows are slow on every dialect.

### D2. Schema (migration 00004, three dialects)

`file_infos`: `id`, `creator_id`, `channel_id`, `post_id` (nullable until attached), `name`, `extension`, `size`,
`mime_type`, `width`, `height`, `has_preview_image`, `path`, `thumbnail_path`, `preview_path`, `position` (its
order in the message), `create_at`, `update_at`, `delete_at`. Indexes: `(channel_id, create_at, id)` for the channel
list and `(post_id, position)` for a message's files, which also finds the unattached uploads for the cleanup. Posts
gain no column: `file_ids` in the projection come from `file_infos` ordered by `position`.

### D3. Upload path

`POST /files` is registered beside the picture route (multipart same-origin group) with a body limit of
`max_size_bytes × max_per_post` plus overhead. The handler reads the multipart stream part by part: `channel_id`
first, then each file is piped to `Storage.Put` through a reader that counts bytes and fails at `max_size_bytes`
(the partial object is deleted), while the first 512 bytes are kept to sniff the media type. Access is checked
before any byte is stored (`CanPost` on the channel, not archived). Rate limit: `ratelimit.NewWindow`, 60 files per
user per minute.

Keys: `<yyyymmdd>/<channel id>/<file id>/<file id>` plus `_thumb.jpg` and `_preview.jpg`; nothing from the client.

### D4. Attaching in the posting transaction

`CreatePost` takes `FileIDs`. Under the channel lock, after the idempotency check, the files are claimed with one
conditional update (`post_id IS NULL AND creator_id = author AND channel_id = channel AND delete_at = 0`); fewer
rows than ids means one of them is not attachable and the whole post fails with `invalid_input`. A repeat of the
same `pending_post_id` returns the stored message with the files it already has, so a retry never claims twice.
`checkMessage` allows an empty text when there are files. `PostView` gains `file_ids` and `metadata.files`;
`views()` loads them for a page of posts in one query, as it does for reactions.

### D5. Serving

`GET /files/{id}` loads the file info, applies `chat`'s read check on its channel (or "uploader only" while
unattached), and streams from the storage with `Content-Disposition: attachment; filename*=UTF-8''…`,
`Content-Type` as stored, `nosniff`, `Content-Security-Policy: sandbox; default-src 'none'` and `Cache-Control:
private`. Range requests are served by the standard library when the driver returns a seekable reader. Thumbnails
and previews are the only responses the browser may render inline, and they are the server's own JPEG encodings.

### D6. Derived images

The scaling of `internal/identity/picture.go` moves to `internal/imaging` (sniff, header limits, box filter) and
both callers use it. At upload, an image of at most 24 megapixels is decoded once from the stored object (not from
memory of the upload), scaled to the thumbnail and the preview, encoded as JPEG (quality 85) and stored beside the
original; failures leave `has_preview_image` false and never fail the upload. Decodes are bounded by a small
semaphore in the service, so concurrent uploads cannot multiply the memory of a 24 MP decode.

### D7. Lifecycle

Deleting a post sets `delete_at` on its files in the same transaction (the thread cascade includes the replies'
files); the bytes are removed by the hourly Asynq task, which also removes uploads unattached for 24 hours. The
task uses the existing period claim, deletes object by object, and leaves a row whose object could not be deleted
for the next run.

### D8. Frontend

- A `useUploads` composable per composer: the attachment tray (name, size, progress, remove, retry), uploads through
  `XMLHttpRequest` (fetch reports no upload progress) with the CSRF header, and the list of ready `file_ids` for
  `chat.send`. Sending waits for the uploads in flight.
- `MessageFiles.vue` renders thumbnails and file cards; `ImageDialog.vue` is the enlarged preview on the themed
  modal. Images use `/files/{id}/thumbnail` and `/preview`; downloads are plain links with `download`.
- The side panel gains a fourth state, `files`, beside details, thread and search; `ChannelDetails.vue` shows the
  five most recent files; `FilesView.vue` replaces the placeholder route.
- Drag-and-drop is handled on the channel view (a themed drop overlay); paste on the composer's textarea.

### D9. Mattermost parity

| MeetHub (`/api/v1`) | Mattermost v4 | Deviation |
| --- | --- | --- |
| `POST /files` | same | multipart only; answers `{file_infos}` without `client_ids` |
| `GET /files/{id}`, `/thumbnail`, `/preview`, `/info` | same | always an attachment; no public links |
| `POST /posts` with `file_ids` | same | — |
| `GET /channels/{id}/files`, `GET /users/me/files` | file search | lists with a name filter instead of the search syntax |

## Risks / Trade-offs

- [A shared disk is required for `local` with several replicas] → documented in `docs/deployment.md`; the readiness
  check fails on a replica that cannot write the directory.
- [Large uploads hold a connection for a long time] → the proxy timeouts in `docs/deployment.md` gain an upload
  row; the limit is configurable downwards.
- [Orphans when a post transaction fails after the upload] → uploads are unattached rows by design; the cleanup
  removes them.
- [Merge conflicts with the meetings branch] → edits to the shared layout files stay additive; whichever change
  lands second rebases.
- [Stored HTML or SVG used for attacks] → attachment disposition, `nosniff` and a sandboxing CSP on every file
  response; inline rendering only of the server's own encodings.

## Migration Plan

Migration `00004` is additive. New settings have defaults, so an upgraded installation keeps working with files
under the data directory. Rollback: `meethub migrate down` drops `file_infos`; stored objects stay on disk or in
the bucket and can be removed by hand.
