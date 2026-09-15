-- +goose Up
ALTER TABLE integration_settings ADD COLUMN hardcover_overwrite_cover INTEGER NOT NULL DEFAULT 0;

-- +goose Down
-- SQLite's ALTER TABLE can't drop a column on the versions this app
-- targets; the column is left in place (harmless, default 0) on downgrade.
