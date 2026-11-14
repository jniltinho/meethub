# Design

## Context

- Meetings (`internal/meetings`) keep one persistent LiveKit room per channel, issue two-minute tokens to people who
  may read the channel, learn who is in a room from LiveKit's signed webhooks and correct that every 3 seconds from
  LiveKit's room API (`Refresh`, `Evict`). The room snapshot (`meeting_updated`, `GET /channels/{id}/meeting`) has a
  revision and the participant list.
- The server already imports `github.com/livekit/server-sdk-go/v2` (v2.19.0, with Pion WebRTC v4) for the room API.
  The same module joins a room as a participant with a custom token and hands each subscribed track as a Pion
  `TrackRemote`; Pion ships writers for Opus (`oggwriter`) and VP8/VP9 (`ivfwriter`) and a `samplebuilder`.
  LiveKit's "filesaver" example is this pattern, including keyframe requests after loss.
- File attachments (`internal/files`, `internal/chat/files.go`) store bytes through a four-method storage interface
  (S3 uploads are multipart) and attach files to a post inside the posting transaction; access to a file is access
  to its channel. `chat.CreatePost` marks the channel read for its author and parses mentions; posts have a `type`
  but no typed metadata in their projection.
- The binary is static, pure Go (no cgo), linux/amd64. The runtime image is distroless; the all-in-one image is
  Alpine-based. In the Compose stack LiveKit advertises `127.0.0.1` for media, which only a browser on the host
  reaches — not another container.
- Several replicas share the database, Redis and the file storage, not a disk. Periodic tasks and queued tasks share
  one worker pool.

## Goals / Non-Goals

**Goals:**

- A recording any member of the channel can play in a current browser, produced without another service to run.
- What the room shows converges to the truth within stated bounds: a recorder stops within 15 seconds of a stop
  request or of losing its lease, and a room stops showing "Recording" within two minutes of its server vanishing.
- A crash, a restart or an overload never leaves a recording running or processing forever, and never posts twice.
- Nothing GPL in the images MeetHub distributes.

**Non-Goals:**

- Live streaming, recording without anyone in the room, per-person consent prompts or opt-out, editing or trimming,
  transcripts and summaries (`add-meeting-summaries`), LiveKit Egress, real-time composition, H.264 or AV1 capture.

## Decisions

### D0. A feasibility spike comes first

Before any schema or interface work, a throwaway program proves, with the exact pinned versions and the browsers
the web app supports, the four things this design rests on: (1) a hidden, subscribe-only participant joins with a
custom token and receives every track; (2) Opus with DTX, mute and unmute, packet loss and a reconnect ends up on
the right timeline after D4's reconstruction, measured on the decoded output; (3) VP8 with simulcast is captured at
the chosen layer, with keyframe recovery, and decodes; (4) the recorder reaches LiveKit's media from another
container in the Compose stack and inside the all-in-one image. Its findings are written into this change, and the
design is corrected before task group 1 starts. The gates are not equal: if the audio timeline (2) or the network
path (4) cannot be made to work, the change stops and is redesigned; only a failed video gate (3) shrinks the scope,
to audio only.

### D1. Capture in Go, compose with the `ffmpeg` binary

The recorder joins the room with the LiveKit Go SDK and writes each subscribed track to files with Pion's writers,
as received (no decoding). `ffmpeg`, run with `os/exec` and an argument list built by MeetHub, produces the final
file. Rejected: Go bindings to libav (cgo, the end of the static binary); a command-line wrapper library (it builds
the same argument list); pure Go only (no maintained Opus/VP8 decoder and encoder pair to mix or compose);
LiveKit Egress (a separate service with a headless Chrome — the later option for large installs, behind the same
API).

### D2. The recorder is a hidden participant, and only while it holds the lease

