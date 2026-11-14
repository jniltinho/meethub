# Spec Delta

## ADDED Requirements

### Requirement: AI summaries are audited
Changing the Meeting summaries setting and requesting a summary SHALL each record an audit event with the actor and
the target (the setting's old and new value, or the recording), never the meeting's content.

#### Scenario: Setting changed
- **WHEN** an owner sets Meeting summaries from `off` to `automatic`
- **THEN** an event `org.meeting_summaries_changed` records the owner and both values
