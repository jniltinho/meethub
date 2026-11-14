# Design

## Context

`Search` joins posts with the caller's memberships and filters with `LOWER(p.message) LIKE ?` per term. Terms are
parsed in Go (`searchTerms`: words of two or more characters, quoted phrases, `from:`, `in:`). Messages are at most
16 383 characters. Three dialects must behave the same.

## Decisions

### D1. Normalize in Go, index natively

The server computes a search text for each message: Markdown syntax removed, lowercase, accents folded (NFD, marks
dropped), punctuation to spaces. Query terms go through the same function. This is what makes the three databases
agree; their own analyzers are set to do nothing else (`simple` configuration, no stemming, no stop words).

- **PostgreSQL**: a `tsvector` column filled from the search text, GIN index, `to_tsquery('simple', 'term:*')`.
- **MariaDB**: a `FULLTEXT` index on the search text column, boolean mode with `+term*`. Terms shorter than the
  server's minimum token size (3 by default) fall back to the scan for that term.
- **SQLite**: an FTS5 table (`unicode61`) keyed by the post's rowid, queried with `term*`.

Phrases use each database's phrase syntax; the access join and the ordering stay as they are.

### D2. Index maintenance in the writing transaction

Insert, edit and delete update the index in the transaction that changes the post (a trigger-free design: the same
Go code for the three dialects). A deleted message leaves the index.

### D3. Backfill without downtime

The migration creates the structures empty. A background task indexes existing posts in batches of 1 000 by id,
recording the last id done; while it has not reached the end, `Search` uses the old scan. When it finishes it sets
a flag in the database and every replica switches.

### D4. Decided while implementing

- **One table for the three dialects.** `post_search (post_id, search_text)` holds the normalized text, padded with a
  space at each end; the Go code that writes it is the same upsert everywhere. PostgreSQL indexes it through a
  generated `tsvector` column (GIN), MariaDB through a `FULLTEXT` key. On SQLite an FTS5 external-content table
  (`post_search_fts`, keyed by an integer `rid` of `post_search`, because the rowid of `posts` may change on VACUUM)
  is kept in step by three triggers — SQLite's documented mechanism for external content. This departs from D2's
  "trigger-free" wording but keeps its aim: the server code is identical across dialects.
- **Words are letters and numbers.** Everything else separates words, in messages and in queries alike, so `100%`
  is the word `100` and `snake_case` the words `snake case`. A term of several words (a quoted phrase, or one with
  punctuation inside) matches those words in sequence, each whole; a term of one word matches the beginning of a
  word. A term with no letter or number matches nothing.
- **MariaDB's fallback** covers words shorter than `innodb_ft_min_token_size` and the server's full-text stop words
  (the default list includes `de`, `la` and `en`), read once per process; a phrase with such a word is matched with
  `LIKE` on the padded text too.
- **The backfill** is a periodic task (`search:backfill`, every minute, one replica at a time through the period
  claim) that runs batches until done; each batch locks the progress row (`FOR UPDATE` on PostgreSQL and MariaDB),
  so two replicas running it at once take turns. A replica caches "done" once it sees it.
- **Highlights** in the search panel follow the same rule (beginning of a word, case and accents ignored), so a
  result found by `relatorio` shows "RELATÓRIO" marked.
- Migration `00007_search` (the next free number).

## Risks / Trade-offs

- [Substring matches people relied on are gone] → the common case (prefixes) still works; documented as a breaking
  change of search.
- [MariaDB's minimum token size] → the per-term fallback keeps two-letter terms working, slowly.
- [FTS5 missing from the SQLite build] → task 1.1 proves it first; if it is missing, SQLite keeps the scan and the
  change ships for the other two.

## Migration Plan

Additive migration, background backfill, automatic switch. Rollback: `meethub migrate down` drops the index; search
returns to the scan.
