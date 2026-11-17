# Delta for audit-log

## ADDED Requirements

### Requirement: AI key changes are audited
Setting, replacing or removing an AI key SHALL be recorded as `ai.key_changed` with the actor, which key it was
and whether it was set or removed, and never the key or any part of it. A refused re-authentication SHALL be
recorded as `settings.reauth_failed`.

#### Scenario: A key replaced
- **WHEN** an owner replaces the AI key
- **THEN** the audit log holds `ai.key_changed` with `key: "ai.api_key"` and `action: "set"`, and no entry holds the key

#### Scenario: A wrong password
- **WHEN** a sensitive change is sent with a wrong password
- **THEN** the audit log holds `settings.reauth_failed` with the actor and the keys named, without their values
