# background-jobs Specification

## Purpose
Defines how MeetHub runs work outside the request path (cleanups, later e-mail, notifications and webhooks): durable,
retried and scheduled tasks on a Redis-backed queue.

## Requirements

### Requirement: Redis-backed durable queue
Background tasks SHALL always be enqueued in Redis and processed by workers in the server process; Redis is a required
service and tasks SHALL survive a server restart.

#### Scenario: Task survives restart
- **GIVEN** a task is enqueued
- **WHEN** the server restarts before the task runs
- **THEN** the task runs after the restart

#### Scenario: Redis unreachable at startup
- **GIVEN** Redis is unreachable
- **WHEN** `meethub server` starts
- **THEN** it retries for the configured connect timeout and then exits with status 1 naming Redis

#### Scenario: Redis lost while running
- **GIVEN** the server is running and Redis becomes unreachable
- **WHEN** a request enqueues a task
- **THEN** the enqueue returns an error that the caller handles, `/api/v1/ready` reports 503 naming `redis`, and
  workers resume when Redis returns

#### Scenario: Shutdown
- **WHEN** the server shuts down
- **THEN** workers stop taking new tasks, running ones get the shutdown timeout, and unfinished tasks stay in Redis to
  run later

### Requirement: Attempts and backoff
Each task type SHALL declare `max_attempts` (total executions, including the first); failures SHALL be retried with
exponential backoff with jitter (default base 10 s, cap 10 min), and after the last attempt the task SHALL be kept as
failed (archived) with its last error.

#### Scenario: Task fails permanently
- **GIVEN** a task type with `max_attempts = 3` that always fails
- **WHEN** it is enqueued
- **THEN** it runs exactly 3 times with increasing delays and ends archived with its last error

### Requirement: Scheduled tasks run their work at most once per period
Periodic tasks SHALL be scheduled on UTC periods; each occurrence SHALL claim its period atomically in the database
before doing its work, so the work completes at most once per period even with several replicas, staggered
schedulers, restarts or retries, and handlers SHALL be idempotent.

#### Scenario: Two replicas
- **GIVEN** two server replicas share Redis and the database
- **WHEN** an hourly task's period starts
- **THEN** its work is done once for that period

#### Scenario: Completed before the second enqueue
- **GIVEN** replica A ran and completed the 10:00 occurrence
- **WHEN** replica B's scheduler enqueues the 10:00 occurrence later
- **THEN** B's run sees the claimed period and exits without doing the work

#### Scenario: Worker crash
- **GIVEN** a worker crashed after claiming a period but before finishing
- **WHEN** the claim lease (default 15 minutes) expires
- **THEN** a retry may claim the period again and complete the work

### Requirement: Session cleanup
An hourly task SHALL delete sessions that expired or were revoked more than 7 days ago.

#### Scenario: Old sessions purged
- **GIVEN** a session revoked 8 days ago and one revoked yesterday
- **WHEN** the cleanup task runs
- **THEN** only the 8-day-old session is deleted

### Requirement: Tasks carry no secrets
Task payloads SHALL contain identifiers and parameters only, never passwords, tokens or message bodies.

#### Scenario: Payload inspection
- **WHEN** any task type defined by this change is enqueued
- **THEN** its stored payload contains only IDs and parameters
