# configuration Specification

## Purpose
Defines how MeetHub reads its settings: a TOML file, environment overrides, defaults and validation, with secrets
supplied only through the environment.

## Requirements

### Requirement: Configuration sources and precedence
The system SHALL read settings from defaults, then the TOML file (`--config`, else `./config.toml`, else
`/etc/meethub/config.toml`), then `MEETHUB_*` environment variables, each source overriding the previous one.

#### Scenario: Environment overrides the file
- **GIVEN** `config.toml` sets `http.addr = ":8080"`
- **WHEN** the server starts with `MEETHUB_HTTP_ADDR=:9090`
- **THEN** the server listens on port 9090

#### Scenario: Runs without a file
- **GIVEN** no configuration file exists
- **WHEN** the server starts in development
- **THEN** it uses the defaults (SQLite at `<data_dir>/meethub.db`, port 8080, Redis at `localhost:6379` database 0)

### Requirement: Secrets only through the environment
Secrets (`MEETHUB_DATABASE_PASSWORD`, `MEETHUB_REDIS_PASSWORD`, `MEETHUB_SETUP_TOKEN`, later LiveKit keys) SHALL be read
only from environment variables or from the file named by the same variable with a `_FILE` suffix (Docker secrets);
a secret set in the TOML file SHALL fail validation.

#### Scenario: Password in the file
- **WHEN** `config.toml` contains `database.password`
- **THEN** startup fails stating that secrets must come from `MEETHUB_DATABASE_PASSWORD` or `MEETHUB_DATABASE_PASSWORD_FILE`

#### Scenario: Docker secret file
- **GIVEN** `MEETHUB_DATABASE_PASSWORD_FILE=/run/secrets/db` and that file holds the password
- **WHEN** the server starts
- **THEN** it connects with that password

#### Scenario: Both forms set
- **WHEN** both `MEETHUB_DATABASE_PASSWORD` and `MEETHUB_DATABASE_PASSWORD_FILE` are set
- **THEN** startup fails naming both variables

### Requirement: Database settings without credentials in DSNs
The database SHALL be configured by structured fields (`driver`, `path` for SQLite; `host`, `port`, `name`, `user`,
`params` for MariaDB/PostgreSQL) and the password secret, never by a DSN string containing credentials.

#### Scenario: Secret not echoed in errors
- **WHEN** the PostgreSQL connection fails with the configured password
- **THEN** the error names host, port and database but contains no password, also in wrapped driver errors

### Requirement: Configuration validation
The system SHALL validate the loaded configuration before using it and SHALL report every invalid setting at once.

#### Scenario: Several errors
- **WHEN** `database.driver` is `oracle` and `http.addr` is `abc`
- **THEN** startup fails listing both settings, the first stating the accepted values `sqlite`, `mariadb`, `postgres`

### Requirement: Public URL and trusted proxies
The configuration SHALL define `http.public_url` (the canonical origin) and `http.trusted_proxies` (CIDRs, default
none); forwarded headers (`X-Forwarded-For`, `X-Forwarded-Proto`) SHALL be honored only from those peers.

#### Scenario: Forged forwarded header
- **GIVEN** no trusted proxies
- **WHEN** a client sends `X-Forwarded-For: 1.2.3.4`
- **THEN** the client address used for rate limits and audit is the TCP peer, not 1.2.3.4

#### Scenario: Behind a trusted proxy
- **GIVEN** `http.trusted_proxies = ["10.0.0.0/8"]` and a request from 10.0.0.5 with `X-Forwarded-For: 203.0.113.9`
- **WHEN** the request is handled
- **THEN** the client address is 203.0.113.9

### Requirement: Example configuration
The repository SHALL contain `config.toml.example` and `.env.example` documenting every setting with its default, and
SHALL NOT contain a real `config.toml` or `.env`.

#### Scenario: Example parses
- **WHEN** the configuration loader reads `config.toml.example`
- **THEN** it parses and validates without errors

### Requirement: File settings
The configuration SHALL have a `files` section: `driver` (`local` or `s3`), `max_size_bytes`, `max_per_post`,
`local.dir`, and `s3.endpoint`, `s3.bucket`, `s3.region` and `s3.use_ssl`; the S3 access key and secret SHALL be
accepted only from `MEETHUB_FILES_S3_ACCESS_KEY` and `MEETHUB_FILES_S3_SECRET_KEY`. Validation SHALL reject an
unknown driver, non-positive limits, more than 50 files per message and an `s3` driver without endpoint, bucket or
credentials.

#### Scenario: S3 without credentials
- **WHEN** the server starts with `files.driver = "s3"` and no secret in the environment
- **THEN** it exits with an error naming `files.s3` and never prints a credential

#### Scenario: Secret in the file
- **WHEN** `config.toml` contains an S3 secret key
- **THEN** startup fails saying secrets come only from the environment

### Requirement: LiveKit settings
The `[livekit]` section SHALL hold `url` (the WebSocket URL browsers use) and `api_url` (the HTTP URL the server uses,
defaulting to `url` with `ws`→`http`); `MEETHUB_LIVEKIT_API_KEY` and `MEETHUB_LIVEKIT_API_SECRET` SHALL be accepted
only from the environment or `_FILE`. Validation SHALL require all three together, a secret of at least 32 bytes, and
`wss://` in production.

#### Scenario: Secret in the file
- **WHEN** `config.toml` sets `livekit.api_secret`
- **THEN** startup fails saying secrets must come from `MEETHUB_LIVEKIT_API_SECRET` or `MEETHUB_LIVEKIT_API_SECRET_FILE`

#### Scenario: Incomplete settings
- **GIVEN** `livekit.url` is set and `MEETHUB_LIVEKIT_API_SECRET` is not
- **WHEN** the server starts
- **THEN** it fails naming the missing setting

#### Scenario: Insecure URL in production
- **GIVEN** `app.env` is production
- **WHEN** `livekit.url` is `ws://livekit.example.com`
- **THEN** validation fails asking for `wss://`

#### Scenario: Meetings off
- **WHEN** no LiveKit setting is given
- **THEN** the server starts with meetings disabled
