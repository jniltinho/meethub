## ADDED Requirements

### Requirement: Native HTTPS
With a certificate configured or `http.self_signed` on, the server SHALL speak HTTPS on `http.addr` with TLS 1.2
or later and HTTP/2, serving the whole configured chain, and SHALL treat those requests as secure without any
proxy header. Without them it SHALL speak plain HTTP as before. It SHALL refuse to start with a certificate and a
key that do not belong together, naming the two settings.

#### Scenario: Certificate signed by an authority
- **GIVEN** `http.cert_file` holds a leaf and its authority and `http.key_file` the leaf's key
- **WHEN** a client that trusts only that authority requests `/api/v1/ready` under the certificate's name
- **THEN** the TLS handshake succeeds with both certificates sent and the answer is 200

#### Scenario: Self-signed certificate
- **GIVEN** `http.self_signed = true` and no certificate configured
- **WHEN** the server starts for the first time
- **THEN** it serves HTTPS with a certificate it generated for the public URL's host, a client that trusts that
  certificate is answered, and a client that does not refuses the connection

#### Scenario: Login and WebSocket over native HTTPS
- **GIVEN** the server speaks HTTPS itself on the public URL
- **WHEN** a user logs in and the app opens its WebSocket
- **THEN** the session cookie is `Secure` and the upgrade answers 101

#### Scenario: Key of another certificate
- **WHEN** `http.key_file` is not the key of `http.cert_file`
- **THEN** the server exits with an error naming `http.cert_file` and `http.key_file`
