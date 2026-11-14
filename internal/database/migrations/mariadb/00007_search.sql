-- MariaDB DDL is not transactional: every statement is re-runnable (IF [NOT] EXISTS, INSERT IGNORE).
-- +goose NO TRANSACTION
-- +goose Up
-- the search text of each message (message-search spec "Search index"), kept by the server in the writing
-- transaction, under a FULLTEXT index (terms shorter than its minimum token size, or among its stop words, are
-- matched by the server with LIKE on the same text)
CREATE TABLE IF NOT EXISTS post_search (
    post_id     CHAR(26)   NOT NULL PRIMARY KEY,
    search_text MEDIUMTEXT NOT NULL,
    FULLTEXT KEY post_search_ft (search_text),
    CONSTRAINT post_search_post_fk FOREIGN KEY (post_id) REFERENCES posts (id) ON DELETE CASCADE
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci;

-- the background indexing of the messages stored before this migration: search scans until it is done
CREATE TABLE IF NOT EXISTS search_backfill (
    name    VARCHAR(32) NOT NULL PRIMARY KEY,
    last_id CHAR(26)    NOT NULL DEFAULT '',
    done    SMALLINT    NOT NULL DEFAULT 0
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci;
INSERT IGNORE INTO search_backfill (name, last_id, done)
    SELECT 'posts', '', CASE WHEN EXISTS (SELECT 1 FROM posts) THEN 0 ELSE 1 END;

-- +goose Down
DROP TABLE IF EXISTS search_backfill;
DROP TABLE IF EXISTS post_search;
