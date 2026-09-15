-- +goose Up
-- +goose StatementBegin
CREATE TABLE IF NOT EXISTS books (
    id              INTEGER PRIMARY KEY AUTOINCREMENT,
    file_path       TEXT NOT NULL UNIQUE,
    file_size       INTEGER NOT NULL,
    file_mtime      INTEGER NOT NULL,

    title           TEXT NOT NULL,
    sort_title      TEXT NOT NULL,
    author          TEXT NOT NULL DEFAULT '',
    sort_author     TEXT NOT NULL DEFAULT '',
    series          TEXT,
    series_index    REAL,

    description     TEXT,
    language        TEXT,
    publisher       TEXT,
    published_date  TEXT,
    identifier      TEXT,

    cover_path      TEXT,
    has_cover       INTEGER NOT NULL DEFAULT 0,

    added_at        INTEGER NOT NULL,
    updated_at      INTEGER NOT NULL,
    parse_error     TEXT,

    enrichment_status TEXT NOT NULL DEFAULT ''
);
-- +goose StatementEnd

CREATE INDEX IF NOT EXISTS idx_books_sort_title  ON books(sort_title);
CREATE INDEX IF NOT EXISTS idx_books_sort_author ON books(sort_author);
CREATE INDEX IF NOT EXISTS idx_books_series      ON books(series, series_index);
CREATE INDEX IF NOT EXISTS idx_books_added_at    ON books(added_at);

CREATE TABLE IF NOT EXISTS meta (key TEXT PRIMARY KEY, value TEXT);

-- +goose StatementBegin
CREATE TABLE IF NOT EXISTS shelves (
    id         INTEGER PRIMARY KEY AUTOINCREMENT,
    username   TEXT NOT NULL,
    slug       TEXT NOT NULL,
    name       TEXT NOT NULL,
    is_system  INTEGER NOT NULL DEFAULT 0,
    created_at INTEGER NOT NULL,
    UNIQUE(username, slug)
);
-- +goose StatementEnd

-- +goose StatementBegin
CREATE TABLE IF NOT EXISTS shelf_books (
    shelf_id INTEGER NOT NULL,
    book_id  INTEGER NOT NULL,
    added_at INTEGER NOT NULL,
    PRIMARY KEY (shelf_id, book_id)
);
-- +goose StatementEnd
CREATE INDEX IF NOT EXISTS idx_shelf_books_book ON shelf_books(book_id);

-- +goose StatementBegin
CREATE TABLE IF NOT EXISTS book_enrichment (
    book_id         INTEGER PRIMARY KEY,
    title           TEXT,
    series          TEXT,
    series_index    REAL,
    published_date  TEXT,
    description     TEXT,
    genres          TEXT,
    publisher       TEXT,
    pages           INTEGER,
    isbn            TEXT,
    rating          REAL,
    status          TEXT NOT NULL DEFAULT '',
    updated_at      INTEGER NOT NULL,
    FOREIGN KEY (book_id) REFERENCES books(id) ON DELETE CASCADE
);
-- +goose StatementEnd

-- +goose Down
-- This baseline captures the schema as it already existed before goose was
-- adopted; there's no meaningful "down" for it (see internal/index's own
-- convention of derived/owned state, not a system of record with rollback).
