-- +goose Up
ALTER TABLE general_settings ADD COLUMN password_reset_enabled INTEGER NOT NULL DEFAULT 0;

-- +goose Down
-- SQLite does not support dropping columns in the versions we support.
