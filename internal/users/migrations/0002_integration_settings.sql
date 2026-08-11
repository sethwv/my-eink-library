-- +goose Up
-- +goose StatementBegin
CREATE TABLE IF NOT EXISTS integration_settings (
    id                 INTEGER PRIMARY KEY CHECK (id = 1),
    hardcover_enabled  INTEGER NOT NULL DEFAULT 0,
    hardcover_token    TEXT NOT NULL DEFAULT '',
    chaptarr_enabled   INTEGER NOT NULL DEFAULT 0,
    chaptarr_url       TEXT NOT NULL DEFAULT '',
    chaptarr_api_key   TEXT NOT NULL DEFAULT ''
);
-- +goose StatementEnd

-- +goose Down
DROP TABLE integration_settings;
