# Spec Delta

## ADDED Requirements

### Requirement: TURN in the Compose stack
The Compose stack SHALL run LiveKit with its TURN server enabled over UDP and, when a TLS domain is configured,
over TLS, with the TURN settings created by `make init`; the deployment guide SHALL list the ports and DNS names a
public install needs.

#### Scenario: Blocked UDP
- **GIVEN** a browser on a network that allows only outgoing TCP 443
- **WHEN** it joins a meeting of a public install set up as the guide says
- **THEN** audio and video connect through TURN over TLS

### Requirement: LiveKit in the all-in-one image
The all-in-one image SHALL run a LiveKit server beside the MeetHub server and Redis, with an API key and secret
generated on the first start and kept in the data volume (readable only by the container's user), meetings enabled,
and the media ports 7881/tcp and 7882/udp exposed.

#### Scenario: Meetings in the first minute
- **WHEN** the image is started on a host with ports 8080, 7880, 7881/tcp and 7882/udp published
- **THEN** `/api/v1/meetings` reports meetings enabled and two browsers on that host can meet

#### Scenario: Secret kept
- **WHEN** the container is deleted and created again on the same volume
- **THEN** it uses the same LiveKit key and secret

#### Scenario: Never as root
- **WHEN** the all-in-one container runs
- **THEN** LiveKit, like every other process in it, runs as the container's unprivileged user
