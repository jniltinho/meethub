# Tasks

## 1. Specs

- [x] 1.1 Confirm that the two new audit scenarios and the new permission scenario describe what the code already
  does (logout and team creation are audited; an admin cannot grant `owner`); verify by pointing each at its
  existing test, adding the test only if one is missing
  - Logout: `TestChangePasswordAndLogout` in `internal/auth/auth_test.go` (the audit check was missing and is added:
    actor is the user, target the session). Team creation: `internal/chat/team_test.go` (audits `team.created`).
    Admin cannot grant `owner`: the role table in `internal/org/org_test.go` ("admin cannot create owners").
- [ ] 1.2 Archive the change and run `openspec validate --all --strict`; verify every spec passes
- [ ] 1.3 Add `openspec validate --all --strict` to `make check` when the CLI is available in the tools image;
  verify `make check` fails on a requirement of 501 characters and passes without it
  - Open: depends on 1.2 — before the archive the main specs still fail strict validation, so the gate would break
    `make check`.

## Workflow follow-up

- Archive with `openspec archive tidy-long-spec-requirements` once task 1.1 is confirmed.
