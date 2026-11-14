# Spec Delta

## ADDED Requirements

### Requirement: Activity cleanup
A scheduled task SHALL run daily, at most once per period across replicas, and remove activity items older than 90
days.

#### Scenario: Two replicas
- **GIVEN** two replicas
- **WHEN** the daily cleanup is due
- **THEN** it runs once
