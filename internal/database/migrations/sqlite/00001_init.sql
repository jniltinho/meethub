-- +goose Up
CREATE TABLE IF NOT EXISTS organizations (
    id            CHAR(26)     NOT NULL PRIMARY KEY,
    singleton     INTEGER      NOT NULL DEFAULT 1 CHECK (singleton = 1),
    slug          VARCHAR(64)  NOT NULL,
    name          VARCHAR(200) NOT NULL,
    default_theme VARCHAR(64)  NOT NULL DEFAULT 'meethub-dark',
    created_at    DATETIME     NOT NULL,
    updated_at    DATETIME     NOT NULL
);
CREATE UNIQUE INDEX IF NOT EXISTS organizations_singleton_uq ON organizations (singleton);
CREATE UNIQUE INDEX IF NOT EXISTS organizations_slug_uq ON organizations (slug);

CREATE TABLE IF NOT EXISTS users (
    id               CHAR(26)     NOT NULL PRIMARY KEY,
    email            VARCHAR(320) NOT NULL,
    email_normalized VARCHAR(320) NOT NULL,
    display_name     VARCHAR(200) NOT NULL,
    password_hash    VARCHAR(255) NOT NULL,
    status           VARCHAR(16)  NOT NULL DEFAULT 'active',
    created_at       DATETIME     NOT NULL,
    updated_at       DATETIME     NOT NULL,
    last_login_at    DATETIME
);
CREATE UNIQUE INDEX IF NOT EXISTS users_email_normalized_uq ON users (email_normalized);

CREATE TABLE IF NOT EXISTS memberships (
    org_id    CHAR(26)    NOT NULL REFERENCES organizations (id) ON DELETE CASCADE,
    user_id   CHAR(26)    NOT NULL REFERENCES users (id) ON DELETE CASCADE,
    role      VARCHAR(16) NOT NULL,
    joined_at DATETIME    NOT NULL,
    PRIMARY KEY (org_id, user_id)
);
CREATE INDEX IF NOT EXISTS memberships_user_idx ON memberships (user_id);
CREATE INDEX IF NOT EXISTS memberships_org_role_idx ON memberships (org_id, role);

CREATE TABLE IF NOT EXISTS sessions (
    id           CHAR(26)     NOT NULL PRIMARY KEY,
    user_id      CHAR(26)     NOT NULL REFERENCES users (id) ON DELETE CASCADE,
    token_hash   CHAR(64)     NOT NULL,
    created_at   DATETIME     NOT NULL,
    last_seen_at DATETIME     NOT NULL,
    expires_at   DATETIME     NOT NULL,
    revoked_at   DATETIME,
    client_ip    VARCHAR(45)  NOT NULL DEFAULT '',
    user_agent   VARCHAR(255) NOT NULL DEFAULT ''
);
CREATE UNIQUE INDEX IF NOT EXISTS sessions_token_hash_uq ON sessions (token_hash);
CREATE INDEX IF NOT EXISTS sessions_user_idx ON sessions (user_id);
CREATE INDEX IF NOT EXISTS sessions_expires_idx ON sessions (expires_at);

CREATE TABLE IF NOT EXISTS user_preferences (
    user_id    CHAR(26)    NOT NULL PRIMARY KEY REFERENCES users (id) ON DELETE CASCADE,
    theme      VARCHAR(64) NOT NULL DEFAULT '',
    language   VARCHAR(16) NOT NULL DEFAULT '',
    updated_at DATETIME    NOT NULL
);

CREATE TABLE IF NOT EXISTS audit_events (
    id          CHAR(26)    NOT NULL PRIMARY KEY,
    org_id      CHAR(26),
    actor_id    CHAR(26),
    action      VARCHAR(64) NOT NULL,
    target_type VARCHAR(32) NOT NULL DEFAULT '',
    target_id   VARCHAR(64) NOT NULL DEFAULT '',
    client_ip   VARCHAR(45) NOT NULL DEFAULT '',
    details     TEXT        NOT NULL DEFAULT '{}',
    created_at  DATETIME    NOT NULL
);
CREATE INDEX IF NOT EXISTS audit_created_idx ON audit_events (created_at, id);
CREATE INDEX IF NOT EXISTS audit_action_idx ON audit_events (action, created_at, id);
CREATE INDEX IF NOT EXISTS audit_actor_idx ON audit_events (actor_id, created_at, id);

CREATE TABLE IF NOT EXISTS job_runs (
    task_type     VARCHAR(64) NOT NULL,
    period_start  DATETIME    NOT NULL,
    claimed_until DATETIME    NOT NULL,
    completed_at  DATETIME,
    attempts      INTEGER     NOT NULL DEFAULT 0,
    PRIMARY KEY (task_type, period_start)
);

-- +goose Down
DROP TABLE IF EXISTS job_runs;
DROP TABLE IF EXISTS audit_events;
DROP TABLE IF EXISTS user_preferences;
DROP TABLE IF EXISTS sessions;
DROP TABLE IF EXISTS memberships;
DROP TABLE IF EXISTS users;
DROP TABLE IF EXISTS organizations;
