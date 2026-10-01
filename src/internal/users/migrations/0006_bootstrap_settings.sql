-- +goose Up
CREATE TABLE IF NOT EXISTS bootstrap_settings (
    id             INTEGER PRIMARY KEY CHECK (id = 1),
    session_secret TEXT NOT NULL DEFAULT ''
);

-- +goose Down
DROP TABLE IF EXISTS bootstrap_settings;
