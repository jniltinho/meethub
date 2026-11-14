# Spec Delta

## ADDED Requirements

### Requirement: Summary of a recording
A recording message SHALL show its summary's state (being written, ready in the thread, failed, cancelled) and, for
who may record, "Summarize" (when it can be summarized and has no summary) and "Try again" (failed); neither SHALL
be offered for a recording without summary sources. The summary reply SHALL carry a "Written by AI" label that is
part of the interface, and render through the same sanitizing pipeline as any message.

#### Scenario: Summarize
- **GIVEN** a recording made under `manual`
- **WHEN** Ana presses "Summarize" on it
- **THEN** the recording shows "Writing the summary…" and, when it is ready, a link to the reply

#### Scenario: Not summarizable
- **GIVEN** a recording made while summaries were off
- **WHEN** Ana opens it
- **THEN** there is no "Summarize" action

#### Scenario: Hostile output
- **WHEN** a summary contains HTML, a script or an image address
- **THEN** it is shown as inert text and nothing is loaded from elsewhere

### Requirement: Summaries setting in Administration
Administration SHALL have the Meeting summaries setting with its three values, a "Test connection" action showing
the result, and, when turning it off, a note that what was already sent to the AI service cannot be recalled.

#### Scenario: Turning off
- **WHEN** an owner chooses `off`
- **THEN** a themed confirmation says running summaries will stop and that data already sent stays with the service
