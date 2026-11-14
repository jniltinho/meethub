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

**The spike passed the gates of the audio timeline, the hidden recorder and video; the network path is feasible in
the reproduced topologies, its acceptance in the real images pending (task 0.4). Its results are in "Spike results
(D0)" below, and D4, D8, D10 and D11 are corrected by them and by the review of that correction.**

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

`meeting_recordings` (migration 00011 — the number assigned so that parallel branches do not clash; 00009 and
00010 belong to another branch): `id`, `channel_id`, `room`, `started_by`, `mode`, `status` (`recording`,
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
  a new publication ends the file and starts another with its own position on the clock. The exact rules, as
  measured in the spike (D0, candidate A2) and required:
  - RTP time is extended to 64 bits per subscription (one SSRC), so a wrap is never a jump; packets pass a
    reordering buffer bounded in time (Pion's sample builder) before these rules.
  - `gap = next.timestamp − (previous.timestamp + previous duration)`, the duration read from the Opus packet's
    TOC byte. A packet whose timestamp is at or before the previous packet's (a duplicate or a late copy) is
    dropped. A gap that is not a whole number of 960-sample (20 ms) silence frames, more than 2 ms off, starts a
    new file — LiveKit's start-of-track padding produced one of 222 samples, which filling would have shifted the
    rest of the file by 15 ms.
  - A file is re-anchored (a new file) when its own clock — its start plus the samples written — and the
    sender-report clock part by more than 40 ms.
  - A file's position is the median, over its packets, of the sender-report time minus the media written before
    the packet; it is updated in the manifest as reports come (the first packets may precede the first report).
  - Overlap: when a later file of the same track starts before an earlier one ends (a backward re-anchor), the
    later one owns the overlap and the earlier is trimmed to it; positions are clamped to the recording.
  - Pion's Ogg writer declares 3840 samples of pre-skip, which the decoder drops: every file starts with four
    20 ms silence frames (`f8 ff fe`); without them each file played 80 ms early.
  - **Clock domain.** LiveKit's down-track sender reports carry the SFU's wall clock (measured: the report's time
    equals its arrival within 0.3 ms on one host). On separate hosts that clock is offset from the recorder's,
    so a track's report time is converted to the recorder's clock by the smallest `arrival − report time` seen on
    that track (the offset plus the one-way delay, which is the same for every track of one SFU). Tracks keep
    their relative timing whatever the offset; a deliberately offset clock is part of the recorder's unit tests.
- **Video (video mode; VP8 and VP9 → IVF with frame timestamps)**: the recorder subscribes cameras at the medium
  simulcast layer and screen shares at the highest, requests a keyframe when a track starts and after any loss,
  and drops frames until a keyframe arrives (if none comes within 5 s the gap is recorded). A change of resolution
  or layer starts a new file. Measured (D0): the subscribed layer is a ceiling, not a fixed size — LiveKit starts a
  subscription low and ramps up, and follows the publisher's own size (320×180, 480×270 and 640×360 arrived in one
  track), and `ffmpeg` decoded those size changes inside one IVF without an error, so a size change does **not**
  start a new file (the composition scales every frame). Frames are assembled with Pion's sample builder (LiveKit's
  packet-count-bounded builder stalled for many seconds on one unrecovered loss at low bitrates); its time bound is
  a span of RTP time, not a wall-clock deadline, so the recorder also drains it when a track stops sending for a
  second and when the track ends. Frames are written with their RTP timestamps; keyframe requests are repeated
  every 500 ms while the recorder waits for one. VP9 uses Pion's VP9 depacketizer and the same rules; the spike
  measured VP8 only (the web app publishes VP8), so VP9 capture is covered by the recorder's tests, not by D0. The web app publishes VP8 (LiveKit's default, set explicitly). A track in a codec
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
  recording. Composing needs a known amount too, at its peak, all bounded by `recordings.max_minutes` (T): while
  mixing, one flattened track per participant (Opus at 64 kbit/s, at most 25, plus one batch of a participant
  being flattened) and the master being written (128 kbit/s) — about 1.8 Mbit/s × T; in video mode, afterwards,
  the segments, their concatenation and the final file (2 Mbit/s each, never all three: segments are removed once
  concatenated) with the master — about 4.3 Mbit/s × T. Intermediates are removed as soon as the next step has
  consumed them. A start is admitted only if the volume's free space covers, for every recording that is
  capturing, waiting or composing and for this one, its capture budget plus that composition reserve (at the
  defaults: 4 GiB + 3.3 GB in audio mode, 4 GiB + 7.8 GB in video mode).
