-- +goose Up
ALTER TABLE shelves ADD COLUMN visibility TEXT NOT NULL DEFAULT 'private';
CREATE UNIQUE INDEX idx_shelves_owner_name ON shelves(username, name COLLATE NOCASE) WHERE is_system = 0;

-- +goose Down
DROP INDEX IF EXISTS idx_shelves_owner_name;
