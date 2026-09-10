-- Time logged by hand ("a phone call counts too") rather than timed live.
--
-- A logged entry is an ordinary entry with a start and a stop, so every total,
-- clip and filter counts it already. The flag exists only so the history can
-- say which entries were typed in afterwards — what someone checking an
-- invoice wants to know. A logged entry is never running, and the schema holds
-- that rather than trusting every insert to.

-- +goose Up
ALTER TABLE time_entries ADD COLUMN manual INTEGER NOT NULL DEFAULT 0
    CHECK (manual IN (0, 1) AND (manual = 0 OR stopped_at IS NOT NULL));

-- +goose Down
ALTER TABLE time_entries DROP COLUMN manual;
