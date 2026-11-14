# Spec Delta

## ADDED Requirements

### Requirement: Notifications in Settings
Settings SHALL have a Notifications section with the desktop preference (and a button that asks for the browser's
permission, showing its state) and the e-mail preference, which SHALL say when e-mail is not configured.

#### Scenario: Permission denied
- **GIVEN** the browser denied notifications
- **WHEN** Ana opens the section
- **THEN** it says desktop notifications are blocked in the browser and how to allow them
