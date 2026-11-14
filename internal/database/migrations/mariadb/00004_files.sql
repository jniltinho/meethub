-- MariaDB DDL is not transactional: every statement is re-runnable (IF [NOT] EXISTS).
-- +goose NO TRANSACTION
-- +goose Up
CREATE TABLE IF NOT EXISTS file_infos (
    id                CHAR(26)     NOT NULL PRIMARY KEY,
    creator_id        CHAR(26)     NOT NULL,
    channel_id        CHAR(26)     NOT NULL,
    post_id           CHAR(26)     NULL,
    position          INT          NOT NULL DEFAULT 0,
    name              VARCHAR(255) NOT NULL,
    search_name       VARCHAR(255) NOT NULL,
    extension         VARCHAR(32)  NOT NULL DEFAULT '',
    size              BIGINT       NOT NULL,
    mime_type         VARCHAR(127) NOT NULL DEFAULT '',
    width             INT          NOT NULL DEFAULT 0,
    height            INT          NOT NULL DEFAULT 0,
    has_preview_image TINYINT      NOT NULL DEFAULT 0,
    path              VARCHAR(255) NOT NULL,
    thumbnail_path    VARCHAR(255) NOT NULL DEFAULT '',
    preview_path      VARCHAR(255) NOT NULL DEFAULT '',
    create_at         BIGINT       NOT NULL,
    update_at         BIGINT       NOT NULL,
    delete_at         BIGINT       NOT NULL DEFAULT 0,
    KEY file_infos_channel_idx (channel_id, create_at, id),
    KEY file_infos_post_idx (post_id, position),
    KEY file_infos_cleanup_idx (delete_at, create_at),
    CONSTRAINT file_infos_channel_fk FOREIGN KEY (channel_id) REFERENCES channels (id) ON DELETE CASCADE
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci;

-- +goose Down
DROP TABLE IF EXISTS file_infos;