- At most 25 tracks per recording; further tracks are not captured and the recording is marked partial.
- One composition at a time per replica, at most four waiting; a full backlog refuses new recordings.

### D9. Lifecycle work has its own workers

The sweeper and the retention task run on a queue with its own worker pool, separate from the pool that serves
long tasks, so a saturated server still ends stale recordings within its promise (two minutes).

### D10. `ffmpeg` in the images: an LGPL build, built from source

The images carry an `ffmpeg` built in a Dockerfile stage from the source tarball of a pinned release (checked by
SHA-256) with `--disable-gpl --disable-nonfree` and only what recordings use (Opus, Vorbis-free: `libopus`, `libvpx`,
the native AAC encoder, the filters and demuxers needed), linked statically, copied into the runtime and
all-in-one images. The spike built it: FFmpeg 9.0.2, libopus 1.6.1 and libvpx 1.17.0 from their release tarballs,
plus Alpine's static zlib (zlib licence) for the PNG name tiles; the static binary is about 10 MB. `lavfi` sources
need their raw PCM decoders (`pcm_u8`, `pcm_f64le`, …) and `wrapped_avframe` enabled, or generated silence and
colour fail to open. The Linux packages carry the same binary as `/opt/meethub/bin/ffmpeg` (their `config.toml`
points `recordings.ffmpeg_path` at it), so a package install records without a distribution `ffmpeg`. No GPL component is distributed. `NOTICE` and a `THIRD_PARTY` file carry the LGPL text, the
licenses of `libopus` and `libvpx`, the exact source URLs and checksums, and the configure line — the Dockerfile
stage is the build recipe.

With `recordings.enabled`, startup runs a **real probe**: a two-second generated capture is mixed, composed with a
name tile and encoded for the configured mode, as the server's user, writing to the capture directory. A failure is
a startup error that names what is missing. The same probe runs in CI inside both final images.

### D11. The recorder must reach LiveKit's media

The recorder connects to LiveKit's signalling at `livekit.api_url` and must reach the media addresses LiveKit
advertises. Measured (D0): **in the tested topologies no LiveKit setting needs to change.** With the Compose stack's own configuration
(`--node-ip 127.0.0.1`, which only a browser on the host reaches), a recorder in another container of the same
network connects anyway: LiveKit is a full ICE agent, its connectivity checks reach the recorder's host candidate,
and the recorder learns LiveKit's real address from them (a peer-reflexive candidate). The same held for a recorder
in LiveKit's network namespace — the all-in-one image, which now bundles LiveKit — with the node IP set to an
address the container cannot reach. `rtc.advertise_internal_ip: true` (LiveKit then also advertises its container
address) works too and is the documented fallback for a LiveKit that is ICE-lite or behind a firewall that drops
its checks — when that address is routable from the server. The prerequisite of the default path is that
LiveKit's connectivity checks reach the server's own address (the same Docker network, or one LAN); across NAT,
blocked UDP or container network policies an advertised, routable address or LiveKit's TCP port is needed, and
`meethub meetings check` gains a recorder join that says whether media flows. The recorder uses the TURN servers
LiveKit hands out like any client; the bundled one is at the node IP, which a container cannot reach.

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

## Spike results (D0)

