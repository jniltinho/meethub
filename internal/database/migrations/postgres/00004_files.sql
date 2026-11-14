-- +goose Up
CREATE TABLE IF NOT EXISTS file_infos (
    id                CHAR(26)     NOT NULL PRIMARY KEY,
    creator_id        CHAR(26)     NOT NULL,
    channel_id        CHAR(26)     NOT NULL REFERENCES channels (id) ON DELETE CASCADE,
    post_id           CHAR(26),
    position          INTEGER      NOT NULL DEFAULT 0,
    name              VARCHAR(255) NOT NULL,
    search_name       VARCHAR(255) NOT NULL,
    extension         VARCHAR(32)  NOT NULL DEFAULT '',
    size              BIGINT       NOT NULL,
    mime_type         VARCHAR(127) NOT NULL DEFAULT '',
    width             INTEGER      NOT NULL DEFAULT 0,
    height            INTEGER      NOT NULL DEFAULT 0,
    has_preview_image BOOLEAN      NOT NULL DEFAULT FALSE,
    path              VARCHAR(255) NOT NULL,
    thumbnail_path    VARCHAR(255) NOT NULL DEFAULT '',
    preview_path      VARCHAR(255) NOT NULL DEFAULT '',
    create_at         BIGINT       NOT NULL,
    update_at         BIGINT       NOT NULL,
    delete_at         BIGINT       NOT NULL DEFAULT 0
);
CREATE INDEX IF NOT EXISTS file_infos_channel_idx ON file_infos (channel_id, create_at, id);
CREATE INDEX IF NOT EXISTS file_infos_post_idx ON file_infos (post_id, position);
CREATE INDEX IF NOT EXISTS file_infos_cleanup_idx ON file_infos (delete_at, create_at);

-- +goose Down
DROP TABLE IF EXISTS file_infos;
