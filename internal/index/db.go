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
    parse_error     TEXT,

    enrichment_status TEXT NOT NULL DEFAULT ''
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

CREATE TABLE IF NOT EXISTS book_enrichment (
    book_id         INTEGER PRIMARY KEY,
    series          TEXT,
    series_index    REAL,
    published_date  TEXT,
    status          TEXT NOT NULL DEFAULT '',
    updated_at      INTEGER NOT NULL,
    FOREIGN KEY (book_id) REFERENCES books(id) ON DELETE CASCADE
);
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

	db := &DB{sql: sdb}
	if err := db.migrateEnrichmentStatusColumn(); err != nil {
		sdb.Close()
		return nil, fmt.Errorf("migrate schema: %w", err)
	}
	if err := db.migrateBookEnrichmentTable(); err != nil {
		sdb.Close()
		return nil, fmt.Errorf("migrate schema: %w", err)
	}

	return db, nil
}

// migrateEnrichmentStatusColumn adds enrichment_status to a books table that
// predates it. CREATE TABLE IF NOT EXISTS in schema only applies to
// brand-new databases, so an existing index.db needs the column added in
// place — there's no migration framework here, just an additive,
// idempotent ALTER TABLE guarded by checking what columns already exist.
func (d *DB) migrateEnrichmentStatusColumn() error {
	rows, err := d.sql.Query(`PRAGMA table_info(books)`)
	if err != nil {
		return err
	}
	hasColumn := false
	for rows.Next() {
		var cid int
		var name, colType string
		var notNull, pk int
		var dflt sql.NullString
		if err := rows.Scan(&cid, &name, &colType, &notNull, &dflt, &pk); err != nil {
			rows.Close()
			return err
		}
		if name == "enrichment_status" {
			hasColumn = true
		}
	}
	if err := rows.Err(); err != nil {
		return err
	}
	rows.Close()

	if !hasColumn {
		if _, err := d.sql.Exec(`ALTER TABLE books ADD COLUMN enrichment_status TEXT NOT NULL DEFAULT ''`); err != nil {
			return err
		}
	}
	return nil
}

// migrateBookEnrichmentTable creates book_enrichment for a database that
// predates the enrichment/EPUB-data split (schema's CREATE TABLE IF NOT
// EXISTS only helps brand-new databases), then backfills status from the old
// books.enrichment_status column so pending/done/no_match/error counts
// survive the upgrade. series/series_index/published_date are deliberately
// left blank in the new table: the books columns already hold whatever
// merged EPUB+Hardcover value is currently displayed, and leaving them there
// preserves current display until the next enrichment pass (or a manual
// reset) repopulates book_enrichment cleanly.
func (d *DB) migrateBookEnrichmentTable() error {
	if _, err := d.sql.Exec(`
		CREATE TABLE IF NOT EXISTS book_enrichment (
		    book_id         INTEGER PRIMARY KEY,
		    series          TEXT,
		    series_index    REAL,
		    published_date  TEXT,
		    status          TEXT NOT NULL DEFAULT '',
		    updated_at      INTEGER NOT NULL,
		    FOREIGN KEY (book_id) REFERENCES books(id) ON DELETE CASCADE
		)`); err != nil {
		return err
	}

	_, err := d.sql.Exec(`
		INSERT INTO book_enrichment (book_id, status, updated_at)
		SELECT id, enrichment_status, updated_at FROM books
		WHERE enrichment_status != '' AND id NOT IN (SELECT book_id FROM book_enrichment)`)
	return err
}

func (d *DB) Close() error {
	return d.sql.Close()
}

// SetMeta upserts a single key/value pair in the meta table.
func (d *DB) SetMeta(key, value string) error {
	_, err := d.sql.Exec(`INSERT INTO meta (key, value) VALUES (?, ?) ON CONFLICT(key) DO UPDATE SET value = excluded.value`, key, value)
	return err
}

// GetMeta returns the value for key and whether it was found.
func (d *DB) GetMeta(key string) (string, bool, error) {
	var value string
	err := d.sql.QueryRow(`SELECT value FROM meta WHERE key = ?`, key).Scan(&value)
	if err == sql.ErrNoRows {
		return "", false, nil
	}
	if err != nil {
		return "", false, err
	}
	return value, true, nil
}
