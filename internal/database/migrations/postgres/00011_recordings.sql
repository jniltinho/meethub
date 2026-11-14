-- +goose Up
-- meeting recordings (add-meeting-recordings, design D3): one row per recording; at most one per channel is
-- 'recording' (the start holds the room's row lock while it checks and inserts)
CREATE TABLE IF NOT EXISTS meeting_recordings (
    id                CHAR(26)     NOT NULL PRIMARY KEY,
    channel_id        CHAR(26)     NOT NULL REFERENCES channels (id) ON DELETE CASCADE,
    room              VARCHAR(64)  NOT NULL,
    started_by        CHAR(26)     NOT NULL,
    mode              VARCHAR(8)   NOT NULL,
    status            VARCHAR(16)  NOT NULL,
    partial_reasons   VARCHAR(255) NOT NULL DEFAULT '',
    started_at        BIGINT       NOT NULL,
    ended_at          BIGINT       NOT NULL DEFAULT 0,
    duration_ms       BIGINT       NOT NULL DEFAULT 0,
    volume            VARCHAR(64)  NOT NULL,
    lease_until       BIGINT       NOT NULL DEFAULT 0,
    generation        BIGINT       NOT NULL DEFAULT 1,
    stop_requested_at BIGINT       NOT NULL DEFAULT 0,
    file_id           CHAR(26)     NOT NULL,
    post_id           CHAR(26)     NOT NULL DEFAULT '',
    error             VARCHAR(64)  NOT NULL DEFAULT '',
    expired_at        BIGINT       NOT NULL DEFAULT 0,
    created_at        BIGINT       NOT NULL,
    updated_at        BIGINT       NOT NULL
);
CREATE INDEX IF NOT EXISTS meeting_recordings_channel_idx ON meeting_recordings (channel_id, started_at);
CREATE INDEX IF NOT EXISTS meeting_recordings_status_idx ON meeting_recordings (status, lease_until);
CREATE INDEX IF NOT EXISTS meeting_recordings_volume_idx ON meeting_recordings (volume, status);

-- typed metadata of server-made posts (a recording's id, duration, mode, partial), JSON
ALTER TABLE posts ADD COLUMN IF NOT EXISTS props TEXT;

-- +goose Down
ALTER TABLE posts DROP COLUMN IF EXISTS props;
DROP TABLE IF EXISTS meeting_recordings;
