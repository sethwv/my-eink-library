-- +goose Up
CREATE TABLE book_locations (
    book_id       INTEGER NOT NULL REFERENCES books(id) ON DELETE CASCADE,
    library_root  TEXT NOT NULL,
    file_path     TEXT NOT NULL,
    file_size     INTEGER NOT NULL,
    file_mtime    INTEGER NOT NULL,
    added_at      INTEGER NOT NULL,
    PRIMARY KEY (library_root, file_path)
);

CREATE INDEX idx_book_locations_book ON book_locations(book_id);

-- +goose Down
DROP TABLE book_locations;
