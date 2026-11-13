# Spec Delta

## Purpose

Defines how MeetHub reads its settings: a TOML file, environment overrides, defaults and validation, with secrets kept
out of versioned files.

## ADDED Requirements

### Requirement: Configuration sources and precedence
The system SHALL read settings from defaults, then the TOML file (`--config`, else `./config.toml`, else
`/etc/meethub/config.toml`), then `MEETHUB_*` environment variables, each source overriding the previous one.

#### Scenario: Environment overrides the file
- **GIVEN** `config.toml` sets `http.addr = ":8080"`
- **WHEN** the server starts with `MEETHUB_HTTP_ADDR=:9090`
- **THEN** the server listens on port 9090

#### Scenario: Runs without a file
- **GIVEN** no configuration file exists and `MEETHUB_SESSION_SECRET` is set
- **WHEN** the server starts
- **THEN** it uses the defaults (SQLite in the data directory, port 8080, Redis disabled)

### Requirement: Configuration validation
The system SHALL validate the loaded configuration before using it and SHALL report every invalid setting, not only
the first.

#### Scenario: Unknown database driver
- **WHEN** `database.driver` is `oracle`
- **THEN** startup fails with a message stating the accepted values `sqlite`, `mariadb` and `postgres`

#### Scenario: Short session secret in production
- **GIVEN** `app.env` is `production`
- **WHEN** the session secret is missing or shorter than 32 bytes
- **THEN** startup fails naming `MEETHUB_SESSION_SECRET`

### Requirement: Secrets only through the environment or protected files
Secrets (session secret, database password, Redis password, LiveKit keys) SHALL be accepted from environment variables
and SHALL never be logged, printed by any command or returned by any API.

#### Scenario: Secret not echoed
- **WHEN** the configuration is invalid because the database is unreachable
- **THEN** the error message contains the host and database name but not the password

### Requirement: Example configuration
The repository SHALL contain `config.toml.example` documenting every setting with its default, and SHALL NOT contain a
real `config.toml` or `.env` file.

#### Scenario: Example parses
- **WHEN** the configuration loader reads `config.toml.example`
- **THEN** it parses without errors
