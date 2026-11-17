-- +goose Up
-- the AI keys entered in Administration (edit-ai-and-recording-settings, design D1): sealed with MEETHUB_SECRET_KEY,
-- '' when not set
ALTER TABLE organizations ADD COLUMN IF NOT EXISTS ai_api_key VARCHAR(1024) NOT NULL DEFAULT '';
ALTER TABLE organizations ADD COLUMN IF NOT EXISTS ai_transcription_api_key VARCHAR(1024) NOT NULL DEFAULT '';

-- +goose Down
ALTER TABLE organizations DROP COLUMN IF EXISTS ai_transcription_api_key;
ALTER TABLE organizations DROP COLUMN IF EXISTS ai_api_key;
