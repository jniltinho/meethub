# Spec Delta

## ADDED Requirements

### Requirement: Meetings check command
`meethub meetings check` SHALL report whether the configured LiveKit answers its room API with the configured key,
the browser URL in use and whether a TURN allocation succeeds, exiting 0 only when all pass and never printing a
secret.

#### Scenario: Wrong secret
- **GIVEN** the configured API secret is not LiveKit's
- **WHEN** the command runs
- **THEN** it says the key was refused and exits 1
