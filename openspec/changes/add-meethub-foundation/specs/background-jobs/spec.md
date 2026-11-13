# Spec Delta

## Purpose

Defines how MeetHub runs work outside the request path (cleanups, future e-mail, notifications and webhooks): queued,
retried and scheduled tasks that work with Redis and, on small installs, without it.

## ADDED Requirements

### Requirement: Durable queue with Redis
When `redis.enabled` is true, background tasks SHALL be enqueued in Redis and processed by workers in the server
process, surviving a server restart.

#### Scenario: Task survives restart
- **GIVEN** Redis is enabled and a task is enqueued
- **WHEN** the server restarts before the task runs
- **THEN** the task runs after the restart

### Requirement: In-process runner without Redis
When Redis is disabled, the same task types SHALL run in an in-process queue, and the documentation and the startup log
SHALL state that queued tasks are lost on restart.

#### Scenario: Task runs without Redis
- **GIVEN** Redis is disabled
- **WHEN** a session-cleanup task is enqueued
- **THEN** it runs in the server process and its result is logged

### Requirement: Retries with backoff
A failing task SHALL be retried with exponential backoff up to its maximum attempts and then recorded as failed with
its last error.

#### Scenario: Task fails permanently
- **GIVEN** a task type allowing 3 attempts that always fails
- **WHEN** it is enqueued
- **THEN** it runs 3 times with increasing delays and is reported as failed

### Requirement: Scheduled tasks
The system SHALL run periodic tasks on a schedule, at most once per period even when several server replicas run with
Redis.

#### Scenario: Expired sessions are purged
- **GIVEN** sessions expired more than the retention period ago
- **WHEN** the hourly cleanup task runs
- **THEN** those sessions are deleted

#### Scenario: One run with two replicas
- **GIVEN** two server replicas share Redis
- **WHEN** a periodic task becomes due
- **THEN** it runs once, not once per replica

### Requirement: Tasks carry no secrets
Task payloads SHALL contain identifiers only, never passwords, tokens or message bodies.

#### Scenario: Payload inspection
- **WHEN** any task type defined by this change is enqueued
- **THEN** its stored payload contains only IDs and parameters
