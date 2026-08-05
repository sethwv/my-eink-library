// Package index maintains a SQLite index of the EPUB library, built from a
// full directory scan and kept current via an fsnotify watcher. All
// browsing/sorting queries read from this index, never the filesystem.
package index

import (
	"database/sql"
	"fmt"

	_ "modernc.org/sqlite"
)

const schema = `
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
    parse_error     TEXT
);

CREATE INDEX IF NOT EXISTS idx_books_sort_title  ON books(sort_title);
CREATE INDEX IF NOT EXISTS idx_books_sort_author ON books(sort_author);
CREATE INDEX IF NOT EXISTS idx_books_series      ON books(series, series_index);
CREATE INDEX IF NOT EXISTS idx_books_added_at    ON books(added_at);

CREATE TABLE IF NOT EXISTS meta (key TEXT PRIMARY KEY, value TEXT);

CREATE TABLE IF NOT EXISTS shelves (
    id         INTEGER PRIMARY KEY AUTOINCREMENT,
    username   TEXT NOT NULL,
    slug       TEXT NOT NULL,
    name       TEXT NOT NULL,
    is_system  INTEGER NOT NULL DEFAULT 0,
    created_at INTEGER NOT NULL,
    UNIQUE(username, slug)
);

CREATE TABLE IF NOT EXISTS shelf_books (
    shelf_id INTEGER NOT NULL,
    book_id  INTEGER NOT NULL,
    added_at INTEGER NOT NULL,
    PRIMARY KEY (shelf_id, book_id)
);
CREATE INDEX IF NOT EXISTS idx_shelf_books_book ON shelf_books(book_id);
`

type DB struct {
	sql *sql.DB
}

// Open opens (creating if necessary) the SQLite index at dbPath and applies the schema.
func Open(dbPath string) (*DB, error) {
	sdb, err := sql.Open("sqlite", dbPath+"?_pragma=busy_timeout(5000)&_pragma=journal_mode(WAL)")
	if err != nil {
		return nil, fmt.Errorf("open sqlite: %w", err)
	}
	// modernc.org/sqlite doesn't support real concurrent writers; a single
	// connection avoids "database is locked" errors from Go's connection pool.
	sdb.SetMaxOpenConns(1)

	if _, err := sdb.Exec(schema); err != nil {
		sdb.Close()
		return nil, fmt.Errorf("apply schema: %w", err)
	}

	return &DB{sql: sdb}, nil
}

func (d *DB) Close() error {
	return d.sql.Close()
}
