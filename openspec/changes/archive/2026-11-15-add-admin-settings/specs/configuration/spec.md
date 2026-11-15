## ADDED Requirements

### Requirement: Settings changed from Administration
The configuration file SHALL remain the only store of settings: a change made in Administration SHALL be a change
of `config.toml`, loaded with the same precedence as any other — the environment still wins — and the file SHALL
stay valid for hand editing afterwards.

#### Scenario: Hand edit after a change from the page
- **GIVEN** a setting changed from Administration
- **WHEN** an administrator edits another key of `config.toml` by hand and restarts
- **THEN** both values are in effect and the page shows them
