# Tasks

Every Go task runs its tests with `-race` on SQLite, MariaDB and PostgreSQL (`make test-db`).

## 1. Preferences and desktop

- [ ] 1.1 Add the migration and the two preferences to the profile's preferences; verify defaults and updates
- [ ] 1.2 Show desktop notifications from the chat store (rule of D1, tag per channel, open on click) and add
  Settings → Notifications with the permission request (pt-BR, es); verify unit tests of the rule and Playwright
  with the permission granted and denied

## 2. E-mail

- [ ] 2.1 Add the `smtp` settings with validation and the example; verify table-driven config tests
- [ ] 2.2 Implement the SMTP sender and the templates in three languages; verify against a test SMTP server in the
  test (STARTTLS, authentication, a refused connection)
- [ ] 2.3 Queue direct messages and direct mentions and implement the task (10 minutes unread, offline, one per 15
  minutes, readable at send time, excerpt setting); verify the `notifications` and `background-jobs` scenarios
- [ ] 2.4 Add the unsubscribe token, link, header and page; verify the scenario and a wrong token

## 3. Validation

- [ ] 3.1 Document the settings and what e-mails contain; run `make check`, `make test`, `make test-db`,
  `make test-e2e`, `make build`, a Codex review with its findings fixed, and CI green

## Workflow follow-up

- Archive with `openspec archive add-notifications` after review.
