# server-cli Specification

## Purpose
Defines the `meethub` binary's command-line interface: the commands an operator uses to run the server, migrate the
database, manage users and inspect the build.

## Requirements

### Requirement: Single binary with subcommands
The system SHALL ship as one `meethub` executable whose subcommands are `server`, `migrate`, `user`, `healthcheck` and `version`, and
every command SHALL accept `--config <path>` and print usage with `--help`.

#### Scenario: Help lists the commands
- **WHEN** the operator runs `meethub --help`
- **THEN** the output lists `server`, `migrate`, `user`, `healthcheck` and `version` and the exit status is 0

#### Scenario: Unknown command
- **WHEN** the operator runs `meethub frobnicate`
- **THEN** an error naming the unknown command is printed to stderr and the exit status is 1

### Requirement: Version command
The `version` command SHALL print the version, commit and build time embedded at build time and SHALL NOT require a
configuration file or a database.

#### Scenario: Version without configuration
- **GIVEN** no `config.toml` exists
- **WHEN** the operator runs `meethub version`
- **THEN** the version, commit and build time are printed and the exit status is 0

### Requirement: Server command
The `server` command SHALL load the configuration, apply pending migrations unless `--no-migrate` is given, start the
HTTP server and background workers, and shut down gracefully on SIGINT or SIGTERM.

#### Scenario: Graceful shutdown
- **GIVEN** the server is running with an in-flight request
- **WHEN** the process receives SIGTERM
- **THEN** new connections are refused, the in-flight request completes within the shutdown timeout and the process
  exits with status 0

#### Scenario: Invalid configuration
- **WHEN** `meethub server` starts with a configuration that fails validation
- **THEN** each invalid setting is reported on stderr and the process exits with status 1 without listening

### Requirement: Migrate command
The `migrate` command SHALL offer `up` (apply pending migrations), `down` (revert the last one) and `status` (list
applied and pending migrations).

#### Scenario: Status after a fresh install
- **GIVEN** an empty database
- **WHEN** the operator runs `meethub migrate up` and then `meethub migrate status`
- **THEN** every migration is reported as applied and none as pending

### Requirement: User administration command
The `user` command SHALL create a user with a role (`user create --email --name --role`), reset a password
(`user set-password`, revoking all of that user's sessions) and list users (`user list`), prompting for passwords on a
terminal (or reading them from stdin with `--password-stdin`) and never accepting them as arguments.

#### Scenario: Initialize from the CLI
- **GIVEN** an uninitialized install with migrations applied
- **WHEN** the operator runs `meethub user create --org-name "Acme" --email ana@example.com --name "Ana" --role owner`
  and types a password
- **THEN** the organization `Acme` (slug `acme`) and its owner are created atomically, as by the setup page, and the
  setup token is invalidated

#### Scenario: First user must be an owner
- **GIVEN** an uninitialized install
- **WHEN** the operator runs `meethub user create` with `--role member` or without `--org-name`
- **THEN** the command fails explaining that the first user is an owner and needs `--org-name`

#### Scenario: JSON output
- **WHEN** the operator runs `meethub user list --json`
- **THEN** the output is a JSON array of users without password hashes

### Requirement: Healthcheck command
The `healthcheck` command SHALL request the local server's `/api/v1/health` and exit 0 when it answers 200 within 3
seconds, and 1 otherwise, so container images without a shell or curl can use it.

#### Scenario: Server down
- **GIVEN** no server listens on the configured address
- **WHEN** `meethub healthcheck` runs
- **THEN** it exits with status 1
