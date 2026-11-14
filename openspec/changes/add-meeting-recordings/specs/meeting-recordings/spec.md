# Spec Delta

## Purpose

Lets a meeting be recorded and kept in its channel: who may record, what is captured, how a recording ends and where
it goes.

## ADDED Requirements

### Requirement: Starting a recording
`POST /api/v1/channels/{id}/meeting/recording` SHALL start a recording of the channel's room for a caller who is in
the room, is a member able to read the channel and holds the recording role. A room SHALL have at most one
recording running; with recordings off the route SHALL answer `503 recordings_unavailable`.

#### Scenario: Channel administrator records
- **GIVEN** recordings are on with the default settings and Ana, a channel administrator, is in the room of `#projects`
- **WHEN** she starts a recording
- **THEN** the answer is `201` with the recording, and every member of the channel receives the room with its recording state

#### Scenario: Member may not record by default
- **GIVEN** Bruno is a member of `#projects`, not an administrator, and is in its room
- **WHEN** he starts a recording
- **THEN** the answer is `403` with code `forbidden` and nothing is recorded

#### Scenario: Administrator who is not a member
- **GIVEN** a team administrator who is not a member of the private `#board`
- **WHEN** they start a recording of its room
- **THEN** the answer is `404` and nothing is recorded

#### Scenario: Already recording
- **GIVEN** a recording of the room is running
- **WHEN** another administrator starts one
- **THEN** the answer is `409` with code `recording_active`

#### Scenario: Conversation
- **GIVEN** a direct conversation between Ana and Bruno, both in its room
- **WHEN** Bruno starts a recording
- **THEN** the answer is `201`

### Requirement: Stopping a recording
`DELETE` on the same path SHALL ask the recording to stop, for whoever started it or may record, through any
replica, and answer `202`. The room SHALL be told the recording stopped only once the recorder has left the room.

#### Scenario: Stopped through another replica
- **GIVEN** replica A records the room and the stop request reaches replica B
- **WHEN** B answers `202`
- **THEN** A's recorder leaves the room within 15 seconds, and only then the members receive the room without a recording

### Requirement: Recording only on a live lease
The server recording a room SHALL hold a lease it renews; when it cannot renew it, it SHALL stop recording and leave
the room at once. A recording whose lease expired SHALL be ended within two minutes, its recorder removed from the
room, and the room told.

#### Scenario: Database unreachable
- **GIVEN** the recording server loses the database but still reaches LiveKit
- **WHEN** its lease renewal fails
- **THEN** its recorder leaves the room without waiting to be told

#### Scenario: Server lost
- **GIVEN** the server recording a room disappears
- **WHEN** two minutes pass
- **THEN** the room no longer shows as being recorded and no recorder is in it

### Requirement: What is recorded
A recording SHALL contain the audio of everyone who speaks in the room from its start to its end, each voice at the
moment it was spoken (within 100 ms for participants whose browser reports its clock), mixed on one timeline; in
video mode it SHALL also contain the shared screen and the cameras published in VP8 or VP9. A person who joins
after the start SHALL be recorded from their arrival.

#### Scenario: Two speakers
- **GIVEN** Ana and Bruno speak in turns during a recorded meeting
- **WHEN** the recording is played
- **THEN** both are heard in the order and at the moments they spoke, within 100 ms

#### Scenario: Silence between sentences
- **GIVEN** Ana speaks, is silent for 20 seconds with her microphone on, and speaks again
- **WHEN** the recording is played
- **THEN** 20 seconds separate her two sentences

#### Scenario: Late arrival
- **GIVEN** Carla joins ten minutes after the recording started
- **WHEN** the recording is played
- **THEN** her voice is heard from the tenth minute on

### Requirement: Partial recordings say so
A recording that could not capture everything — a camera in a codec that is not captured, more tracks than the
limit, a capture cut short, a voice placed only approximately in time — SHALL be marked partial with its reasons, in
the API and on its message.

