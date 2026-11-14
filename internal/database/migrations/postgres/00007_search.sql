-- +goose Up
-- the search text of each message (message-search spec "Search index"), kept by the server in the writing
-- transaction; a tsvector of the 'simple' configuration (no stemming, no stop words) under a GIN index
CREATE TABLE IF NOT EXISTS post_search (
    post_id     CHAR(26) NOT NULL PRIMARY KEY REFERENCES posts (id) ON DELETE CASCADE,
    search_text TEXT     NOT NULL,
    tsv         tsvector GENERATED ALWAYS AS (to_tsvector('simple', search_text)) STORED
);
CREATE INDEX IF NOT EXISTS post_search_tsv_idx ON post_search USING GIN (tsv);

-- the background indexing of the messages stored before this migration: search scans until it is done
CREATE TABLE IF NOT EXISTS search_backfill (
    name    VARCHAR(32) NOT NULL PRIMARY KEY,
    last_id CHAR(26)    NOT NULL DEFAULT '',
    done    SMALLINT    NOT NULL DEFAULT 0
);
INSERT INTO search_backfill (name, last_id, done)
    SELECT 'posts', '', CASE WHEN EXISTS (SELECT 1 FROM posts) THEN 0 ELSE 1 END
    ON CONFLICT (name) DO NOTHING;

-- +goose Down
DROP TABLE IF EXISTS search_backfill;
DROP TABLE IF EXISTS post_search;
