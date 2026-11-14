# database Specification

## Purpose
Defines how MeetHub stores its data in exactly one relational database chosen by configuration (SQLite, MariaDB or
PostgreSQL) and how its schema evolves through versioned migrations.

## Requirements

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

### Requirement: Versioned migrations
Schema changes SHALL be applied through numbered migrations recorded in a migrations table, each with an up and a down
step, and applying the same migrations twice SHALL change nothing.

#### Scenario: Idempotent migrate
- **GIVEN** all migrations are applied
- **WHEN** `meethub migrate up` runs again
- **THEN** no migration runs and the command reports nothing pending

#### Scenario: Concurrent starts
- **GIVEN** two server processes start against the same database (any dialect)
- **WHEN** both try to migrate
- **THEN** a connection-scoped migration lock serializes them and each migration is applied exactly once

#### Scenario: Lock timeout
- **GIVEN** another process holds the migration lock for longer than the lock timeout (default 5 minutes)
- **WHEN** a server tries to migrate
- **THEN** it exits with status 1 stating the lock could not be acquired

#### Scenario: Interrupted migration on MariaDB
- **GIVEN** a migration failed half-way on MariaDB, where DDL is not transactional
- **WHEN** `meethub migrate up` runs again after the cause is fixed
- **THEN** the migration completes, because every MariaDB migration step is written to be safely re-runnable

### Requirement: Same behavior on every dialect
Every migration and every query the application runs SHALL behave the same on SQLite, MariaDB and PostgreSQL, and the
test suite SHALL run against all three.

#### Scenario: Unique email on every dialect
- **GIVEN** a user with email `ana@example.com` exists
- **WHEN** another user is created with `ANA@example.com` on any of the three dialects
- **THEN** creation fails as a duplicate email

### Requirement: Identical uniqueness semantics
E-mail uniqueness SHALL compare the normalized e-mail (trimmed, Unicode NFC, lower-cased) byte for byte on every
dialect (binary collation on MariaDB), and memberships SHALL be unique per organization and user with foreign keys
enforced.

#### Scenario: Accent-insensitive collation not used
- **GIVEN** a user `jose@example.com`
- **WHEN** a user `josé@example.com` is created on MariaDB
- **THEN** both exist, as on SQLite and PostgreSQL

### Requirement: Portable identifiers and time
Primary keys SHALL be ULIDs stored as strings, timestamps SHALL be stored in UTC and text SHALL be stored as UTF-8
(`utf8mb4` on MariaDB).

#### Scenario: Emoji round trip on MariaDB
- **WHEN** a display name containing an emoji is saved on MariaDB and read back
- **THEN** the same string is returned

### Requirement: Readiness reflects the database
The readiness probe SHALL fail while the database is unreachable or migrations are pending.

#### Scenario: Pending migrations
- **GIVEN** the server was started with `--no-migrate` on an outdated schema
- **WHEN** `/api/v1/ready` is requested
- **THEN** the response is 503 with the reason `migrations pending`
