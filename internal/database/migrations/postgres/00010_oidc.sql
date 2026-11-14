-- +goose Up
-- single sign-on: the provider's subject (sub), stored on a user's first OpenID Connect login; later logins must match
ALTER TABLE users ADD COLUMN IF NOT EXISTS oidc_subject VARCHAR(255) COLLATE "C" NULL;
CREATE UNIQUE INDEX IF NOT EXISTS users_oidc_subject_uq ON users (oidc_subject);

-- +goose Down
DROP INDEX IF EXISTS users_oidc_subject_uq;
ALTER TABLE users DROP COLUMN IF EXISTS oidc_subject;
