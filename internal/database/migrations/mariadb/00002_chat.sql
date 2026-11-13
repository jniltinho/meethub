-- MariaDB DDL is not transactional: every statement is re-runnable (IF [NOT] EXISTS).
-- +goose NO TRANSACTION
-- +goose Up
ALTER TABLE users ADD COLUMN IF NOT EXISTS username VARCHAR(22) NULL;
ALTER TABLE users ADD COLUMN IF NOT EXISTS username_normalized VARCHAR(22) CHARACTER SET utf8mb4 COLLATE utf8mb4_bin NULL;
CREATE UNIQUE INDEX IF NOT EXISTS users_username_normalized_uq ON users (username_normalized);

CREATE TABLE IF NOT EXISTS teams (
    id           CHAR(26)     NOT NULL PRIMARY KEY,
    name         VARCHAR(64)  CHARACTER SET utf8mb4 COLLATE utf8mb4_bin NOT NULL,
    display_name VARCHAR(64)  NOT NULL,
    description  VARCHAR(255) NOT NULL DEFAULT '',
    type         VARCHAR(8)   NOT NULL DEFAULT 'open',
    is_default   TINYINT      NOT NULL DEFAULT 0,
    archived_at  BIGINT       NOT NULL DEFAULT 0,
    created_at   BIGINT       NOT NULL,
    updated_at   BIGINT       NOT NULL,
    UNIQUE KEY teams_name_uq (name)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci;

CREATE TABLE IF NOT EXISTS team_members (
    team_id    CHAR(26)    NOT NULL,
    user_id    CHAR(26)    NOT NULL,
    role       VARCHAR(16) NOT NULL DEFAULT 'team_user',
    created_at BIGINT      NOT NULL,
    PRIMARY KEY (team_id, user_id),
    KEY team_members_user_idx (user_id),
    CONSTRAINT team_members_team_fk FOREIGN KEY (team_id) REFERENCES teams (id) ON DELETE CASCADE,
    CONSTRAINT team_members_user_fk FOREIGN KEY (user_id) REFERENCES users (id) ON DELETE CASCADE
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci;

CREATE TABLE IF NOT EXISTS channels (
    id              CHAR(26)      NOT NULL PRIMARY KEY,
    team_id         VARCHAR(26)   NOT NULL DEFAULT '',
    type            CHAR(1)       NOT NULL,
    name            VARCHAR(64)   CHARACTER SET utf8mb4 COLLATE utf8mb4_bin NOT NULL,
    display_name    VARCHAR(64)   NOT NULL DEFAULT '',
    purpose         VARCHAR(250)  NOT NULL DEFAULT '',
    header          VARCHAR(1024) NOT NULL DEFAULT '',
    creator_id      VARCHAR(26)   NOT NULL DEFAULT '',
    is_default      TINYINT       NOT NULL DEFAULT 0,
    revision        BIGINT        NOT NULL DEFAULT 0,
    total_msg_count BIGINT        NOT NULL DEFAULT 0,
    last_post_at    BIGINT        NOT NULL DEFAULT 0,
    archived_at     BIGINT        NOT NULL DEFAULT 0,
    created_at      BIGINT        NOT NULL,
    updated_at      BIGINT        NOT NULL,
    UNIQUE KEY channels_team_name_uq (team_id, name)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci;

CREATE TABLE IF NOT EXISTS channel_members (
    channel_id     CHAR(26)    NOT NULL,
    user_id        CHAR(26)    NOT NULL,
    role           VARCHAR(16) NOT NULL DEFAULT 'channel_user',
    msg_count      BIGINT      NOT NULL DEFAULT 0,
    mention_count  BIGINT      NOT NULL DEFAULT 0,
    last_viewed_at BIGINT      NOT NULL DEFAULT 0,
    visible        TINYINT     NOT NULL DEFAULT 1,
    created_at     BIGINT      NOT NULL,
    PRIMARY KEY (channel_id, user_id),
    KEY channel_members_user_idx (user_id, visible),
    CONSTRAINT channel_members_channel_fk FOREIGN KEY (channel_id) REFERENCES channels (id) ON DELETE CASCADE,
    CONSTRAINT channel_members_user_fk FOREIGN KEY (user_id) REFERENCES users (id) ON DELETE CASCADE
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci;

CREATE TABLE IF NOT EXISTS posts (
    id              CHAR(26)    NOT NULL PRIMARY KEY,
    channel_id      CHAR(26)    NOT NULL,
    user_id         CHAR(26)    NOT NULL,
    root_id         VARCHAR(26) NOT NULL DEFAULT '',
    message         MEDIUMTEXT  NOT NULL,
    type            VARCHAR(32) NOT NULL DEFAULT '',
    pending_post_id VARCHAR(64) CHARACTER SET ascii COLLATE ascii_bin NULL,
    create_at       BIGINT      NOT NULL,
    update_at       BIGINT      NOT NULL,
    edit_at         BIGINT      NOT NULL DEFAULT 0,
    delete_at       BIGINT      NOT NULL DEFAULT 0,
    reply_count     BIGINT      NOT NULL DEFAULT 0,
    last_reply_at   BIGINT      NOT NULL DEFAULT 0,
    revision        BIGINT      NOT NULL DEFAULT 0,
    KEY posts_channel_create_idx (channel_id, create_at, id),
    KEY posts_channel_revision_idx (channel_id, revision),
    KEY posts_root_idx (root_id, create_at, id),
    UNIQUE KEY posts_pending_uq (user_id, pending_post_id),
    CONSTRAINT posts_channel_fk FOREIGN KEY (channel_id) REFERENCES channels (id) ON DELETE CASCADE
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci;

CREATE TABLE IF NOT EXISTS reactions (
    post_id    CHAR(26)    NOT NULL,
    user_id    CHAR(26)    NOT NULL,
    emoji_name VARCHAR(64) CHARACTER SET ascii COLLATE ascii_bin NOT NULL,
    create_at  BIGINT      NOT NULL,
    PRIMARY KEY (post_id, user_id, emoji_name),
    CONSTRAINT reactions_post_fk FOREIGN KEY (post_id) REFERENCES posts (id) ON DELETE CASCADE
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci;

-- +goose Down
DROP TABLE IF EXISTS reactions;
DROP TABLE IF EXISTS posts;
DROP TABLE IF EXISTS channel_members;
DROP TABLE IF EXISTS channels;
DROP TABLE IF EXISTS team_members;
DROP TABLE IF EXISTS teams;
DROP INDEX IF EXISTS users_username_normalized_uq ON users;
ALTER TABLE users DROP COLUMN IF EXISTS username_normalized;
ALTER TABLE users DROP COLUMN IF EXISTS username;
