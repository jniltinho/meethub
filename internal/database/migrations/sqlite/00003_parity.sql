-- +goose Up
ALTER TABLE posts ADD COLUMN pinned_at BIGINT NOT NULL DEFAULT 0;
ALTER TABLE posts ADD COLUMN pinned_by CHAR(26);
CREATE INDEX IF NOT EXISTS posts_channel_pinned_idx ON posts (channel_id, pinned_at);
ALTER TABLE channel_members ADD COLUMN favorite INTEGER NOT NULL DEFAULT 0;
ALTER TABLE users ADD COLUMN last_picture_update BIGINT NOT NULL DEFAULT 0;

CREATE TABLE IF NOT EXISTS user_images (
    user_id    CHAR(26) NOT NULL PRIMARY KEY REFERENCES users (id) ON DELETE CASCADE,
    data       BLOB     NOT NULL,
    updated_at BIGINT   NOT NULL
);

-- +goose Down
DROP TABLE IF EXISTS user_images;
ALTER TABLE users DROP COLUMN last_picture_update;
ALTER TABLE channel_members DROP COLUMN favorite;
DROP INDEX IF EXISTS posts_channel_pinned_idx;
ALTER TABLE posts DROP COLUMN pinned_by;
ALTER TABLE posts DROP COLUMN pinned_at;