Measured on 2026-11-14 with the pinned versions (LiveKit server 1.13.10, `server-sdk-go` 2.19.0, Pion WebRTC
4.2.23, `livekit-client` 2.23.0, Playwright 1.65.0's Chromium and Firefox) and the `ffmpeg` of D10, in a Compose
project of its own (no published ports), on an otherwise idle host (load average 0.4–0.8). The spike program was
throwaway and is not in the repository.

**Method.** A recorder joined as in D2 and dumped every RTP packet (arrival time, sequence, timestamp, payload),
every RTCP sender report and every mute event; each candidate reconstruction was then built from the same dumps,
flattened per participant and mixed by `ffmpeg` (as in D5), decoded to PCM, and searched for 200 ms tone markers
(Goertzel) whose send times the publishers logged. Error = decoded position − real time; the beep's end is used
instead of its start when its first frames were lost. Every marker was accounted for: all sent while the recorder
was connected were found (Go 11 of 11, Chrome 22 of 22, Firefox 22 of 22), and in the outage runs only those sent
while the recorder had no network were missing. Three Firefox beeps per run — the first after joining, after
unmuting and after the reconnect — lost their first second at the source (identically for every candidate, so
before the recorder): they are placed by their end, within 31 ms. (The evaluator matched markers within ±300 ms;
re-run with ±2.5 s it found no further marker.) Publishers: two Go SDK publishers (DTX gaps, mutes with and
without the RTP clock advancing, a full reconnect), and Chrome and Firefox through `livekit-client` with the web
app's room options (WebAudio tones in Chrome; Firefox's fake microphone switched on per marker, because its
AudioContext cannot run without an audio device in a container), each with a VP8 simulcast canvas camera, silence
(DTX) for 8 s, a mute of 5 s, a resolution change and a reconnect. Loss: 5 % on the publishers' uplink (netem),
5 % on the recorder's input (iptables, before WebRTC) or dropped after WebRTC in the recorder (no retransmission
possible); recorder outages of 4, 12 and 35 s (the last one forced the SDK to rejoin and resubscribe).

**Candidates** (D4, task 0.2): A — Ogg files with a manifest exactly as D4 was written; A2 — the same with the
rules now in D4; B — one timestamped WebM per publication written in Go, timestamps from the first packet's
arrival plus RTP time (Galène's `diskwriter`, Pion's `save-to-webm`); B-SR — the same WebM with sender-report
timestamps; C — LiveKit's `pkg/synchronizer` (the SDK's Egress synchronizer). Worst error per run, in ms:

| Run | A | **A2** | B | B-SR | C |
| --- | --- | --- | --- | --- | --- |
| Go, clean (2 publishers) | 21.7 | **6.7** | 6.7 | 6.7 | 6.7 |
| Go, 5 % up + 5 % after WebRTC | 27.3 | **27.1** | 18.2 | 18.2 | 18.2 |
| Go, recorder outage 4 s + loss | 21.4 | **17.8** | 11.2 | 26.4 | 11.2 |
| Go, recorder outage 35 s (rejoin) | 21.3 | **7.4** | 7.4 | 6.3 | 7.4 |
| Chrome, clean | 32.9 | **32.9** | 35.4 | 30.4 | 35.4 |
| Chrome, 5 % + 5 % network loss | 51.9 | **39.4** | 34.4 | 44.4 | 41.9 |
| Chrome, 5 % up + 5 % after WebRTC | 55.1 | **40.1** | 35.1 | 37.6 | 35.1 |
| Firefox, clean | 63.4 | **40.9** | 94.9 | 40.9 | 40.9 |
| Firefox, 5 % + 5 % network loss | 61.5 | **35.0** | 114.0 | 34.5 | 35.0 |
| Firefox, 5 % up + 5 % after WebRTC | 59.9 | **41.6** | 94.9 | 35.0 | 34.9 |

(Repeated runs agree within a few ms; the browsers' constant 20–35 ms is their capture and encoding latency.)

**Kept: A2 — Ogg with a manifest and sender-report placement.** Arrival-based placement (B) fails the 100 ms
promise with Firefox (114 ms): RTP time drifts against wall time and only the sender reports correct it. With the
same sender-report clock the two containers measure the same within the detector's resolution (worst case A2
41.6 ms, B-SR 44.4 ms); Ogg is kept because it needs no new Go module (WebM would add `at-wat/ebml-go`), its
pages make a torn tail harmless, and its timeline is explicit, whereas the WebM path relies on `ffmpeg`'s
`aresample` to insert silence at timestamp gaps (with the default 100 ms threshold short losses compressed the
timeline by up to 95 ms; `min_hard_comp=0.01` was needed). LiveKit's synchronizer measured like B-SR and would
add the `media-sdk` module. LiveKit's down-track sender reports come every 3 s and carry the SFU's own wall clock.

**Gates.**

