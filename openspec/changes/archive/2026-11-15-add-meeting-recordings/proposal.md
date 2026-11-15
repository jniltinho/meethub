# Meeting recordings

## Why

A meeting that is not recorded is lost to everyone who could not attend, and the decisions taken in it live only in
the memory of those who did. Teams ask for the recording first, and a recording is also what a later change needs to
write a summary of what was said (`add-meeting-summaries`).

LiveKit's own answer is its Egress service: another server, with a headless browser, to deploy and operate. MeetHub
aims at one binary that a small team can run, so this change records inside MeetHub: a hidden participant captures
each person's audio and video with Go libraries the server already depends on, and `ffmpeg` turns the captures into
one file when the meeting ends.

## What Changes

- A member allowed to record starts and stops a recording from the meeting room. One recording per room at a time.
- Everyone is told: a "Recording" mark in the room for as long as it runs, an announcement when it starts and stops,
  and a notice on the pre-join screen before entering a room that is being recorded.
- A hidden recorder participant (the LiveKit Go SDK, as a subscriber that publishes nothing) captures every
  person's audio (Opus) and, in video mode, their camera and shared screen, to files on the replica's disk. It never
  shows as a participant.
- When the recording stops — by hand, when the room has been empty for 30 seconds, at the maximum duration or
  size, or when the channel is archived — the `ffmpeg` binary mixes the audio into one file (`.m4a`), or composes
  audio and video into one `.webm` (the shared screen with the cameras in a strip, or a grid of cameras), on the
  same timeline. A recording that could not capture everything says so (it is marked partial).
- The result is posted in the channel as a message with the recording attached. It is an ordinary attachment: who
  may read the channel may play and download it, deleting the message deletes it, and it is stored by the file
  storage driver (disk or S3).
- Recordings play in the message (audio or video player) and list in the channel details.
- `[recordings]` settings: on/off (off by default), audio or video, who may record, maximum duration, retention in
  days, the `ffmpeg` path. With recordings on and no usable `ffmpeg`, the server refuses to start and says why.
- The container images carry an `ffmpeg` built from source without GPL components.
- A recording is owned by one server through a lease: stopping works from any replica, a server that cannot renew
  its lease stops recording at once, and a room whose recording server vanished stops showing "Recording" within
  two minutes.
- Starting and stopping a recording are audited.

Assumptions recorded here because they shape the scope (change them before implementation if they are wrong):

- **Capture in Go, processing with the `ffmpeg` binary** — not Go bindings to FFmpeg's libraries (cgo would end the
  static pure-Go binary) and not a wrapper library around the command line (it only builds an argument list).
- **Audio is the default mode**; video is opt-in because composing it costs CPU and disk after every meeting.
- **Video is VP8 in WebM**, so nothing GPL ships in the images. MP4 (H.264) needs an `ffmpeg` the operator
  supplies. Cameras published in H.264 or AV1 are not captured (the web app publishes VP8).
- **A feasibility spike comes first** (design D0): the media timeline and the recorder's network path are proved
  with the pinned versions before anything else is built; audio only is the fallback scope if only the video gate
  fails.
- **By default channel administrators record** (team administrators and `manage_system` too); in direct and group
  conversations, which have no administrators, any member does. `recordings.start = "members"` lets any member.
- **No consent prompt per person**: people are informed (mark, announcement, pre-join notice) and may leave.
  Whether that is enough is a legal question of each organization; the documentation says so.

## Capabilities

### New Capabilities

- `meeting-recordings`: starting and stopping a recording, what is captured, how it ends, the result in the
  channel, failure and recovery, retention.

### Modified Capabilities

- `meetings`: the recorder is not a participant of the room.
- `realtime-events`: `meeting_updated` carries the room's recording state.
- `meeting-room-ui`: the record control, the "Recording" mark, announcements and the pre-join notice.
- `chat-ui`: recordings inside messages (player) and in the channel details.
- `configuration`: the `recordings` settings and their validation.
- `build-and-deploy`: `ffmpeg` in the runtime and all-in-one images.
- `audit-log`: recording started and stopped.

## Impact

- **Server**: a new `internal/recordings` package (the recorder, the `ffmpeg` runner, the service), migration
  `00011` (`meeting_recordings`), routes under `/api/v1/channels/{id}/meeting/recording(s)`, wiring in
  `cmd/server.go`. `internal/meetings` excludes the recorder from room presence and adds the recording state to the
  room snapshot. `internal/files` gains a way to store a server-made file (no upload limit) for a post.
- **Dependencies**: none new in Go — `github.com/livekit/server-sdk-go/v2` (already used) brings the room client and
  Pion's media writers. A runtime dependency on the `ffmpeg` executable when recordings are on.
- **Images**: an LGPL `ffmpeg` built in a Dockerfile stage from pinned source, copied into the runtime and the
  all-in-one image; `NOTICE` and `THIRD_PARTY` carry the licenses, the source and the build configuration.
- **Chat**: a server-side publication operation for system posts with typed metadata (`props` in the post
  projection), used for the recording and failure messages.
- **Operations**: disk for captures in progress (`<data_dir>/recordings`, a volume that must survive restarts),
  CPU after each meeting, more file storage, limits on concurrent recordings and their size. A recording in
  progress is lost with its capture volume.
- **Compose and all-in-one**: LiveKit must advertise an address the server reaches for media, not only the
  browsers' one.
- **Web app**: the room's controls and marks, a media player for recordings in messages, strings in the three
  languages, contrast recipes.
- **Docs**: `docs/api.md`, `docs/deployment.md` (settings, `ffmpeg`, disk and CPU), `docs/security.md` (who may
  record, what is stored, informing participants).
