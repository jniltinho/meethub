# Spec Delta

## MODIFIED Requirements

### Requirement: One database selected by configuration
The system SHALL connect to exactly one database whose dialect is `sqlite`, `mariadb` or `postgres`, chosen by
`database.driver` and its connection settings.

#### Scenario: SQLite default
- **GIVEN** no database settings are given
- **WHEN** the server starts
- **THEN** it creates `meethub.db` in the data directory, and every connection reports `journal_mode=wal`,
  `busy_timeout=30000` and `foreign_keys=1`

#### Scenario: Unreachable database
- **GIVEN** `database.driver` is `postgres` and the server is down
- **WHEN** `meethub server` starts
- **THEN** it retries for the configured connect timeout and then exits with status 1 and a clear error

#### Scenario: Burst of writes on SQLite
- **GIVEN** four clients post a thousand messages to one channel at once on SQLite
- **WHEN** a writer finds the database locked by the others for longer than five seconds
- **THEN** it keeps waiting and its message is stored, in order with the others
