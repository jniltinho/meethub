-- MariaDB DDL is not transactional: every statement is re-runnable (IF [NOT] EXISTS)
-- so a migration interrupted half-way can simply be run again.
-- +goose NO TRANSACTION
-- +goose Up
CREATE TABLE IF NOT EXISTS organizations (
    id            CHAR(26)     NOT NULL PRIMARY KEY,
    singleton     TINYINT      NOT NULL DEFAULT 1 CHECK (singleton = 1),
    slug          VARCHAR(64)  NOT NULL,
    name          VARCHAR(200) NOT NULL,
    default_theme VARCHAR(64)  NOT NULL DEFAULT 'meethub-dark',
    created_at    DATETIME(6)  NOT NULL,
    updated_at    DATETIME(6)  NOT NULL,
    UNIQUE KEY organizations_singleton_uq (singleton),
    UNIQUE KEY organizations_slug_uq (slug)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci;

CREATE TABLE IF NOT EXISTS users (
    id               CHAR(26)     NOT NULL PRIMARY KEY,
    email            VARCHAR(320) NOT NULL,
    email_normalized VARCHAR(320) CHARACTER SET utf8mb4 COLLATE utf8mb4_bin NOT NULL,
    display_name     VARCHAR(200) NOT NULL,
    password_hash    VARCHAR(255) NOT NULL,
    status           VARCHAR(16)  NOT NULL DEFAULT 'active',
    created_at       DATETIME(6)  NOT NULL,
    updated_at       DATETIME(6)  NOT NULL,
    last_login_at    DATETIME(6)  NULL,
    UNIQUE KEY users_email_normalized_uq (email_normalized)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci;

CREATE TABLE IF NOT EXISTS memberships (
    org_id    CHAR(26)    NOT NULL,
    user_id   CHAR(26)    NOT NULL,
    role      VARCHAR(16) NOT NULL,
    joined_at DATETIME(6) NOT NULL,
    PRIMARY KEY (org_id, user_id),
    KEY memberships_user_idx (user_id),
    KEY memberships_org_role_idx (org_id, role),
    CONSTRAINT memberships_org_fk FOREIGN KEY (org_id) REFERENCES organizations (id) ON DELETE CASCADE,
    CONSTRAINT memberships_user_fk FOREIGN KEY (user_id) REFERENCES users (id) ON DELETE CASCADE
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci;

CREATE TABLE IF NOT EXISTS sessions (
    id           CHAR(26)     NOT NULL PRIMARY KEY,
    user_id      CHAR(26)     NOT NULL,
    token_hash   CHAR(64)     CHARACTER SET ascii COLLATE ascii_bin NOT NULL,
    created_at   DATETIME(6)  NOT NULL,
    last_seen_at DATETIME(6)  NOT NULL,
    expires_at   DATETIME(6)  NOT NULL,
    revoked_at   DATETIME(6)  NULL,
    client_ip    VARCHAR(45)  NOT NULL DEFAULT '',
    user_agent   VARCHAR(255) NOT NULL DEFAULT '',
    UNIQUE KEY sessions_token_hash_uq (token_hash),
    KEY sessions_user_idx (user_id),
    KEY sessions_expires_idx (expires_at),
    CONSTRAINT sessions_user_fk FOREIGN KEY (user_id) REFERENCES users (id) ON DELETE CASCADE
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci;

CREATE TABLE IF NOT EXISTS user_preferences (
    user_id    CHAR(26)    NOT NULL PRIMARY KEY,
    theme      VARCHAR(64) NOT NULL DEFAULT '',
    language   VARCHAR(16) NOT NULL DEFAULT '',
    updated_at DATETIME(6) NOT NULL,
    CONSTRAINT user_preferences_user_fk FOREIGN KEY (user_id) REFERENCES users (id) ON DELETE CASCADE
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci;

CREATE TABLE IF NOT EXISTS audit_events (
    id          CHAR(26)    NOT NULL PRIMARY KEY,
    org_id      CHAR(26)    NULL,
    actor_id    CHAR(26)    NULL,
    action      VARCHAR(64) NOT NULL,
    target_type VARCHAR(32) NOT NULL DEFAULT '',
    target_id   VARCHAR(64) NOT NULL DEFAULT '',
    client_ip   VARCHAR(45) NOT NULL DEFAULT '',
    details     TEXT        NOT NULL,
    created_at  DATETIME(6) NOT NULL,
    KEY audit_created_idx (created_at, id),
    KEY audit_action_idx (action, created_at, id),
    KEY audit_actor_idx (actor_id, created_at, id)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci;

CREATE TABLE IF NOT EXISTS job_runs (
    task_type     VARCHAR(64) NOT NULL,
    period_start  DATETIME(6) NOT NULL,
    claimed_until DATETIME(6) NOT NULL,
    completed_at  DATETIME(6) NULL,
    attempts      INT         NOT NULL DEFAULT 0,
    PRIMARY KEY (task_type, period_start)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci;

-- +goose Down
DROP TABLE IF EXISTS job_runs;
DROP TABLE IF EXISTS audit_events;
DROP TABLE IF EXISTS user_preferences;
DROP TABLE IF EXISTS sessions;
DROP TABLE IF EXISTS memberships;
DROP TABLE IF EXISTS users;
DROP TABLE IF EXISTS organizations;