1. *Hidden subscribe-only recorder* (0.1) — **passed.** Grants: `RoomJoin` for the one room, `Hidden`,
   `Recorder`, `CanSubscribe: true`, `CanPublish: false`, `CanPublishData: false`, joined with
   `ConnectToRoomWithToken` and auto-subscribe. It received every track of the Go publishers, Chrome and Firefox;
   neither browser listed it among its remote participants. LiveKit's room API does list it (permission
   `hidden: true`), so MeetHub's presence must drop `recorder:` identities, as D2 says.
2. *Audio timeline* (0.2) — **passed**: every received marker within 41.6 ms with A2, under DTX, mute and unmute
   (LiveKit's JS mute; the Go SDK's mute with and without RTP advancing), 5 % loss on either side, recorder
   outages and a publisher reconnect. Audio sent while the recorder had no network is missing, as expected. Not
   measured, and covered by the recorder's unit tests instead: LiveKit and the recorder on hosts with offset
   clocks (one host here), and runs longer than a minute.
3. *Video* (0.3) — **passed**: VP8 simulcast from both browsers at the medium layer, through a resolution change
   and loss, decoded by the image's `ffmpeg` with no errors. With 1 % loss after WebRTC (unrecoverable), the
   longest wait for a keyframe after a loss was 1.55 s (bound: 5 s); with 5 % network loss retransmission
   recovered everything.
4. *Network path* (0.4) — **feasible** with the stack's own LiveKit settings (D11): a recorder in another
   container of the Compose network, LiveKit with `--node-ip 127.0.0.1`; and a recorder in LiveKit's network
   namespace (the all-in-one layout) with an unreachable node IP; also with `rtc.advertise_internal_ip: true`.
   These reproduced the two layouts with the same LiveKit configuration, not the final images: a recording in
   the real Compose stack (with its `--node-ip 127.0.0.1`, which the e2e stack removes) and in the real all-in-one
   image is still required to close task 0.4.

## Implementation notes

What the implementation settled where the decisions above left room, or where it differs from the proposal:

- **One new Go module: `golang.org/x/image`** (BSD-3-Clause, the Go project's) — the name tiles of D5 need an
  embedded font and a rasterizer, which the standard library lacks; the tiles use its Go font. Everything else comes
  from modules already required (the LiveKit SDK brings Pion's writers and sample builder).
- **Layout dwell (D5):** a camera or screen that starts or stops changes the layout once the current layout has
  lasted 10 s, with the sources active then; past 200 segments the rest of the recording keeps the layout of the
  200th (a source that starts later in it is not shown).
- **Stops between servers (D3):** the Redis channel `meethub:recordings:stop`; the owner also sees the flag at its
  next renewal.
- **Lifecycle workers (D9):** a second Asynq server on the queue `meethub:lifecycle` with two workers runs the
  sweeper (every 30 s, claimed per period like the other periodic tasks) and the daily retention; adoption of the
  volume's own recordings and the cleanup of capture directories run on every server every 30 s.
- **Admission (D8):** every recording a server owns (capturing, waiting or composing) counts its full capture
  budget and composition reserve against the volume's free space.
- **API (D12):** `GET /channels/{id}/meeting` also says `can_record` for the caller (the web app shows the control
  from it); starting outside the room answers `403 not_in_meeting`.
- **Server-made posts (D7):** cannot be edited, by the API or the web app.
- **Video positions (D4):** a video file is placed by the sender-report time of its first frame (the median rule is
  for audio, whose files are long).
- `meethub recordings probe [--mode audio|video|all]` runs the startup probe on demand; the all-in-one smoke test
  and the package smoke test run it in both modes as the unprivileged user, and the e2e stack (the runtime image,
  recordings on) runs it at every start.
- The test stack (`compose.test.yml`) has a LiveKit, so `make test` runs the recorder against a real one.

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

Migration `00011` is additive. Recordings are off by default; an upgraded install changes nothing until
`recordings.enabled = true`. Rollback: `meethub migrate down` drops `meeting_recordings`; recordings already posted
stay as messages with files.

## Open Questions

- Opting out of being recorded while staying in the meeting (that person's tracks left out) is not in this change.
- ~~Whether the Compose stack can make LiveKit advertise two addresses, or the recorder should use TURN/TCP~~ —
  decided by the spike: neither is needed (D11).
