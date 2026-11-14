# Tasks

## 1. Specs

- [ ] 1.1 Confirm that the two new audit scenarios and the new permission scenario describe what the code already
  does (logout and team creation are audited; an admin cannot grant `owner`); verify by pointing each at its
  existing test, adding the test only if one is missing
- [ ] 1.2 Archive the change and run `openspec validate --all --strict`; verify every spec passes
- [ ] 1.3 Add `openspec validate --all --strict` to `make check` when the CLI is available in the tools image;
  verify `make check` fails on a requirement of 501 characters and passes without it

## Workflow follow-up

- Archive with `openspec archive tidy-long-spec-requirements` once task 1.1 is confirmed.
