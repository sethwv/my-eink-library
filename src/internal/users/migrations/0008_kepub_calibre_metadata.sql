-- +goose Up
ALTER TABLE kepub_settings ADD COLUMN write_calibre_metadata INTEGER NOT NULL DEFAULT 0;

-- +goose Down
-- SQLite cannot drop columns on all supported versions. The setting is harmless
-- if this migration is rolled back.
