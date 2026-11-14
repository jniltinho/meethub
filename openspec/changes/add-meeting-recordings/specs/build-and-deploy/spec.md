# Spec Delta

## ADDED Requirements

### Requirement: ffmpeg in the images
The runtime image and the all-in-one image SHALL contain an `ffmpeg` executable built from pinned source without
GPL or non-free components, able to produce both recording formats as the container's user, and SHALL carry its
license texts, source locations and build configuration.

#### Scenario: Real recording in each image
- **WHEN** CI runs the recording probe (mix, compose with a name tile, encode) inside the runtime image and inside
  the all-in-one image as their unprivileged user
- **THEN** both produce a playable `.m4a` and a playable `.webm`

#### Scenario: No GPL components
- **WHEN** the image's `ffmpeg` prints its configuration
- **THEN** it shows neither `--enable-gpl` nor `--enable-nonfree`

### Requirement: The recorder reaches LiveKit in the stack
In the Compose stack the MeetHub server SHALL be able to join a room's media as the recorder, and the deployment
guide SHALL state what the all-in-one image needs for the same (a LiveKit the container reaches).

#### Scenario: Recording across containers
- **GIVEN** the Compose stack with recordings on and a participant publishing audio
- **WHEN** a recording is started and stopped
- **THEN** the posted file contains that audio
