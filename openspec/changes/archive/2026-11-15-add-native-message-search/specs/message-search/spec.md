# Spec Delta

## MODIFIED Requirements

### Requirement: Query syntax
A message SHALL match when it contains every term as a word or the beginning of a word, ignoring letter case and
accents on every database; text between double quotes SHALL match those words in sequence. `from:<username>` SHALL
keep only that author's messages and `in:<channel name>` only that channel's; several of the same modifier SHALL be
alternatives.

#### Scenario: All terms
- **GIVEN** messages "deploy at 17h" and "deploy tomorrow"
- **WHEN** the user searches `deploy 17h`
- **THEN** only the first is returned

#### Scenario: Phrase and author
- **WHEN** the user searches `"release notes" from:carla.pereira`
- **THEN** only Carla's messages containing "release notes" are returned

#### Scenario: Case
- **WHEN** the user searches `REDIS`
- **THEN** a message containing "Redis" is returned

#### Scenario: Accents
- **GIVEN** a message "RELATÓRIO de vendas" on SQLite, MariaDB or PostgreSQL
- **WHEN** the user searches `relatorio`
- **THEN** it is returned

#### Scenario: Beginning of a word
- **WHEN** the user searches `deploy`
- **THEN** a message containing "deployment" is returned

#### Scenario: Inside a word
- **WHEN** the user searches `ploy`
- **THEN** a message whose only match would be inside "deploy" is not returned

## ADDED Requirements

### Requirement: Search index
Messages SHALL be indexed for search in the transaction that stores, edits or deletes them, and search SHALL use
that index. After an upgrade, existing messages SHALL be indexed in the background, and until that completes search
SHALL still return every matching message.

#### Scenario: Edited message
- **WHEN** a message is edited from "budget approved" to "budget postponed"
- **THEN** a search for `approved` no longer returns it and `postponed` does

#### Scenario: During the backfill
- **GIVEN** an upgraded install is still indexing its history
- **WHEN** a user searches a word used only in an old message
- **THEN** that message is returned
