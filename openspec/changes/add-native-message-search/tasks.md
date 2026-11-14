# Tasks

Every Go task runs its tests with `-race` on SQLite, MariaDB and PostgreSQL (`make test-db`).

## 1. Index

- [ ] 1.1 Prove the three full-text engines in the test stack (FTS5 in the SQLite driver, `FULLTEXT` on the
  MariaDB image, `tsvector` on PostgreSQL) with a throwaway test; verify and record the result in the change before
  going on
- [ ] 1.2 Implement the normalization (Markdown removed, lowercase, accents folded) for messages and query terms;
  verify table-driven tests in Portuguese, Spanish and English
- [ ] 1.3 Write the migrations and maintain the index on insert, edit and delete; verify the "Search index"
  scenarios on the three dialects

## 2. Search

- [ ] 2.1 Query the index (terms as prefixes, phrases, the MariaDB short-term fallback) keeping scope, modifiers,
  order and paging; verify every `message-search` scenario on the three dialects and the existing search tests
- [ ] 2.2 Implement the background backfill with its progress and the switch from the scan; verify the backfill
  scenario, a restart in the middle and two replicas
- [ ] 2.3 Measure search on 500 000 seeded messages before and after on each dialect and record the numbers

## 3. Validation

- [ ] 3.1 Update `docs/api.md` (matching rules) and remove the shortcut note in the code; run `make check`,
  `make test`, `make test-db`, `make test-e2e`, `make build`, a Codex review with its findings fixed, and CI green

## Workflow follow-up

- Archive with `openspec archive add-native-message-search` after review.
