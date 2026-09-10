-- Deleting an entry only marks it, so an entry deleted by mistake can be brought
-- back exactly as it was.
--
-- Every read of time_entries leaves a marked row out — the running list, the
-- history, its totals and the export — and so does every write that changes an
-- existing entry, so a deleted timer cannot be stopped or renamed behind the
-- person's back. A NULL deleted_at is a live entry.

-- +goose Up
ALTER TABLE time_entries ADD COLUMN deleted_at INTEGER;

-- +goose Down
-- Rolling back drops the mark itself, so every deleted entry comes back as a live one.
ALTER TABLE time_entries DROP COLUMN deleted_at;
