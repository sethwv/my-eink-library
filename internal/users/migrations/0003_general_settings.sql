-- +goose Up
-- +goose StatementBegin
CREATE TABLE IF NOT EXISTS general_settings (
    id                 INTEGER PRIMARY KEY CHECK (id = 1),
    site_name          TEXT NOT NULL DEFAULT '',
    public_url         TEXT NOT NULL DEFAULT '',
    cover_width        INTEGER NOT NULL DEFAULT 0,
    page_size          INTEGER NOT NULL DEFAULT 0,
    session_ttl_seconds INTEGER NOT NULL DEFAULT 0
);
-- +goose StatementEnd

-- +goose Down
DROP TABLE general_settings;
