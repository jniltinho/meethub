# Message search on a real index

## Why

Message search scans the text of every message of the caller's channels with `LIKE` on each request. It is exact
and simple, and it does not scale: the time grows with the history, on every database. It also ignores case only
for ASCII on SQLite, so "RELATÓRIO" does not find "relatório" on the default database. Both are recorded as known
shortcuts in the code. An install that people rely on for a year will feel it.

## What Changes

- Messages are indexed for search as they are written, edited and deleted, in the same transaction.
- Search uses the database's own full-text index: `tsvector` with a GIN index on PostgreSQL, `FULLTEXT` on
  MariaDB, FTS5 on SQLite — over text normalized by the server (lowercase, accents folded), so the three behave
  alike.
- **BREAKING (search semantics)**: a term matches a word or the beginning of a word, no longer any substring:
  `deploy` still finds "deployment", `ploy` no longer finds "deploy". Case and accents are ignored on every
  database. Quoted text matches consecutive words.
- Existing messages are indexed in the background after the upgrade; until that finishes, search answers from the
  old scan, so results are never incomplete.
- `from:` and `in:`, the scope rules, paging and the rate limit do not change.

## Capabilities

### New Capabilities

None.

### Modified Capabilities

- `message-search`: how terms match, and the index behind the search.

## Impact

- **Server**: `internal/chat/search.go` and the posting, editing and deleting paths; a migration per dialect
  (a normalized text column or table and its index); a background backfill with its progress in the database.
- **Dependencies**: `golang.org/x/text` for accent folding (already in the module graph).
- **Database**: more disk for the index (roughly the size of the message text); FTS5 must be present in the SQLite
  driver in use — verified by a task before anything else.
