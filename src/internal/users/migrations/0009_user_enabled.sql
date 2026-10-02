-- +goose Up
ALTER TABLE users ADD COLUMN enabled INTEGER NOT NULL DEFAULT 1;

-- +goose Down
-- SQLite does not support dropping columns in the versions we support.
