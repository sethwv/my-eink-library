-- +goose Up
ALTER TABLE general_settings ADD COLUMN shelf_limit INTEGER NOT NULL DEFAULT 5;

-- +goose Down
-- SQLite cannot drop columns on supported installations.
