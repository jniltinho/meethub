-- +goose Up
CREATE TABLE IF NOT EXISTS meeting_rooms (
    channel_id CHAR(26)    NOT NULL PRIMARY KEY REFERENCES channels (id) ON DELETE CASCADE,
    name       VARCHAR(64) NOT NULL,
    revision   BIGINT      NOT NULL DEFAULT 0,
    created_at BIGINT      NOT NULL
);
CREATE UNIQUE INDEX IF NOT EXISTS meeting_rooms_name_uq ON meeting_rooms (name);

CREATE TABLE IF NOT EXISTS meeting_participants (
    channel_id CHAR(26)    NOT NULL REFERENCES meeting_rooms (channel_id) ON DELETE CASCADE,
    user_id    CHAR(26)    NOT NULL,
    sid        VARCHAR(64) NOT NULL,
    joined_at  BIGINT      NOT NULL,
    PRIMARY KEY (channel_id, user_id)
);

-- +goose Down
DROP TABLE IF EXISTS meeting_participants;
DROP TABLE IF EXISTS meeting_rooms;
