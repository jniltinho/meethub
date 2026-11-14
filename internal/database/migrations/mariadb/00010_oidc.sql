-- MariaDB DDL is not transactional: every statement is re-runnable (IF [NOT] EXISTS).
-- +goose NO TRANSACTION
-- +goose Up
-- single sign-on: the provider's subject (sub), stored on a user's first OpenID Connect login; later logins must match
ALTER TABLE users ADD COLUMN IF NOT EXISTS oidc_subject VARCHAR(255) CHARACTER SET utf8mb4 COLLATE utf8mb4_bin NULL;
CREATE UNIQUE INDEX IF NOT EXISTS users_oidc_subject_uq ON users (oidc_subject);

-- +goose Down
DROP INDEX IF EXISTS users_oidc_subject_uq ON users;
ALTER TABLE users DROP COLUMN IF EXISTS oidc_subject;
