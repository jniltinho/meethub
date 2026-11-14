-- MariaDB DDL is not transactional: every statement is re-runnable (IF [NOT] EXISTS).
-- +goose NO TRANSACTION
-- +goose Up
-- notification preferences (notifications spec): '' is the default (mentions)
ALTER TABLE user_preferences ADD COLUMN IF NOT EXISTS desktop_notifications VARCHAR(16) NOT NULL DEFAULT '';
ALTER TABLE user_preferences ADD COLUMN IF NOT EXISTS email_notifications VARCHAR(16) NOT NULL DEFAULT '';

-- direct messages and direct mentions waiting for a notification e-mail
CREATE TABLE IF NOT EXISTS notification_queue (
    user_id   CHAR(26) NOT NULL,
    post_id   CHAR(26) NOT NULL,
    create_at BIGINT   NOT NULL,
    PRIMARY KEY (user_id, post_id),
    KEY notification_queue_create_idx (create_at),
    CONSTRAINT notification_queue_user_fk FOREIGN KEY (user_id) REFERENCES users (id) ON DELETE CASCADE,
    CONSTRAINT notification_queue_post_fk FOREIGN KEY (post_id) REFERENCES posts (id) ON DELETE CASCADE
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci;

-- when each person was last e-mailed (one e-mail every 15 minutes at most)
CREATE TABLE IF NOT EXISTS notification_state (
    user_id       CHAR(26) NOT NULL PRIMARY KEY,
    last_email_at BIGINT   NOT NULL DEFAULT 0,
    CONSTRAINT notification_state_user_fk FOREIGN KEY (user_id) REFERENCES users (id) ON DELETE CASCADE
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci;

-- the unsubscribe links of the e-mails sent, by the SHA-256 of their token
CREATE TABLE IF NOT EXISTS notification_unsubscribe (
    token_hash CHAR(64) CHARACTER SET ascii COLLATE ascii_bin NOT NULL PRIMARY KEY,
    user_id    CHAR(26) NOT NULL,
    create_at  BIGINT   NOT NULL,
    KEY notification_unsubscribe_user_idx (user_id),
    CONSTRAINT notification_unsubscribe_user_fk FOREIGN KEY (user_id) REFERENCES users (id) ON DELETE CASCADE
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci;

-- +goose Down
DROP TABLE IF EXISTS notification_unsubscribe;
DROP TABLE IF EXISTS notification_state;
DROP TABLE IF EXISTS notification_queue;
ALTER TABLE user_preferences DROP COLUMN IF EXISTS email_notifications;
ALTER TABLE user_preferences DROP COLUMN IF EXISTS desktop_notifications;
