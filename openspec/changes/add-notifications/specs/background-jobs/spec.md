# Spec Delta

## ADDED Requirements

### Requirement: Notification e-mails
A scheduled task SHALL run every minute, at most once per period across replicas, and send the pending notification
e-mails; a failed send SHALL leave its messages for a later run, and the task SHALL send nothing while presence is
unavailable.

#### Scenario: Mail server down
- **GIVEN** the SMTP server refuses connections
- **WHEN** the task runs
- **THEN** nothing is lost and the e-mail goes out on a later run