#### Scenario: Camera not captured
- **GIVEN** a participant publishes a camera in a codec the recorder does not capture
- **WHEN** the recording is posted
- **THEN** it has that person's voice, a name tile in place of their camera, and says it is partial

### Requirement: How a recording ends
A recording SHALL end when it is stopped, when nobody has been in the room for 30 seconds, at
`recordings.max_minutes` or `recordings.max_bytes`, when its channel is archived, or when the server shuts down;
whatever was captured until then SHALL be kept.

#### Scenario: Everyone leaves
- **WHEN** the last person leaves a room that is being recorded
- **THEN** the recording ends within 30 seconds and what was captured is saved

#### Scenario: Maximum duration
- **GIVEN** `recordings.max_minutes = 60`
- **WHEN** a recording reaches one hour
- **THEN** it ends and the room is told the recording stopped

#### Scenario: Channel archived
- **WHEN** the channel of a room being recorded is archived
- **THEN** the recording ends and its result is still posted in that channel

### Requirement: The recording in the channel
A finished recording SHALL be posted once in its channel, attributed to the person who started it, with the
recording attached as a file (`.m4a` in audio mode, `.webm` in video mode) and its duration; it SHALL follow the
rules of any attachment. Posting it SHALL NOT mark the channel read for that person nor notify anyone by mention.

#### Scenario: Posted when ready
- **WHEN** a 42-minute recording of `#projects` finishes processing
- **THEN** `#projects` receives one message with the recording and "42 min", and a member plays it

#### Scenario: Posted exactly once
- **GIVEN** the server stops right after posting the recording, before recording that it did
- **WHEN** it starts again
- **THEN** the channel still has one message for that recording

#### Scenario: Starter's unread messages
- **GIVEN** Ana started the recording and has unread messages in `#projects` when it is posted
- **WHEN** the recording message appears
- **THEN** her unread count is not cleared by it

#### Scenario: Outside the channel
- **WHEN** someone who is not a member of `#projects` requests the recording's file
- **THEN** the answer is `404`

### Requirement: Recovery
A server that starts with a capture volume SHALL finish the recordings captured on it, whenever it returns within
24 hours; a recording whose capture volume has not returned by then, or that cannot be processed, SHALL be marked
failed and reported in the channel, whatever state it was in.

#### Scenario: Restarted while processing
- **GIVEN** a server stops while it turns a capture into a file
- **WHEN** it starts again with the same volume
- **THEN** it finishes the file and posts the recording once

#### Scenario: Server back after an hour
- **GIVEN** a server stops while recording and starts again an hour later with the same volume
- **WHEN** it starts
- **THEN** what was captured is posted as a partial recording

#### Scenario: Volume lost
- **GIVEN** a server and its volume are lost while processing a recording
- **WHEN** 24 hours pass
- **THEN** the recording is failed and the channel is told it could not be saved

### Requirement: Limits
A server SHALL refuse a new recording with `503 recordings_busy` when it already records
`recordings.max_concurrent` rooms, when its processing backlog is full, or when its capture volume cannot hold the
budgets of the recordings in progress and the new one.

#### Scenario: Volume nearly full
- **GIVEN** the capture volume has less free space than a recording's budget requires
- **WHEN** someone starts a recording
- **THEN** the answer is `503` with code `recordings_busy` and nothing is recorded

### Requirement: Recording list and retention
`GET /api/v1/channels/{id}/meeting/recordings` SHALL list the channel's recordings to its readers, newest first, with
who started each, when, its duration, state and whether it is partial. With `recordings.retention_days` above zero,
recordings older than that SHALL lose their file by a daily task, in archived channels too.

#### Scenario: Retention
- **GIVEN** `recordings.retention_days = 30` and a recording posted 31 days ago
- **WHEN** the daily task runs
- **THEN** its file is gone and its message says the recording expired
