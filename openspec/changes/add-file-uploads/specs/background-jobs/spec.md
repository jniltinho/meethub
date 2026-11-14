# Spec Delta

## ADDED Requirements

### Requirement: File cleanup
A scheduled task SHALL run hourly, at most once per period across replicas, and remove the uploads never attached
for more than 24 hours and the stored bytes of deleted files; a failure to remove one object SHALL be retried in a
later run without stopping the others.

#### Scenario: Two replicas
- **GIVEN** two replicas and one abandoned upload
- **WHEN** the hour's cleanup is due
- **THEN** the upload is removed once and one replica records the run

#### Scenario: Storage error
- **GIVEN** the storage fails to delete one of three objects
- **WHEN** the cleanup runs
- **THEN** the other two are removed and the failed one is removed by a later run
