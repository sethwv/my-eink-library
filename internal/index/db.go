// Package index maintains a SQLite index of the EPUB library, built from a
// full directory scan and kept current via an fsnotify watcher. All
// browsing/sorting queries read from this index, never the filesystem.
package index

import (
	"database/sql"
	"embed"
	"fmt"

	"github.com/pressly/goose/v3"
	_ "modernc.org/sqlite"
)

//go:embed migrations/*.sql
var migrationsFS embed.FS

type DB struct {
	sql *sql.DB
}

// Open opens (creating if necessary) the SQLite index at dbPath and applies
// the schema via goose (migrations/*.sql, embedded in the binary —
// 0001_baseline.sql is a byte-for-byte copy of the CREATE TABLE IF NOT
// EXISTS statements this package used to apply directly; anything added
// after goose's adoption gets its own numbered migration file instead).
func Open(dbPath string) (*DB, error) {
	sdb, err := sql.Open("sqlite", dbPath+"?_pragma=busy_timeout(5000)&_pragma=journal_mode(WAL)")
	if err != nil {
		return nil, fmt.Errorf("open sqlite: %w", err)
	}
	// modernc.org/sqlite doesn't support real concurrent writers; a single
	// connection avoids "database is locked" errors from Go's connection pool.
	sdb.SetMaxOpenConns(1)

	goose.SetBaseFS(migrationsFS)
	if err := goose.SetDialect("sqlite3"); err != nil {
		sdb.Close()
		return nil, fmt.Errorf("set migration dialect: %w", err)
	}
	if err := goose.Up(sdb, "migrations"); err != nil {
		sdb.Close()
		return nil, fmt.Errorf("apply migrations: %w", err)
	}

	db := &DB{sql: sdb}
	// These three predate goose's adoption and stay as-is: they upgrade
	// databases whose book_enrichment/books tables were created before the
	// columns they check for existed, which the goose baseline's
	// CREATE-TABLE-IF-NOT-EXISTS alone can't retrofit onto an
	// already-existing table. See eink-library-y3h for why this wasn't
	// folded into individual historical migrations.
	if err := db.migrateEnrichmentStatusColumn(); err != nil {
		sdb.Close()
		return nil, fmt.Errorf("migrate schema: %w", err)
	}
	if err := db.migrateBookEnrichmentTable(); err != nil {
		sdb.Close()
		return nil, fmt.Errorf("migrate schema: %w", err)
	}
	if err := db.migrateBookEnrichmentColumns(); err != nil {
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

// migrateBookEnrichmentColumns adds the title/description/genres/publisher/
// pages/isbn/rating columns to a book_enrichment table that predates them
// (same additive, idempotent ALTER TABLE approach as
// migrateEnrichmentStatusColumn — CREATE TABLE IF NOT EXISTS only helps
// brand-new databases). Books already marked "done" under the pre-broadened
// schema won't automatically re-queue to pick these up; an admin "reset
// enrichment" is the documented way to force a full re-pass after upgrading.
func (d *DB) migrateBookEnrichmentColumns() error {
	rows, err := d.sql.Query(`PRAGMA table_info(book_enrichment)`)
	if err != nil {
		return err
	}
	existing := map[string]bool{}
	for rows.Next() {
		var cid int
		var name, colType string
		var notNull, pk int
		var dflt sql.NullString
		if err := rows.Scan(&cid, &name, &colType, &notNull, &dflt, &pk); err != nil {
			rows.Close()
			return err
		}
		existing[name] = true
	}
	if err := rows.Err(); err != nil {
		return err
	}
	rows.Close()

	newColumns := []struct{ name, ddl string }{
		{"title", "TEXT"},
		{"description", "TEXT"},
		{"genres", "TEXT"},
		{"publisher", "TEXT"},
		{"pages", "INTEGER"},
		{"isbn", "TEXT"},
		{"rating", "REAL"},
	}
	for _, c := range newColumns {
		if existing[c.name] {
			continue
		}
		if _, err := d.sql.Exec(fmt.Sprintf(`ALTER TABLE book_enrichment ADD COLUMN %s %s`, c.name, c.ddl)); err != nil {
			return err
		}
	}
	return nil
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
