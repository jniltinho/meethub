-- +goose Up
ALTER TABLE users ADD COLUMN IF NOT EXISTS username VARCHAR(22);
ALTER TABLE users ADD COLUMN IF NOT EXISTS username_normalized VARCHAR(22) COLLATE "C";
CREATE UNIQUE INDEX IF NOT EXISTS users_username_normalized_uq ON users (username_normalized);

CREATE TABLE IF NOT EXISTS teams (
    id           CHAR(26)     NOT NULL PRIMARY KEY,
    name         VARCHAR(64)  NOT NULL,
    display_name VARCHAR(64)  NOT NULL,
    description  VARCHAR(255) NOT NULL DEFAULT '',
    type         VARCHAR(8)   NOT NULL DEFAULT 'open',
    is_default   INTEGER      NOT NULL DEFAULT 0,
    archived_at  BIGINT       NOT NULL DEFAULT 0,
    created_at   BIGINT       NOT NULL,
    updated_at   BIGINT       NOT NULL
);
CREATE UNIQUE INDEX IF NOT EXISTS teams_name_uq ON teams (name);

CREATE TABLE IF NOT EXISTS team_members (
    team_id    CHAR(26)    NOT NULL REFERENCES teams (id) ON DELETE CASCADE,
    user_id    CHAR(26)    NOT NULL REFERENCES users (id) ON DELETE CASCADE,
    role       VARCHAR(16) NOT NULL DEFAULT 'team_user',
    created_at BIGINT      NOT NULL,
    PRIMARY KEY (team_id, user_id)
);
CREATE INDEX IF NOT EXISTS team_members_user_idx ON team_members (user_id);

CREATE TABLE IF NOT EXISTS channels (
    id              CHAR(26)      NOT NULL PRIMARY KEY,
    team_id         VARCHAR(26)   NOT NULL DEFAULT '',
    type            CHAR(1)       NOT NULL,
    name            VARCHAR(64)   NOT NULL,
    display_name    VARCHAR(64)   NOT NULL DEFAULT '',
    purpose         VARCHAR(250)  NOT NULL DEFAULT '',
    header          VARCHAR(1024) NOT NULL DEFAULT '',
    creator_id      VARCHAR(26)   NOT NULL DEFAULT '',
    is_default      INTEGER       NOT NULL DEFAULT 0,
    revision        BIGINT        NOT NULL DEFAULT 0,
    total_msg_count BIGINT        NOT NULL DEFAULT 0,
    last_post_at    BIGINT        NOT NULL DEFAULT 0,
    archived_at     BIGINT        NOT NULL DEFAULT 0,
    created_at      BIGINT        NOT NULL,
    updated_at      BIGINT        NOT NULL
);
CREATE UNIQUE INDEX IF NOT EXISTS channels_team_name_uq ON channels (team_id, name);

CREATE TABLE IF NOT EXISTS channel_members (
    channel_id     CHAR(26)    NOT NULL REFERENCES channels (id) ON DELETE CASCADE,
    user_id        CHAR(26)    NOT NULL REFERENCES users (id) ON DELETE CASCADE,
    role           VARCHAR(16) NOT NULL DEFAULT 'channel_user',
    msg_count      BIGINT      NOT NULL DEFAULT 0,
    mention_count  BIGINT      NOT NULL DEFAULT 0,
    last_viewed_at BIGINT      NOT NULL DEFAULT 0,
    visible        INTEGER     NOT NULL DEFAULT 1,
    created_at     BIGINT      NOT NULL,
    PRIMARY KEY (channel_id, user_id)
);
CREATE INDEX IF NOT EXISTS channel_members_user_idx ON channel_members (user_id, visible);

CREATE TABLE IF NOT EXISTS posts (
    id              CHAR(26)    NOT NULL PRIMARY KEY,
    channel_id      CHAR(26)    NOT NULL REFERENCES channels (id) ON DELETE CASCADE,
    user_id         CHAR(26)    NOT NULL,
    root_id         VARCHAR(26) NOT NULL DEFAULT '',
    message         TEXT        NOT NULL,
    type            VARCHAR(32) NOT NULL DEFAULT '',
    pending_post_id VARCHAR(64),
    create_at       BIGINT      NOT NULL,
    update_at       BIGINT      NOT NULL,
    edit_at         BIGINT      NOT NULL DEFAULT 0,
    delete_at       BIGINT      NOT NULL DEFAULT 0,
    reply_count     BIGINT      NOT NULL DEFAULT 0,
    last_reply_at   BIGINT      NOT NULL DEFAULT 0,
    revision        BIGINT      NOT NULL DEFAULT 0
);
CREATE INDEX IF NOT EXISTS posts_channel_create_idx ON posts (channel_id, create_at, id);
CREATE INDEX IF NOT EXISTS posts_channel_revision_idx ON posts (channel_id, revision);
CREATE INDEX IF NOT EXISTS posts_root_idx ON posts (root_id, create_at, id);
CREATE UNIQUE INDEX IF NOT EXISTS posts_pending_uq ON posts (user_id, pending_post_id);

CREATE TABLE IF NOT EXISTS reactions (
    post_id    CHAR(26)    NOT NULL REFERENCES posts (id) ON DELETE CASCADE,
    user_id    CHAR(26)    NOT NULL,
    emoji_name VARCHAR(64) NOT NULL,
    create_at  BIGINT      NOT NULL,
    PRIMARY KEY (post_id, user_id, emoji_name)
);

-- +goose Down
DROP TABLE IF EXISTS reactions;
DROP TABLE IF EXISTS posts;
DROP TABLE IF EXISTS channel_members;
DROP TABLE IF EXISTS channels;
DROP TABLE IF EXISTS team_members;
DROP TABLE IF EXISTS teams;
DROP INDEX IF EXISTS users_username_normalized_uq;
ALTER TABLE users DROP COLUMN IF EXISTS username_normalized;
ALTER TABLE users DROP COLUMN IF EXISTS username;
