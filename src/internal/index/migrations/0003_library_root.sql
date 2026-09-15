-- +goose Up
-- +goose StatementBegin
CREATE TABLE books_new (
    id              INTEGER PRIMARY KEY AUTOINCREMENT,
    library_root    TEXT NOT NULL DEFAULT '',
    file_path       TEXT NOT NULL,
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

    enrichment_status TEXT NOT NULL DEFAULT '',

    UNIQUE(library_root, file_path)
);

INSERT INTO books_new (id, library_root, file_path, file_size, file_mtime, title, sort_title, author, sort_author,
    series, series_index, description, language, publisher, published_date, identifier,
    cover_path, has_cover, added_at, updated_at, parse_error, enrichment_status)
SELECT id, '', file_path, file_size, file_mtime, title, sort_title, author, sort_author,
    series, series_index, description, language, publisher, published_date, identifier,
    cover_path, has_cover, added_at, updated_at, parse_error, enrichment_status
FROM books;

DROP TABLE books;
ALTER TABLE books_new RENAME TO books;

CREATE INDEX idx_books_sort_title  ON books(sort_title);
CREATE INDEX idx_books_sort_author ON books(sort_author);
CREATE INDEX idx_books_series      ON books(series, series_index);
CREATE INDEX idx_books_added_at    ON books(added_at);
-- +goose StatementEnd

-- +goose Down
-- Rebuilding books to drop library_root would require re-collapsing rows
-- that may now legitimately share a file_path across roots; not attempted
-- (see internal/index's own convention of derived/owned state, not a system
-- of record with rollback).
