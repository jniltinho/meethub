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

### Requirement: ffmpeg with the Linux packages
The Linux packages SHALL carry no `ffmpeg` and SHALL use the distribution's: the `.deb` SHALL depend on it, the
`.rpm` SHALL recommend one without requiring it, and a package install SHALL compose both recording formats with
it, and MP4 where that `ffmpeg` has `libx264`.

#### Scenario: Installed from the .deb
- **WHEN** the `.deb` is installed on Debian or Ubuntu
- **THEN** the distribution's `ffmpeg` is installed with it and the recording probe passes for audio, WebM and MP4

#### Scenario: Installed from the .rpm
- **WHEN** the `.rpm` is installed on AlmaLinux with only its base repositories
- **THEN** it installs without an `ffmpeg`, and with EPEL's `ffmpeg-free` added the recording probe passes for
  audio and WebM

### Requirement: The recorder reaches LiveKit in the stack
In the Compose stack the MeetHub server SHALL be able to join a room's media as the recorder, and the deployment
guide SHALL state what the all-in-one image needs for the same (a LiveKit the container reaches).

#### Scenario: Recording across containers
- **GIVEN** the Compose stack with recordings on and a participant publishing audio
- **WHEN** a recording is started and stopped
- **THEN** the posted file contains that audio
