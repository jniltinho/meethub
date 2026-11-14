-- MariaDB DDL is not transactional: every statement is re-runnable (IF [NOT] EXISTS).
-- +goose NO TRANSACTION
-- +goose Up
-- meeting recordings (add-meeting-recordings, design D3): one row per recording; at most one per channel is
-- 'recording' (the start holds the room's row lock while it checks and inserts)
CREATE TABLE IF NOT EXISTS meeting_recordings (
    id                CHAR(26)     NOT NULL PRIMARY KEY,
    channel_id        CHAR(26)     NOT NULL,
    room              VARCHAR(64)  CHARACTER SET ascii COLLATE ascii_bin NOT NULL,
    started_by        CHAR(26)     NOT NULL,
    mode              VARCHAR(8)   NOT NULL,
    status            VARCHAR(16)  NOT NULL,
    partial_reasons   VARCHAR(255) NOT NULL DEFAULT '',
    started_at        BIGINT       NOT NULL,
    ended_at          BIGINT       NOT NULL DEFAULT 0,
    duration_ms       BIGINT       NOT NULL DEFAULT 0,
    volume            VARCHAR(64)  CHARACTER SET ascii COLLATE ascii_bin NOT NULL,
    lease_until       BIGINT       NOT NULL DEFAULT 0,
    generation        BIGINT       NOT NULL DEFAULT 1,
    stop_requested_at BIGINT       NOT NULL DEFAULT 0,
    file_id           CHAR(26)     NOT NULL,
    post_id           CHAR(26)     NOT NULL DEFAULT '',
    error             VARCHAR(64)  NOT NULL DEFAULT '',
    expired_at        BIGINT       NOT NULL DEFAULT 0,
    created_at        BIGINT       NOT NULL,
    updated_at        BIGINT       NOT NULL,
    KEY meeting_recordings_channel_idx (channel_id, started_at),
    KEY meeting_recordings_status_idx (status, lease_until),
    KEY meeting_recordings_volume_idx (volume, status),
    CONSTRAINT meeting_recordings_channel_fk FOREIGN KEY (channel_id) REFERENCES channels (id) ON DELETE CASCADE
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci;

-- typed metadata of server-made posts (a recording's id, duration, mode, partial), JSON
ALTER TABLE posts ADD COLUMN IF NOT EXISTS props TEXT NULL;

-- +goose Down
ALTER TABLE posts DROP COLUMN IF EXISTS props;
DROP TABLE IF EXISTS meeting_recordings;
