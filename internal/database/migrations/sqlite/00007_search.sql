-- +goose Up
-- the search text of each message (message-search spec "Search index"), kept by the server in the writing
-- transaction; FTS5 indexes it as an external-content table kept in step by the triggers below
CREATE TABLE IF NOT EXISTS post_search (
    rid         INTEGER  PRIMARY KEY,
    post_id     CHAR(26) NOT NULL UNIQUE REFERENCES posts (id) ON DELETE CASCADE,
    search_text TEXT     NOT NULL
);
CREATE VIRTUAL TABLE IF NOT EXISTS post_search_fts USING fts5(
    search_text, content='post_search', content_rowid='rid', tokenize='unicode61 remove_diacritics 0'
);
-- +goose StatementBegin
CREATE TRIGGER IF NOT EXISTS post_search_ai AFTER INSERT ON post_search BEGIN
    INSERT INTO post_search_fts (rowid, search_text) VALUES (new.rid, new.search_text);
END;
-- +goose StatementEnd
-- +goose StatementBegin
CREATE TRIGGER IF NOT EXISTS post_search_ad AFTER DELETE ON post_search BEGIN
    INSERT INTO post_search_fts (post_search_fts, rowid, search_text) VALUES ('delete', old.rid, old.search_text);
END;
-- +goose StatementEnd
-- +goose StatementBegin
CREATE TRIGGER IF NOT EXISTS post_search_au AFTER UPDATE ON post_search BEGIN
    INSERT INTO post_search_fts (post_search_fts, rowid, search_text) VALUES ('delete', old.rid, old.search_text);
    INSERT INTO post_search_fts (rowid, search_text) VALUES (new.rid, new.search_text);
END;
-- +goose StatementEnd

-- the background indexing of the messages stored before this migration: search scans until it is done
CREATE TABLE IF NOT EXISTS search_backfill (
    name    VARCHAR(32) NOT NULL PRIMARY KEY,
    last_id CHAR(26)    NOT NULL DEFAULT '',
    done    SMALLINT    NOT NULL DEFAULT 0
);
INSERT INTO search_backfill (name, last_id, done)
    SELECT 'posts', '', CASE WHEN EXISTS (SELECT 1 FROM posts) THEN 0 ELSE 1 END;

-- +goose Down
DROP TABLE IF EXISTS search_backfill;
DROP TRIGGER IF EXISTS post_search_au;
DROP TRIGGER IF EXISTS post_search_ad;
DROP TRIGGER IF EXISTS post_search_ai;
DROP TABLE IF EXISTS post_search_fts;
DROP TABLE IF EXISTS post_search;
