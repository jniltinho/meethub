# Spec Delta

## ADDED Requirements

### Requirement: Enrolling an authenticator
A logged-in person SHALL be able to add a time-based one-time password authenticator (RFC 6238: 6 digits, 30
seconds): the server gives the secret and a QR code, enrollment completes only with a valid code, and ten one-time
recovery codes are then shown once and stored only as hashes.

#### Scenario: Enroll
- **WHEN** Ana scans the QR code and submits the code her app shows
- **THEN** two-factor is on for her account and she sees ten recovery codes

#### Scenario: Wrong confirmation
- **WHEN** she submits a wrong code
- **THEN** two-factor stays off

### Requirement: Second step at login
For a person with two-factor on, a correct password SHALL create no session: the answer carries a single-use ticket
valid for 5 minutes, and `POST /api/v1/auth/login/two-factor` with the ticket and a current code, or an unused
recovery code, SHALL create the session. A code SHALL be accepted only once.

#### Scenario: Password, then code
- **WHEN** Ana logs in with her password and then submits the current code
- **THEN** the first answer has no session cookie and the second one sets it

#### Scenario: Code replayed
- **WHEN** the same code is submitted for a second login within its 30 seconds
- **THEN** it is refused

#### Scenario: Recovery code
- **WHEN** Ana submits one of her recovery codes instead of a code
- **THEN** the session is created and that recovery code no longer works

### Requirement: Limits on the second step
Five wrong codes SHALL void the ticket, and the attempt SHALL count as a failed login in the brute-force
protection.

#### Scenario: Guessing
- **WHEN** five wrong codes are sent with one ticket
- **THEN** the ticket no longer works, even with the right code

### Requirement: Turning off and resets
A person SHALL turn two-factor off only with their password and a current code. A user with `manage_users` SHALL be
able to reset another person's two-factor (an owner's only with `manage_owners`); a reset SHALL end every session
of that person.

#### Scenario: Admin reset
- **WHEN** an admin resets Bruno's two-factor
- **THEN** Bruno's sessions end and his next login needs only the password
