-- +goose Up
-- authenticator apps (TOTP, RFC 6238): the secret sealed with AES-256-GCM under MEETHUB_SECRET_KEY; confirmed_at is
-- 0 while the enrollment waits for its first code; last_step is the last time step accepted (each code works once)
CREATE TABLE IF NOT EXISTS user_second_factors (
    user_id      CHAR(26)     NOT NULL PRIMARY KEY REFERENCES users (id) ON DELETE CASCADE,
    secret       VARCHAR(255) NOT NULL,
    confirmed_at BIGINT       NOT NULL DEFAULT 0,
    last_step    BIGINT       NOT NULL DEFAULT 0
);

-- one-time recovery codes, Argon2id hashes; a used code is deleted
CREATE TABLE IF NOT EXISTS user_recovery_codes (
    id        CHAR(26)     NOT NULL PRIMARY KEY,
    user_id   CHAR(26)     NOT NULL REFERENCES users (id) ON DELETE CASCADE,
    code_hash VARCHAR(255) NOT NULL
);
CREATE INDEX IF NOT EXISTS user_recovery_codes_user_idx ON user_recovery_codes (user_id);

-- +goose Down
DROP TABLE IF EXISTS user_recovery_codes;
DROP TABLE IF EXISTS user_second_factors;
