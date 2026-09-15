-- +goose Up
ALTER TABLE integration_settings ADD COLUMN hide_no_chaptarr_match INTEGER NOT NULL DEFAULT 0;
ALTER TABLE integration_settings ADD COLUMN hide_no_hardcover_match INTEGER NOT NULL DEFAULT 0;

-- +goose Down
-- SQLite's ALTER TABLE can't drop a column on the versions this app
-- targets; the columns are left in place (harmless, default 0) on downgrade.
