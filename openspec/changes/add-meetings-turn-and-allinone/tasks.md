# Tasks

## 1. TURN

- [ ] 1.1 Enable LiveKit's TURN in the Compose stack (UDP, and TLS with a configured domain and certificate),
  generate its settings in `make init`, and document ports, DNS and the proxy; verify with the stack's LiveKit that
  a client forced to relay connects (LiveKit's own connection test or a headless browser with only relay candidates)

## 2. All-in-one

- [ ] 2.1 Add LiveKit to the `allinone` target (binary from its image pinned by digest), a supervisord program
  started before the server, and the generated key and secret in the volume, exported to both processes; verify
  the smoke test extended with: meetings enabled, the secret kept across a new container, no process as root
- [ ] 2.2 Document the `docker run` line with the extra ports and `LIVEKIT_NODE_IP`; verify the documented line
  on a clean host and a call between two browsers

## 3. Command

- [ ] 3.1 Implement `meethub meetings check`; verify against the test stack: all passing, a wrong secret, an
  unreachable server, and that no secret is printed

## 4. Validation

- [ ] 4.1 Run `make check`, `make test`, `make test-e2e`, `make test-allinone`, `make build` and CI green

## Workflow follow-up

- Archive with `openspec archive add-meetings-turn-and-allinone` after review.
