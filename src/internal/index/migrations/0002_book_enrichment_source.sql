-- +goose Up
ALTER TABLE book_enrichment ADD COLUMN source TEXT NOT NULL DEFAULT '';

-- +goose Down
-- SQLite's ALTER TABLE can't drop a column on the versions this app
-- targets; source is left in place (harmless, defaults to '') on downgrade.
