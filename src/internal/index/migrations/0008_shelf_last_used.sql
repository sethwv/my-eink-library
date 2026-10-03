-- +goose Up
ALTER TABLE shelves ADD COLUMN last_used_at INTEGER NOT NULL DEFAULT 0;

-- +goose Down
SELECT 1;
