-- +goose Up
-- meeting summaries (add-meeting-summaries, design D2, D4, D5): the organization's setting, the summary's state on
-- the recording, and the inventory of the objects derived from a recording (registered before they are written)
ALTER TABLE organizations ADD COLUMN IF NOT EXISTS meeting_summaries VARCHAR(16) NOT NULL DEFAULT 'off';

ALTER TABLE meeting_recordings ADD COLUMN IF NOT EXISTS summary_policy VARCHAR(16) NOT NULL DEFAULT 'off';
ALTER TABLE meeting_recordings ADD COLUMN IF NOT EXISTS summary_source VARCHAR(16) NOT NULL DEFAULT 'none';
ALTER TABLE meeting_recordings ADD COLUMN IF NOT EXISTS summary_source_expires_at BIGINT NOT NULL DEFAULT 0;
ALTER TABLE meeting_recordings ADD COLUMN IF NOT EXISTS summary_status VARCHAR(16) NOT NULL DEFAULT 'none';
ALTER TABLE meeting_recordings ADD COLUMN IF NOT EXISTS summary_error VARCHAR(64) NOT NULL DEFAULT '';
ALTER TABLE meeting_recordings ADD COLUMN IF NOT EXISTS summary_claim_until BIGINT NOT NULL DEFAULT 0;
ALTER TABLE meeting_recordings ADD COLUMN IF NOT EXISTS summary_generation BIGINT NOT NULL DEFAULT 0;
ALTER TABLE meeting_recordings ADD COLUMN IF NOT EXISTS summary_post_id CHAR(26) NOT NULL DEFAULT '';
CREATE INDEX IF NOT EXISTS meeting_recordings_summary_idx ON meeting_recordings (summary_status, summary_claim_until);

CREATE TABLE IF NOT EXISTS recording_objects (
    id           CHAR(26)     NOT NULL PRIMARY KEY,
    recording_id CHAR(26)     NOT NULL,
    generation   BIGINT       NOT NULL,
    object_key   VARCHAR(255) NOT NULL,
    kind         VARCHAR(16)  NOT NULL,
    speaker      CHAR(26)     NOT NULL DEFAULT '',
    offset_ms    BIGINT       NOT NULL DEFAULT 0,
    duration_ms  BIGINT       NOT NULL DEFAULT 0,
    state        VARCHAR(8)   NOT NULL,
    create_at    BIGINT       NOT NULL
);
CREATE UNIQUE INDEX IF NOT EXISTS recording_objects_key_uq ON recording_objects (object_key);
CREATE INDEX IF NOT EXISTS recording_objects_recording_idx ON recording_objects (recording_id, kind);

-- +goose Down
DROP TABLE IF EXISTS recording_objects;
DROP INDEX IF EXISTS meeting_recordings_summary_idx;
ALTER TABLE meeting_recordings DROP COLUMN IF EXISTS summary_post_id;
ALTER TABLE meeting_recordings DROP COLUMN IF EXISTS summary_generation;
ALTER TABLE meeting_recordings DROP COLUMN IF EXISTS summary_claim_until;
ALTER TABLE meeting_recordings DROP COLUMN IF EXISTS summary_error;
ALTER TABLE meeting_recordings DROP COLUMN IF EXISTS summary_status;
ALTER TABLE meeting_recordings DROP COLUMN IF EXISTS summary_source_expires_at;
ALTER TABLE meeting_recordings DROP COLUMN IF EXISTS summary_source;
ALTER TABLE meeting_recordings DROP COLUMN IF EXISTS summary_policy;
ALTER TABLE organizations DROP COLUMN IF EXISTS meeting_summaries;
