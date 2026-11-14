# Spec Delta

## ADDED Requirements

### Requirement: Summary task
Writing a summary SHALL be a queued task carrying only the recording's id, run under a claim so that one runs per
recording at a time, on workers separate from the ones that serve periodic tasks. A retry SHALL reuse every stored
transcription result; a summary in an active state without a running task SHALL be queued again within two minutes,
and one that failed or was cancelled SHALL NOT be restarted on its own.

#### Scenario: Retry reuses finished work
- **GIVEN** nine of ten audio pieces were transcribed and stored before the service failed
- **WHEN** the task is retried
- **THEN** only the tenth piece is sent again

#### Scenario: Enqueue lost
- **GIVEN** a server stops right after a recording became ready under `automatic`, before queueing its summary
- **WHEN** two minutes pass with a server running
- **THEN** the summary is queued again and runs when a worker is free

#### Scenario: Cancelled stays cancelled
- **GIVEN** a summary was cancelled because the setting was turned off
- **WHEN** the setting is turned on again
- **THEN** that summary does not start until someone asks for it

#### Scenario: Periodic tasks keep running
- **GIVEN** summary tasks occupy all their workers
- **WHEN** a periodic lifecycle task is due
- **THEN** it runs on time
