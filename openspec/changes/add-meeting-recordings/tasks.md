# Tasks

Dependency order: 0 → 1 → 2 → 3 → 4 → 5. Group 0 is a gate: nothing after it starts until its findings are written
into the design. Every Go task runs its tests with `-race` on SQLite, MariaDB and PostgreSQL (`make test-db`) unless
it says otherwise.

## 0. Feasibility spike (throwaway code, findings kept)

- [x] 0.1 With the pinned SDK and Pion versions, join the test stack's LiveKit as a hidden subscribe-only
  participant with a custom token and receive the tracks of two test publishers and of Chrome and Firefox; verify
  the others do not see it and record which grants were needed
- [x] 0.2 Capture Opus under DTX, mute and unmute, 5% packet loss and a reconnect, rebuild the timeline as design
  D4 says, and measure on the decoded output; verify every spoken marker lands within 100 ms of its real time
  (compare the design's Ogg files with a manifest against one timestamped WebM per participant written in Go, as
  Galène's `diskwriter` and Pion's `save-to-webm` do, and keep the one that measures better)
- [x] 0.3 Capture simulcast VP8 at the medium layer (a ceiling) with keyframe requests, through loss and a
  resolution change, and decode it with the image's `ffmpeg`; verify no undecodable span longer than the bounded keyframe wait
- [x] 0.4 Make the recorder reach LiveKit's media from the MeetHub container of the Compose stack and inside the
  all-in-one image; verify with a recording in each and record the LiveKit settings used (the spike reproduced
  both topologies with the stack's LiveKit settings; then TestServerRecording recorded through the server in the
  real Compose stack and the all-in-one image — design D11)
- [x] 0.5 Write the findings into `design.md`. A failed 0.1, 0.2 or 0.4 stops the change for a redesign; only a
  failed 0.3 shrinks the scope to audio only; verify by a Codex review of the corrected design

## 1. Settings, schema, ffmpeg

- [x] 1.1 Add the `recordings` settings with validation and document them in `config.toml.example`; verify
  table-driven config tests
- [x] 1.2 Write migration 00011 (the assigned number; 00009 and 00010 belong to another branch) for the three
  dialects (`meeting_recordings` with its indexes) and its down step;
  verify up/down/up on the three dialects
- [x] 1.3 Build the LGPL `ffmpeg` in a Dockerfile stage from pinned, checksummed source and copy it into both
  images and the Linux packages (`/opt/meethub/bin/ffmpeg`, the packages' `config.toml` pointing at it), with
  `NOTICE`/`THIRD_PARTY` and the licence texts and build record beside the binary; verify its configuration has no
  GPL flag, that it is statically linked, and the package smoke test
- [x] 1.4 Implement the `ffmpeg` runner (argument lists, two threads, lowered priority, time limit, error class
  only in logs) and the startup probe; verify unit tests of the argument lists, the startup errors, and the probe as
  the unprivileged user inside both final images in CI

## 2. Lease, capture, presence

- [x] 2.1 Implement the lease (conditional renewal on the generation, fail closed), the stop request through the
  database and the bus, and the sweeper on its own worker pool; verify with two test replicas: stop through the
  other replica, a database-only partition of the owner, a lost owner, and the sweeper's latency with the main pool
  saturated
- [x] 2.2 Implement the recorder per the spike: custom token, manifests, audio with rebuilt silence, video with
  layer and keyframe policy, partial marking, stop conditions, byte budget; verify decoded-output timing under DTX,
  loss, variable frame rate and a reconnect
- [x] 2.3 Exclude `recorder:` identities from room presence and remove recorders without a live lease in
  reconciliation; verify the `meetings` scenarios and an orphan recorder removed
- [x] 2.4 Implement admission (concurrent captures, free-space accounting, track limit, backlog); verify the
  `Limits` scenarios and several recordings filling a small test volume

## 3. Composition, publication, recovery

- [x] 3.1 Compose the audio master (each participant flattened in batches of at most 50 files, then at most 25
  tracks mixed) and audio mode; verify positions and duration with generated tones, a meeting with no speech, and
  25 participants muting every 30 seconds for four hours
- [x] 3.2 Draw name tiles in Go and compose video mode (layout rules, dwell time, segment cap, video-only concat,
  mux with the audio master); verify frame size, pixel format, duration, a layout change, a camera ending early,
  201 layout changes, and audio/video sync at the last stitch
- [x] 3.3 Add `PublishSystemPost` with typed `props` in the post projection and publish the recording in one
  conditional transaction with deterministic keys; verify posted once across a crash at each step, the starter's
  unread state untouched, no mention processing, an archived channel, a starter who left, access by a non-member
- [x] 3.4 Recover by volume identity on startup and in the sweeper (`interrupted`, adopt, resume, torn manifest
  line, `capture_lost` after 24 hours, attempt keys and their cleanup); verify a process killed while capturing and
  while composing, a new container on the same volume, a volume that returns after an hour, a lost volume, and a
  superseded attempt uploading late

## 4. API, events, audit, retention

- [x] 4.1 Add the start, stop and list routes with the permission rule and the error codes, `recording` in the room
  snapshot and `meeting_updated`, and the audit events; verify the `meeting-recordings`, `realtime-events` and
  `audit-log` scenarios and the route documentation test
- [x] 4.2 Add the retention task on the lifecycle pool; verify the retention scenario in an archived channel too

## 5. Web app, docs, validation

- [x] 5.1 Add the record control with its confirmation, the "Recording" mark and timer (and "stopping"), the
  announcements and the pre-join notice; publish VP8 explicitly; verify Playwright with two browsers and the
  accessibility checks
- [x] 5.2 Add the recording player in messages (partial, failed and expired states) and the recordings of the
  channel details; verify Playwright: play, keyboard, download, each state
- [x] 5.3 Add the pt-BR and es strings, contrast recipes and gallery entries; document the API, the settings, the
  capture volume, disk and CPU needs, formats and browsers, and what participants are told (informing is not consent)
- [ ] 5.4 Run `make check`, `make test`, `make test-db`, `make test-e2e`, `make test-load`, `make build`, a Codex
  review of the implementation with its findings fixed, and CI green (all run locally and green, the Codex review's
  findings fixed; CI on GitHub needs a push, which is the owner's)

## Workflow follow-up

- Archive with `openspec archive add-meeting-recordings` after review.
- `add-meeting-summaries` builds on the per-speaker captures of this change.
