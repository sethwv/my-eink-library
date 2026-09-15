-- +goose Up
-- +goose StatementBegin
CREATE TABLE IF NOT EXISTS users (
    id                         INTEGER PRIMARY KEY AUTOINCREMENT,
    username                   TEXT NOT NULL UNIQUE,
    password_hash              TEXT NOT NULL,
    is_admin                   INTEGER NOT NULL DEFAULT 0,
    created_at                 INTEGER NOT NULL,
    bookmark_token_hash        TEXT,
    bookmark_token_created_at  INTEGER
);
-- +goose StatementEnd

-- +goose StatementBegin
CREATE TABLE IF NOT EXISTS smtp_settings (
    id         INTEGER PRIMARY KEY CHECK (id = 1),
    host       TEXT NOT NULL DEFAULT '',
    port       INTEGER NOT NULL DEFAULT 465,
    encryption TEXT NOT NULL DEFAULT 'tls',
    username   TEXT NOT NULL DEFAULT '',
    password   TEXT NOT NULL DEFAULT '',
    from_name  TEXT NOT NULL DEFAULT '',
    from_addr  TEXT NOT NULL DEFAULT ''
);
-- +goose StatementEnd

-- +goose Down
-- This baseline captures the schema as it already existed before goose was
-- adopted; there's no meaningful "down" for it.
