# Tasks

Every Go task runs its tests with `-race` on SQLite, MariaDB and PostgreSQL (`make test-db`).

## 1. Index

- [x] 1.1 Prove the three full-text engines in the test stack (FTS5 in the SQLite driver, `FULLTEXT` on the
  MariaDB image, `tsvector` on PostgreSQL) with a throwaway test; verify and record the result in the change before
  going on
  - Result (throwaway test on the test stack, removed afterwards): FTS5 is compiled into modernc.org/sqlite v1.62.0
    (`CREATE VIRTUAL TABLE … USING fts5`, `MATCH '"deploy"* "17h"*'`), MariaDB 11.4 InnoDB `FULLTEXT` with
    `MATCH … AGAINST ('+deploy* +17h*' IN BOOLEAN MODE)`, and PostgreSQL 17 a generated `tsvector` column with a GIN
    index and `to_tsquery('simple', 'deploy:* & 17h:*')` each returned exactly the expected row. MariaDB's settings:
    `innodb_ft_min_token_size = 3`, stop words on, the default list (it includes `de`, `la`, `en`).
- [x] 1.2 Implement the normalization (Markdown removed, lowercase, accents folded) for messages and query terms;
  verify table-driven tests in Portuguese, Spanish and English
- [x] 1.3 Write the migrations and maintain the index on insert, edit and delete; verify the "Search index"
  scenarios on the three dialects

## 2. Search

- [x] 2.1 Query the index (terms as prefixes, phrases, the MariaDB short-term fallback) keeping scope, modifiers,
  order and paging; verify every `message-search` scenario on the three dialects and the existing search tests
- [x] 2.2 Implement the background backfill with its progress and the switch from the scan; verify the backfill
  scenario, a restart in the middle and two replicas
- [x] 2.3 Measure search on 500 000 seeded messages before and after on each dialect and record the numbers
  - `TestSearchAtScale` (`-tags searchbench`, `internal/chat/search_bench_test.go`): 500 000 messages of 12 words in
    one channel, statistics analyzed, best of 5, first page of 20. Index / scan:

    | Query | SQLite | MariaDB | PostgreSQL |
    | --- | --- | --- | --- |
    | rare word (10 messages) | 0.3 ms / 187 ms | 0.5 ms / 298 ms | 1.7 ms / 78 ms |
    | common word (50 000 messages) | 173 ms / 233 ms | 404 ms / 330 ms | 62 ms / 81 ms |
    | rare + common word | 4 ms / 287 ms | 7.5 ms / 355 ms | 6.1 ms / 87 ms |
    | phrase without a match | 0.3 ms / 286 ms | 0.7 ms / 355 ms | 2.6 ms / 86 ms |

    A word in one message out of ten is the index's worst case: every match is read and sorted by time, where the
    scan stops at the first page (on MariaDB the scan is then faster). The scan grows with the history; the index
    with the matches. Without fresh statistics PostgreSQL chose a nested loop over the newest posts (1 s for the rare
    word); autovacuum's ANALYZE gives the plan above.

## 3. Validation

- [ ] 3.1 Update `docs/api.md` (matching rules) and remove the shortcut note in the code; run `make check`,
  `make test`, `make test-db`, `make test-e2e`, `make build`, a Codex review with its findings fixed, and CI green
  - Open for CI only (it runs after a push); the rest is recorded in the implementation report.

## Workflow follow-up

- Archive with `openspec archive add-native-message-search` after review.
