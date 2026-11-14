-- MariaDB DDL is not transactional: every statement is re-runnable (IF [NOT] EXISTS).
-- +goose NO TRANSACTION
-- +goose Up
CREATE TABLE IF NOT EXISTS meeting_rooms (
    channel_id CHAR(26)    NOT NULL PRIMARY KEY,
    name       VARCHAR(64) CHARACTER SET ascii COLLATE ascii_bin NOT NULL,
    revision   BIGINT      NOT NULL DEFAULT 0,
    created_at BIGINT      NOT NULL,
    UNIQUE KEY meeting_rooms_name_uq (name),
    CONSTRAINT meeting_rooms_channel_fk FOREIGN KEY (channel_id) REFERENCES channels (id) ON DELETE CASCADE
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci;

CREATE TABLE IF NOT EXISTS meeting_participants (
    channel_id CHAR(26)    NOT NULL,
    user_id    CHAR(26)    NOT NULL,
    sid        VARCHAR(64) CHARACTER SET ascii COLLATE ascii_bin NOT NULL,
    joined_at  BIGINT      NOT NULL,
    PRIMARY KEY (channel_id, user_id),
    CONSTRAINT meeting_participants_room_fk FOREIGN KEY (channel_id) REFERENCES meeting_rooms (channel_id) ON DELETE CASCADE
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci;

-- +goose Down
DROP TABLE IF EXISTS meeting_participants;
DROP TABLE IF EXISTS meeting_rooms;
