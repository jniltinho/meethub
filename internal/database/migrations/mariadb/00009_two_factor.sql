-- MariaDB DDL is not transactional: every statement is re-runnable (IF [NOT] EXISTS).
-- +goose NO TRANSACTION
-- +goose Up
-- authenticator apps (TOTP, RFC 6238): the secret sealed with AES-256-GCM under MEETHUB_SECRET_KEY; confirmed_at is
-- 0 while the enrollment waits for its first code; last_step is the last time step accepted (each code works once)
CREATE TABLE IF NOT EXISTS user_second_factors (
    user_id      CHAR(26)     NOT NULL PRIMARY KEY,
    secret       VARCHAR(255) CHARACTER SET ascii COLLATE ascii_bin NOT NULL,
    confirmed_at BIGINT       NOT NULL DEFAULT 0,
    last_step    BIGINT       NOT NULL DEFAULT 0,
    CONSTRAINT user_second_factors_user_fk FOREIGN KEY (user_id) REFERENCES users (id) ON DELETE CASCADE
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci;

-- one-time recovery codes, Argon2id hashes; a used code is deleted
CREATE TABLE IF NOT EXISTS user_recovery_codes (
    id        CHAR(26)     NOT NULL PRIMARY KEY,
    user_id   CHAR(26)     NOT NULL,
    code_hash VARCHAR(255) CHARACTER SET ascii COLLATE ascii_bin NOT NULL,
    KEY user_recovery_codes_user_idx (user_id),
    CONSTRAINT user_recovery_codes_user_fk FOREIGN KEY (user_id) REFERENCES users (id) ON DELETE CASCADE
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci;

-- +goose Down
DROP TABLE IF EXISTS user_recovery_codes;
DROP TABLE IF EXISTS user_second_factors;
