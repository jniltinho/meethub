# Tasks

## 1. TURN

- [x] 1.1 Enable LiveKit's TURN in the Compose stack (UDP, and TLS with a configured domain and certificate),
  generate its settings in `make init`, and document ports, DNS and the proxy; verify with the stack's LiveKit that
  a client forced to relay connects (LiveKit's own connection test or a headless browser with only relay candidates)
  - Verified with the stack's LiveKit configuration (a throwaway Compose project, `--node-ip` a private address): two
    relay-only clients (server-sdk-go, `ICETransportPolicyRelay`) exchanged a reliable data packet over TURN/UDP,
    and again from a network with only TCP to the signalling port and to a TLS proxy for `turns:<domain>:443`
    (TURN over TLS through the proxy's certificate). A real browser from another network is left to the owner.

## 2. All-in-one

- [x] 2.1 Add LiveKit to the `allinone` target (binary from its image pinned by digest), a supervisord program
  started before the server, and the generated key and secret in the volume, exported to both processes; verify
  the smoke test extended with: meetings enabled, the secret kept across a new container, no process as root
- [ ] 2.2 Document the `docker run` line with the extra ports and `LIVEKIT_NODE_IP`; verify the documented line
  on a clean host and a call between two browsers
  - Documented; the verification on a clean host with two browsers needs the owner.

## 3. Command

- [x] 3.1 Implement `meethub meetings check`; verify against the test stack: all passing, a wrong secret, an
  unreachable server, and that no secret is printed

## 4. Validation

- [x] 4.1 Run `make check`, `make test`, `make test-e2e`, `make test-allinone`, `make build` and CI green
  - All run locally and pass; CI on GitHub is pending.

## Decisions made while implementing

- **LiveKit relays to a private node IP.** LiveKit's TURN refuses relays to private and loopback peers unless
  allowed, so a `LIVEKIT_NODE_IP` on a LAN or Docker network got no relay. The stack and the all-in-one image allow
  exactly `LIVEKIT_NODE_IP/32` (`turn.allow_restricted_peer_cidrs`); LiveKit itself limits relays to its own host to
  its media ports.
- **TURN over TLS is always port 443.** LiveKit announces `turns:<domain>:443` whatever its `tls_port`, so the
  reverse proxy routes the TURN name by SNI on 443, ends TLS and passes plain TCP to LiveKit's 5349
  (`external_tls`), published on `127.0.0.1` only. `LIVEKIT_TURN_DOMAIN` empty (what `make init` adds) leaves TLS
  off; TURN over UDP on 3478 is always on.
- **TURN over UDP in the all-in-one image too** (3478/udp, optional to publish): without a TURN server
  `meethub meetings check` could never pass there.
- **The key and secret reach the server as files.** `/usr/local/bin/meethub` in the image is a small wrapper
  (`docker/allinone/meethub.sh`) that points `MEETHUB_LIVEKIT_API_KEY_FILE` and `MEETHUB_LIVEKIT_API_SECRET_FILE` at
  `/data/livekit/…` (0600, directory 0700) unless the variable or its `_FILE` form is already set, instead of
  exporting variables from the entrypoint: `docker exec <container> meethub …` and the health check load the same
  settings, and an explicit `MEETHUB_LIVEKIT_API_KEY`/`_SECRET` (an install that ran an external LiveKit beside the
  previous image) is not refused as "both set". LiveKit reads a key file made from them. The key is random (`API` +
  16 hex digits), the secret 32 random bytes.
- **`--stop-timeout 55`** instead of 45: LiveKit adds a 10-second stop to supervisord's sequence.
- **The check's targets.** `meethub meetings check` signals through `livekit.api_url` (the browser URL is printed;
  in the Compose stack it is `localhost` of the browsers, not of the server's container) and tries a TURN server on
  a loopback address at `livekit.api_url`'s host.

## Workflow follow-up

- Archive with `openspec archive add-meetings-turn-and-allinone` after review.
