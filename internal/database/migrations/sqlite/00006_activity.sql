-- +goose Up
-- one row per person addressed by a message (activity spec): a mention by name or a reply in their thread
CREATE TABLE IF NOT EXISTS activity_items (
    user_id    CHAR(26)    NOT NULL REFERENCES users (id) ON DELETE CASCADE,
    post_id    CHAR(26)    NOT NULL REFERENCES posts (id) ON DELETE CASCADE,
    channel_id CHAR(26)    NOT NULL REFERENCES channels (id) ON DELETE CASCADE,
    kind       VARCHAR(16) NOT NULL,
    create_at  BIGINT      NOT NULL,
    PRIMARY KEY (user_id, post_id)
);
CREATE INDEX IF NOT EXISTS activity_items_user_create_idx ON activity_items (user_id, create_at, post_id);
CREATE INDEX IF NOT EXISTS activity_items_post_idx ON activity_items (post_id);
CREATE INDEX IF NOT EXISTS activity_items_create_idx ON activity_items (create_at);
-- the seen mark: the newest item (time, then post id) when the person last opened Activity
ALTER TABLE users ADD COLUMN activity_seen_at BIGINT NOT NULL DEFAULT 0;
ALTER TABLE users ADD COLUMN activity_seen_post CHAR(26) NOT NULL DEFAULT '';

-- +goose Down
ALTER TABLE users DROP COLUMN activity_seen_post;
ALTER TABLE users DROP COLUMN activity_seen_at;
DROP TABLE IF EXISTS activity_items;
