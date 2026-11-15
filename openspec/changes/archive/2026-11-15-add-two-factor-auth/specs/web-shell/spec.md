# Spec Delta

## ADDED Requirements

### Requirement: Two-factor in Settings and at login
Settings SHALL have a Security section to add an authenticator (QR code and the secret as text), see new recovery
codes once, and turn two-factor off. The login page SHALL ask for the code after the password, with a way to use a
recovery code, and say when the code is wrong without saying more.

#### Scenario: Code step
- **GIVEN** Ana has two-factor on
- **WHEN** she submits her password on the login page
- **THEN** the page asks for the 6-digit code, with the focus in the field
