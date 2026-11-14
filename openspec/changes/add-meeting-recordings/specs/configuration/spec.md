# Spec Delta

## ADDED Requirements

### Requirement: Recording settings
The configuration SHALL have a `recordings` section: `enabled` (default false), `mode` (`audio` or `video`),
`video_format`, `start` (`admins` or `members`), `max_minutes`, `max_bytes`, `max_concurrent`, `retention_days`,
`ffmpeg_path` and `dir`. With `enabled`, validation SHALL require LiveKit to be configured.

#### Scenario: Off by default
- **WHEN** the server starts without any `recordings` setting
- **THEN** it starts without looking for `ffmpeg` and the recording routes answer `503`

#### Scenario: Without LiveKit
- **WHEN** `recordings.enabled = true` and LiveKit is not configured
- **THEN** startup fails naming `recordings.enabled`

### Requirement: Recording probe at startup
With recordings on, startup SHALL run a short real recording through `ffmpeg` in the configured mode and fail,
naming what is missing, when it cannot be produced.

#### Scenario: No ffmpeg
- **GIVEN** `recordings.enabled = true` on a host without `ffmpeg`
- **WHEN** the server starts
- **THEN** it exits with an error naming `recordings.ffmpeg_path`

#### Scenario: MP4 without an encoder
- **GIVEN** `recordings.video_format = "mp4"` and an `ffmpeg` without an H.264 encoder
- **WHEN** the server starts
- **THEN** it exits with an error naming the missing encoder
