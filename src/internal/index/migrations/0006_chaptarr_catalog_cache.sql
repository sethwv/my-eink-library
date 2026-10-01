-- +goose Up
CREATE TABLE chaptarr_catalog_cache (
    scope TEXT PRIMARY KEY,
    refreshed_at INTEGER NOT NULL,
    books_json TEXT NOT NULL
);

-- +goose Down
DROP TABLE chaptarr_catalog_cache;
