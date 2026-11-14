# Spec Delta

## ADDED Requirements

### Requirement: Fair writes on SQLite
On SQLite, the write transactions of one server process SHALL run one at a time in the order they were requested,
none failing because the database is busy with another of them; a request cancelled while waiting SHALL stop
waiting, and reads SHALL NOT wait for writes.

#### Scenario: Burst of writers
- **GIVEN** four clients post a thousand messages to one channel at once on SQLite
- **WHEN** the burst runs
- **THEN** every message is stored, and no writer waits longer than the time the writers ahead of it take

#### Scenario: Cancelled while waiting
- **GIVEN** a write is waiting behind a slow transaction
- **WHEN** its request is cancelled
- **THEN** it returns at once without having written

#### Scenario: Reads pass
- **GIVEN** a write transaction is open
- **WHEN** another request reads the history of a channel
- **THEN** the read answers without waiting for the write to end
