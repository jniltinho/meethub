-- MariaDB DDL is not transactional: every statement is re-runnable (IF [NOT] EXISTS).
-- +goose NO TRANSACTION
-- +goose Up
-- one row per person addressed by a message (activity spec): a mention by name or a reply in their thread
CREATE TABLE IF NOT EXISTS activity_items (
    user_id    CHAR(26)    NOT NULL,
    post_id    CHAR(26)    NOT NULL,
    channel_id CHAR(26)    NOT NULL,
    kind       VARCHAR(16) NOT NULL,
    create_at  BIGINT      NOT NULL,
    PRIMARY KEY (user_id, post_id),
    KEY activity_items_user_create_idx (user_id, create_at, post_id),
    KEY activity_items_post_idx (post_id),
    KEY activity_items_create_idx (create_at),
    CONSTRAINT activity_items_user_fk FOREIGN KEY (user_id) REFERENCES users (id) ON DELETE CASCADE,
    CONSTRAINT activity_items_post_fk FOREIGN KEY (post_id) REFERENCES posts (id) ON DELETE CASCADE,
    CONSTRAINT activity_items_channel_fk FOREIGN KEY (channel_id) REFERENCES channels (id) ON DELETE CASCADE
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci;
ALTER TABLE users ADD COLUMN IF NOT EXISTS activity_seen_at BIGINT NOT NULL DEFAULT 0;

-- +goose Down
ALTER TABLE users DROP COLUMN IF EXISTS activity_seen_at;
DROP TABLE IF EXISTS activity_items;
