-- +goose Up
CREATE TABLE IF NOT EXISTS kepub_settings (
    id      INTEGER PRIMARY KEY CHECK (id = 1),
    enabled INTEGER NOT NULL DEFAULT 1
);

-- +goose Down
DROP TABLE IF EXISTS kepub_settings;
