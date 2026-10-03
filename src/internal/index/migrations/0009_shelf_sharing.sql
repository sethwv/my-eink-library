-- +goose Up
CREATE TABLE shelf_members (
    shelf_id   INTEGER NOT NULL,
    username   TEXT NOT NULL,
    created_at INTEGER NOT NULL,
    PRIMARY KEY (shelf_id, username)
);
CREATE INDEX idx_shelf_members_username ON shelf_members(username, shelf_id);
CREATE INDEX idx_shelves_visibility ON shelves(visibility);

-- +goose Down
DROP INDEX IF EXISTS idx_shelves_visibility;
DROP INDEX IF EXISTS idx_shelf_members_username;
DROP TABLE IF EXISTS shelf_members;
