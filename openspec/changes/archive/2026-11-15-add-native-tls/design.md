# Design

## Context

`httpserver.Run` starts Echo's `StartConfig`, which wraps the listener with TLS when `TLSConfig` is set. Cookies,
HSTS and the WebSocket origin check already follow `http.public_url`, and `requestScheme` already answers `https`
for a request that arrived over TLS. ad-manager, whose conventions this project follows, has `cert_file`,
`key_file` and `self_signed` with a generated certificate kept in its data directory.

## Decisions

### D1. The same three settings as ad-manager

`http.cert_file`, `http.key_file`, `http.self_signed`. A configured pair wins over `self_signed`. A signed and a
self-signed certificate brought by the install take the same path: the server never checks who signed a chain,
browsers do.

### D2. The generated certificate is kept

ECDSA P-256, 825 days (the longest Apple's platforms accept), for the public URL's host, `localhost`, the
loopback addresses, the machine's name and its interface addresses. It lives in `<data_dir>/tls` (directory
0700, files 0600, enforced at every start) and is reused, so a device that trusted it keeps trusting it; it is
replaced, at a start, only when it no longer names the public host, is not valid yet or has under 30 days left.
Starts take turns on a lock file and each file is written through a rename, so the pair is never half-written;
a stop between the two renames leaves a pair that does not match, which the next start replaces. It is not an
authority (`IsCA` false): a leaked key cannot sign certificates for other names on the devices that trusted it,
at the price that Android will not install it. The fingerprint is logged at every start, and a warning when a
new one was generated.

### D3. TLS requires an https:// public URL

Cookies become `Secure` and HSTS is sent from the public URL. A server that speaks TLS with an `http://` public
URL would set cookies browsers send over plain HTTP and refuse its own WebSocket origin, so validation rejects it.

### D4. The health probe trusts the certificate file, not the system store

The probe connects to the loopback, which a signed certificate does not name, and a self-signed one is in no
trust store. It takes the first certificate of the server's file as its only root, verifies under the public
URL's host and then requires the peer's certificate to be that very one: strict verification with no "skip
verify", and any other certificate on the port — also one of the same authority for the same name — fails the
probe.

### D5. No reload, no ACME, no redirect listener

A renewed certificate takes effect on restart; the document gives certbot a deploy hook that copies the files and
restarts the service. A redirect from port 80 and several names on port 443 are a reverse proxy's job
(`docs/nginx.md`).

### D6. Port 443 is a drop-in, not the default

The unit keeps an empty capability set. Listening below 1024 as the `meethub` user needs
`CAP_NET_BIND_SERVICE`; the document gives the two-line drop-in and uses 8443 otherwise.

## Risks

- A self-signed certificate must be trusted on every device, or browsers warn at each visit and some refuse the
  camera and microphone. The document says so and recommends a signed certificate for anything beyond a first look.
- Meetings on an HTTPS page need `wss://` signalling, which LiveKit does not end itself: with native TLS the chat
  and the files work alone, the meetings still need a proxy or a load balancer for LiveKit.
- Verified under systemd on Debian 13 in a container: self-signed on 8443 and a chain signed by a test authority on
  443 (ready, login, WebSocket, HTTP/2, health probe, the drop-in). Not verified: a certificate from a public
  authority, a browser on a phone, the certbot hook.
