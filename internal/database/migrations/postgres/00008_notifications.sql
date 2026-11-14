-- +goose Up
-- notification preferences (notifications spec): '' is the default (mentions)
ALTER TABLE user_preferences ADD COLUMN IF NOT EXISTS desktop_notifications VARCHAR(16) NOT NULL DEFAULT '';
ALTER TABLE user_preferences ADD COLUMN IF NOT EXISTS email_notifications VARCHAR(16) NOT NULL DEFAULT '';

-- direct messages and direct mentions waiting for a notification e-mail
CREATE TABLE IF NOT EXISTS notification_queue (
    user_id   CHAR(26) NOT NULL REFERENCES users (id) ON DELETE CASCADE,
    post_id   CHAR(26) NOT NULL REFERENCES posts (id) ON DELETE CASCADE,
    create_at BIGINT   NOT NULL,
    PRIMARY KEY (user_id, post_id)
);
CREATE INDEX IF NOT EXISTS notification_queue_create_idx ON notification_queue (create_at);

-- when each person was last e-mailed (one e-mail every 15 minutes at most)
CREATE TABLE IF NOT EXISTS notification_state (
    user_id       CHAR(26) NOT NULL PRIMARY KEY REFERENCES users (id) ON DELETE CASCADE,
    last_email_at BIGINT   NOT NULL DEFAULT 0
);

-- the unsubscribe links of the e-mails sent, by the SHA-256 of their token
CREATE TABLE IF NOT EXISTS notification_unsubscribe (
    token_hash CHAR(64) NOT NULL PRIMARY KEY,
    user_id    CHAR(26) NOT NULL REFERENCES users (id) ON DELETE CASCADE,
    create_at  BIGINT   NOT NULL
);
CREATE INDEX IF NOT EXISTS notification_unsubscribe_user_idx ON notification_unsubscribe (user_id);

-- +goose Down
DROP TABLE IF EXISTS notification_unsubscribe;
DROP TABLE IF EXISTS notification_state;
DROP TABLE IF EXISTS notification_queue;
ALTER TABLE user_preferences DROP COLUMN IF EXISTS email_notifications;
ALTER TABLE user_preferences DROP COLUMN IF EXISTS desktop_notifications;