Its custom token (not the SDK's default join) has `RoomJoin` for that one room, `Hidden`, `Recorder`,
`CanSubscribe`, and publishing explicitly off; identity `recorder:<recording id>`. LiveKit does not list hidden
participants to the others. MeetHub's presence ignores `recorder:` identities in webhooks, reconciliation, counts
and eviction; a room with only the recorder is empty. The maximum duration is the recorder's own timer — a token's
lifetime does not end a connection.

Reconciliation removes from LiveKit any `recorder:` participant whose recording has no live lease (D3), so an
orphan can never keep listening.

### D3. One recording per room, owned through a renewable lease

`meeting_recordings` (migration 00006): `id`, `channel_id`, `room`, `started_by`, `mode`, `status` (`recording`,
`processing`, `interrupted`, `ready`, `failed`), `partial` and its reasons, `started_at`, `ended_at`, `duration_ms`, `volume`
(D6), `lease_until`, `generation`, `stop_requested_at`, `file_id`, `post_id`, `error`, timestamps. At most one row
per channel is `recording`: the start takes the channel's row lock, checks and inserts.

- **Lease**: the owner renews `lease_until` (30 s ahead) every 10 s with a conditional update on its `generation`.
  If a renewal fails for any reason — the database unreachable, the row no longer its own — the recorder stops
  capturing and leaves the room at once (fail closed). It never records on a lease it could not renew.
- **Stopping from any replica**: `DELETE` sets `stop_requested_at` and publishes a stop message on the Redis bus.
  The owner stops on the message, or at its next renewal (which reads the flag) if the bus is down — at most 10 s.
  The request answers `202`; the room is told "stopped" only when the owner has left the room and moved the row to
  `processing`. A row is never announced stopped because a flag changed.
- **Sweeper**: a periodic task (every 30 s, once across replicas, on the lifecycle queue of D9) takes every
  `recording` or `processing` row whose lease expired, bumps its `generation` (so a late owner's writes fail),
  removes the recorder participant through LiveKit's API, and moves the row to `interrupted`: the room stops
  showing "Recording", and the captures wait for their volume (D6).

*Alternative considered:* a queue task per recording on any replica — captures are local files, so composition must
run where they are; a queue adds nothing.

### D4. What is captured: tracks on one media clock

Every track gets a **manifest**, append-only JSON lines flushed and synced with the media: the codec, the RTP clock
rate, the mapping from RTP timestamps to the recording's clock, and every discontinuity. The mapping comes from the
track's RTCP sender reports (the sender's own wall clock against its RTP clock), which is what places different
people's tracks on one timeline; packets that arrive before a track's first report are kept and placed once it
comes. A track that never sends a report is placed by the arrival time of its packets, and the recording is marked
partial (`timing_approximate`): the 100 ms accuracy this change promises holds for tracks with sender reports.

- **Audio (Opus → Ogg)**: packets are written in RTP order. A gap in RTP time (DTX, loss, mute) is filled with
  silence frames so the file's timeline equals the RTP timeline; a gap longer than 2 s, an unmute after a mute, or
  a new publication ends the file and starts another with its own position on the clock.
- **Video (video mode; VP8 and VP9 → IVF with frame timestamps)**: the recorder subscribes cameras at the medium
  simulcast layer and screen shares at the highest, requests a keyframe when a track starts and after any loss,
  and drops frames until a keyframe arrives (if none comes within 5 s the gap is recorded). A change of resolution
  or layer starts a new file. The web app publishes VP8 (LiveKit's default, set explicitly). A track in a codec
  the recorder does not capture (H.264, AV1) is recorded as its audio and a name tile, and the recording is marked
  **partial** with the reason.

A person who leaves and returns, or mutes and unmutes, yields several files, each with its position. A torn last
manifest line after a crash is ignored; the media up to the last complete line is used.

The recording stops when: someone allowed stops it; nobody but the recorder has been in the room for 30 s; the
maximum duration (`recordings.max_minutes`, default 240) or the byte budget (D8) is reached; the channel is
archived; the lease cannot be renewed; the server shuts down (it stops capturing, syncs, and releases the lease;
composition happens on the next start, D6).

### D5. Composition: one audio master, video in bounded segments

- **Audio master** (both modes), in two bounded steps: first each participant's files are flattened into one
  continuous track of that participant, silence where they were muted or away, joining at most 50 files per
  `ffmpeg` run; then the participant tracks (at most 25) are mixed without automatic attenuation
  (`amix normalize=0`) and loudness-normalized into one continuous stream. The mix never has more inputs than
  participants, however often they muted.
- **Audio mode**: the master as AAC in `.m4a` (FFmpeg's own encoder). A meeting where nobody spoke yields a
  recording with silence, and its message says so.
- **Video mode**: 1280×720, 25 fps, `yuv420p`, VP8 + Opus in `.webm`. Layout changes only when a camera or a
  screen share starts or stops, and never more often than every 10 s: a screen share fills the frame (the first
  one started, if several) with up to four cameras in a strip; otherwise a grid of up to nine cameras in order of
  joining; a name tile where a camera is off or was not captured. Name tiles are PNG images drawn by MeetHub in Go
  with an embedded font, so `ffmpeg` needs no fonts. Each layout span is one segment: sources trimmed to the span,
  timestamps reset, identical encoding parameters, black or the name tile where a source ends early. At most 200
  segments; beyond that the remaining time uses one fixed layout. Segments are concatenated video-only and then
  muxed with the audio master, so stitches cannot drift the sound.
- Every encode has a bitrate ceiling (audio 128 kbit/s, video 2 Mbit/s), so the size of the output and of every
  intermediate is bounded by the recording's duration; D8 reserves that space.
- `ffmpeg` runs with two threads, lowered priority, a time limit (four times the duration, at least 10 minutes) and
  its output on the capture volume. On failure only an error class goes to the log; its error output stays in a
  file beside the captures for the operator, never in logs or messages.

`recordings.video_format = "mp4"` (H.264 + AAC) exists for operators who supply their own `ffmpeg` with `libx264`;
MeetHub's images cannot produce it (D10).

### D6. Durability and recovery need the capture volume

Captures go to `<recordings.dir>/<recording id>/` (default `<data_dir>/recordings`), synced as written. The
directory has a **volume identity**: a random id in a file created once at its root. Rows record the volume, not the
process, so a container that restarts with the same volume under a new name resumes its work.

On startup, and whenever its sweeper turn comes, a replica adopts the `interrupted` rows of its volume (and its own
rows whose lease expired): a capture cut short is closed at the last synced manifest line and is a recording up to
the cut, marked partial; an interrupted composition is done again from the captures. An `interrupted` row whose
volume has not come back after 24 hours is marked `failed` (`capture_lost`) and the channel is told — not before,
so a server that is merely restarting, or down for a night, loses nothing. A replica deletes a capture directory
only when its row is `ready`, or `failed`, or does not exist and the directory is older than 7 days; never one whose
row is `interrupted`. **A recording in progress survives only if the capture volume does**; the documentation says
so.

Shutdown does not compose: within the shutdown deadline the recorder stops, syncs and releases the lease.

### D7. Publishing the result: idempotent, by the server

When a recording starts, its `file_id` is fixed and stored in the row. Each composition attempt uploads to its own
storage key, which includes the attempt's `generation`, so a late upload of a superseded attempt can never
overwrite the object of the one that published. Publication is one database transaction, conditional on
`status = 'processing'` and the current `generation`: insert the file info pointing at this attempt's key, insert
the post with the deterministic key `recording:<id>`, attach the file, set the row `ready`. A crash before the
commit repeats the transaction; a crash after it finds the row `ready`. There is never a second post, and the
objects of attempts that did not publish are removed by the cleanup.

The post is made by a narrow server-side operation in `internal/chat` (`PublishSystemPost`), not by `CreatePost`:

- it is attributed to the person who started the recording, even if they have left the channel since;
- it does **not** mark the channel read for them, parse mentions, or count against rate limits;
- it carries typed metadata set only by the server (`props`: recording id, duration, mode, partial), which the
  post projection gains;
- it is allowed in an archived channel (archiving stops the recording, and the result still belongs there);
- failure posts use the same operation with type `recording_failed`.

**Who may record**: a caller who is in the room, is a current member able to read the channel, and has the
recording role — channel administrator, or a team administrator or `manage_system` holder *who is a member of the
channel*; in direct and group conversations any member; with `recordings.start = "members"` any member.

### D8. Limits and admission

- `recordings.max_concurrent` (default 2) captures per replica; a start beyond it answers `503 recordings_busy`.
- Each capture has a byte budget (`recordings.max_bytes`, default 4 GiB, counted as written); reaching it stops the
  recording. Composing needs a known amount too: the output and its intermediates are bounded by the bitrate
  ceilings of D5 times `recordings.max_minutes`, twice over (segments, then the final file). A start is admitted
  only if the volume's free space covers, for every recording that is capturing, waiting or composing and for this
  one, its capture budget plus that composition reserve.
- At most 25 tracks per recording; further tracks are not captured and the recording is marked partial.
- One composition at a time per replica, at most four waiting; a full backlog refuses new recordings.

### D9. Lifecycle work has its own workers

The sweeper and the retention task run on a queue with its own worker pool, separate from the pool that serves
long tasks, so a saturated server still ends stale recordings within its promise (two minutes).

### D10. `ffmpeg` in the images: an LGPL build, built from source

The images carry an `ffmpeg` built in a Dockerfile stage from the source tarball of a pinned release (checked by
SHA-256) with `--disable-gpl --disable-nonfree` and only what recordings use (Opus, Vorbis-free: `libopus`, `libvpx`,
the native AAC encoder, the filters and demuxers needed), linked statically, copied into the runtime and
all-in-one images. No GPL component is distributed. `NOTICE` and a `THIRD_PARTY` file carry the LGPL text, the
licenses of `libopus` and `libvpx`, the exact source URLs and checksums, and the configure line — the Dockerfile
stage is the build recipe.

With `recordings.enabled`, startup runs a **real probe**: a two-second generated capture is mixed, composed with a
name tile and encoded for the configured mode, as the server's user, writing to the capture directory. A failure is
a startup error that names what is missing. The same probe runs in CI inside both final images.

### D11. The recorder must reach LiveKit's media

The recorder connects to LiveKit's signalling at `livekit.api_url` and must reach the media addresses LiveKit
advertises. In the Compose stack that means LiveKit advertising, besides the host's address for browsers, one the
MeetHub container reaches; which settings achieve it is an output of the spike (D0), and a recording across the two
containers is part of CI. The all-in-one image has no LiveKit of its own today: recordings there work with an
external LiveKit that the container can reach, and with the bundled one once `add-meetings-turn-and-allinone` is
implemented. This change does not depend on that one; its all-in-one test uses an external LiveKit.

### D12. API and events

| Route | Who | Result |
| --- | --- | --- |
| `POST /api/v1/channels/{id}/meeting/recording` | may record (D7) | `201` the recording; `409 recording_active`; `403`; `503 recordings_unavailable` or `recordings_busy` |
| `DELETE /api/v1/channels/{id}/meeting/recording` | who started it or may record | `202`; `404` when none is running |
| `GET /api/v1/channels/{id}/meeting/recordings` | channel readers | recordings of the channel, newest first, paged |

The room snapshot gains `recording: {id, started_by, started_at, mode, stopping}` or `null`; starting, a stop
request and the actual stop each bump the room's revision and send `meeting_updated`.

## Prior art (looked at before the spike)

Projects that record WebRTC media with Go, with or without FFmpeg — what each one shows:

| Project | How it records | What it means here |
| --- | --- | --- |
| [CloudGroundControl/livekit-recorder](https://github.com/CloudGroundControl/livekit-recorder) (Go, Apache-2.0) | Bots join a LiveKit room through `server-sdk-go`, save each participant's RTP stream without transcoding, then use `ffmpeg` to put it in a container; start/stop by API or LiveKit webhooks; optional S3 upload | The same shape as this design (capture in Go, `ffmpeg` afterwards), proven in production by its authors, who later moved to Egress |
| [livekit/server-sdk-go `examples/filesaver`](https://github.com/livekit/server-sdk-go) (Go, official) | Saves a room's audio and video tracks to local disk with the SDK | The starting point of task 0.1 |
| [Pion `save-to-disk` and `save-to-webm`](https://pkg.go.dev/github.com/pion/webrtc/v3/examples/save-to-disk) (Go) | Writes VP8 and Opus to disk in pure Go; the WebM variant puts both in one timestamped file | A timestamped container written in Go is possible without FFmpeg |
| [Galène](https://galene.org/) `diskwriter` (Go, Pion) | A videoconference server that records to WebM/Matroska on disk in pure Go, with its own depacketizer and audio/video sync buffering | An alternative for D4 that the spike must weigh: one timestamped WebM per participant written in Go, instead of Ogg and IVF files with a manifest — `ffmpeg` would then only mix and compose |
| [bic4907/WebRTC-Broadcast-Recording](https://github.com/bic4907/WebRTC-Broadcast-Recording) (Go, Pion) | A small SFU that records broadcasters' tracks by calling the `ffmpeg` binary | The "pipe RTP to an `ffmpeg` process" variant; simpler capture, but one `ffmpeg` process per track while the meeting runs |
| [LiveKit Egress](https://docs.livekit.io/home/egress/overview/) (Go) | Room composites are rendered by a headless Chrome and encoded with GStreamer; single tracks are exported with the Go SDK | The reference product; heavier to run, and what D1 leaves as the later option |
| [MediaMTX](https://github.com/bluenviron/mediamtx) (Go) | A media server that records the streams published to it, natively | Shows recording in Go without FFmpeg for stream-per-file cases; it does not mix or compose |

Go libraries for FFmpeg, and why D1 uses neither:

- [u2takey/ffmpeg-go](https://github.com/u2takey/ffmpeg-go) — a port of ffmpeg-python: a fluent builder for the
  command line of the `ffmpeg` binary, which it still needs installed. It would add a dependency to build the
  argument lists this change builds itself.
- [asticode/go-astiav](https://github.com/asticode/go-astiav) — Go bindings to FFmpeg's libraries (cgo), tied to
  one FFmpeg version (n8.0 at the time of writing) with breaking changes between releases. In-process muxing and
  encoding, at the price of cgo and shared libraries: the end of MeetHub's static pure-Go binary.

## Risks / Trade-offs

- [The media timeline is wrong in cases the tests did not think of] → the spike measures decoded output under DTX,
  loss, variable frame rate and reconnects before anything else is built; audio-only is the fallback scope.
- [Captures live on one volume] → a volume lost with its replica loses the recording in progress; stated in the
  product and the documentation; the sweeper ends the "recording" state within two minutes and removes the
  recorder from the room.
- [Composing video is slow] → audio is the default; bounded segments; one composition at a time; a backlog limit.
- [WebM does not play in some older Safari versions] → audio mode (`.m4a`) plays everywhere; the download always
  works; operators who need MP4 supply their own `ffmpeg`.
- [People recorded without noticing] → the mark in the room and on the pre-join screen, an announcement at start
  and stop, audit events. This informs; it is not consent, and the documentation says which is which.
- [A recording that is partial looks complete] → `partial` with its reasons is part of the recording and is shown
  on its message.

## Migration Plan

Migration `00006` is additive. Recordings are off by default; an upgraded install changes nothing until
`recordings.enabled = true`. Rollback: `meethub migrate down` drops `meeting_recordings`; recordings already posted
stay as messages with files.

## Open Questions

- Opting out of being recorded while staying in the meeting (that person's tracks left out) is not in this change.
- Whether the Compose stack can make LiveKit advertise two addresses, or the recorder should use TURN/TCP — the
  spike decides.
