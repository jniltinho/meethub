-- MariaDB DDL is not transactional: every statement is re-runnable (IF [NOT] EXISTS).
-- +goose NO TRANSACTION
-- +goose Up
-- meeting summaries (add-meeting-summaries, design D2, D4, D5): the organization's setting, the summary's state on
-- the recording, and the inventory of the objects derived from a recording (registered before they are written)
ALTER TABLE organizations ADD COLUMN IF NOT EXISTS meeting_summaries VARCHAR(16) NOT NULL DEFAULT 'off';

ALTER TABLE meeting_recordings
    ADD COLUMN IF NOT EXISTS summary_policy VARCHAR(16) NOT NULL DEFAULT 'off',
    ADD COLUMN IF NOT EXISTS summary_source VARCHAR(16) NOT NULL DEFAULT 'none',
    ADD COLUMN IF NOT EXISTS summary_source_expires_at BIGINT NOT NULL DEFAULT 0,
    ADD COLUMN IF NOT EXISTS summary_status VARCHAR(16) NOT NULL DEFAULT 'none',
    ADD COLUMN IF NOT EXISTS summary_error VARCHAR(64) NOT NULL DEFAULT '',
    ADD COLUMN IF NOT EXISTS summary_claim_until BIGINT NOT NULL DEFAULT 0,
    ADD COLUMN IF NOT EXISTS summary_generation BIGINT NOT NULL DEFAULT 0,
    ADD COLUMN IF NOT EXISTS summary_post_id CHAR(26) NOT NULL DEFAULT '';
CREATE INDEX IF NOT EXISTS meeting_recordings_summary_idx ON meeting_recordings (summary_status, summary_claim_until);

CREATE TABLE IF NOT EXISTS recording_objects (
    id           CHAR(26)     NOT NULL PRIMARY KEY,
    recording_id CHAR(26)     NOT NULL,
    generation   BIGINT       NOT NULL,
    object_key   VARCHAR(255) CHARACTER SET ascii COLLATE ascii_bin NOT NULL,
    kind         VARCHAR(16)  NOT NULL,
    speaker      CHAR(26)     NOT NULL DEFAULT '',
    offset_ms    BIGINT       NOT NULL DEFAULT 0,
    duration_ms  BIGINT       NOT NULL DEFAULT 0,
    state        VARCHAR(8)   NOT NULL,
    create_at    BIGINT       NOT NULL,
    UNIQUE KEY recording_objects_key_uq (object_key),
    KEY recording_objects_recording_idx (recording_id, kind)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci;

-- +goose Down
DROP TABLE IF EXISTS recording_objects;
DROP INDEX IF EXISTS meeting_recordings_summary_idx ON meeting_recordings;
ALTER TABLE meeting_recordings
    DROP COLUMN IF EXISTS summary_post_id,
    DROP COLUMN IF EXISTS summary_generation,
    DROP COLUMN IF EXISTS summary_claim_until,
    DROP COLUMN IF EXISTS summary_error,
    DROP COLUMN IF EXISTS summary_status,
    DROP COLUMN IF EXISTS summary_source_expires_at,
    DROP COLUMN IF EXISTS summary_source,
    DROP COLUMN IF EXISTS summary_policy;
ALTER TABLE organizations DROP COLUMN IF EXISTS meeting_summaries;
