# Shorter requirements where the specs warn

## Why

`openspec validate --all --strict` fails on three specs because one requirement in each is longer than 500
characters: `audit-log` ("Security events are recorded"), `organizations` ("Roles and permissions") and `chat-ui`
("Channel view"). The warnings are right — each mixes a rule with a list — and while they stand, strict validation
of the whole repository cannot be a gate.

## What Changes

- `audit-log`: the rule stays in "Security events are recorded"; the list of audited actions moves to two new
  requirements, one for accounts and the organization, one for teams, channels and messages.
- `organizations`: the permission table loses its parenthetical explanations, which become a requirement of their
  own, "What each permission covers".
- `chat-ui`: "Channel view" is reworded to say the same in fewer words.
- **No behavior changes**: every existing scenario stays, word for word, under the same requirement.

## Capabilities

### New Capabilities

None.

### Modified Capabilities

- `audit-log`, `organizations`, `chat-ui`: wording and structure only.

## Impact

- Specs only. No code, no tests, no documentation of behavior changes.
- After archiving, `openspec validate --all --strict` passes for every spec and can be added to `make check`.
