package index

import (
	"database/sql"
	"fmt"
	"time"
)

// Shelf is a named, per-user collection of books. "Favourites" is the first
// system-provided shelf; user-created shelves (with their own management UI)
// are a natural future extension of this same table.
type Shelf struct {
	ID       int64
	Username string
	Slug     string
	Name     string
	IsSystem bool
}

// EnsureSystemShelf returns the id of the given system shelf for username,
// creating it (marked is_system) if it doesn't exist yet.
func (d *DB) EnsureSystemShelf(username, slug, name string) (int64, error) {
	var id int64
	err := d.sql.QueryRow(`SELECT id FROM shelves WHERE username = ? AND slug = ?`, username, slug).Scan(&id)
	if err == nil {
		return id, nil
	}
	if err != sql.ErrNoRows {
		return 0, err
	}

	res, err := d.sql.Exec(
		`INSERT INTO shelves (username, slug, name, is_system, created_at) VALUES (?, ?, ?, 1, ?)`,
		username, slug, name, time.Now().Unix(),
	)
	if err != nil {
		return 0, fmt.Errorf("create shelf: %w", err)
	}
	return res.LastInsertId()
}

// ListShelves returns every shelf owned by username, system shelves first.
func (d *DB) ListShelves(username string) ([]Shelf, error) {
	rows, err := d.sql.Query(
		`SELECT id, username, slug, name, is_system FROM shelves WHERE username = ? ORDER BY is_system DESC, name`,
		username,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var shelves []Shelf
	for rows.Next() {
		var sh Shelf
		var isSystem int
		if err := rows.Scan(&sh.ID, &sh.Username, &sh.Slug, &sh.Name, &isSystem); err != nil {
			return nil, err
		}
		sh.IsSystem = isSystem != 0
		shelves = append(shelves, sh)
	}
	return shelves, rows.Err()
}

// GetShelf returns the shelf with the given id, or nil if it doesn't exist.
// Callers must check Shelf.Username against the current user before acting
// on it — a shelf id alone doesn't prove ownership.
func (d *DB) GetShelf(id int64) (*Shelf, error) {
	var sh Shelf
	var isSystem int
	err := d.sql.QueryRow(
		`SELECT id, username, slug, name, is_system FROM shelves WHERE id = ?`, id,
	).Scan(&sh.ID, &sh.Username, &sh.Slug, &sh.Name, &isSystem)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	sh.IsSystem = isSystem != 0
	return &sh, nil
}

// IsBookOnShelf reports whether bookID is already on shelfID.
func (d *DB) IsBookOnShelf(shelfID, bookID int64) (bool, error) {
	var exists int
	err := d.sql.QueryRow(`SELECT 1 FROM shelf_books WHERE shelf_id = ? AND book_id = ?`, shelfID, bookID).Scan(&exists)
	if err == sql.ErrNoRows {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	return true, nil
}

// AddBookToShelf adds bookID to shelfID, a no-op if already present.
func (d *DB) AddBookToShelf(shelfID, bookID int64) error {
	_, err := d.sql.Exec(
		`INSERT OR IGNORE INTO shelf_books (shelf_id, book_id, added_at) VALUES (?, ?, ?)`,
		shelfID, bookID, time.Now().Unix(),
	)
	return err
}

// RemoveBookFromShelf removes bookID from shelfID, a no-op if not present.
func (d *DB) RemoveBookFromShelf(shelfID, bookID int64) error {
	_, err := d.sql.Exec(`DELETE FROM shelf_books WHERE shelf_id = ? AND book_id = ?`, shelfID, bookID)
	return err
}

// ShelfBookIDs returns the set of book ids on shelfID, for marking state
// (e.g. a filled star) across a whole listing page in one query.
func (d *DB) ShelfBookIDs(shelfID int64) (map[int64]bool, error) {
	rows, err := d.sql.Query(`SELECT book_id FROM shelf_books WHERE shelf_id = ?`, shelfID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	ids := make(map[int64]bool)
	for rows.Next() {
		var id int64
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		ids[id] = true
	}
	return ids, rows.Err()
}
