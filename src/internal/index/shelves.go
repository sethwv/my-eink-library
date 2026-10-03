package index

import (
	"database/sql"
	"fmt"
	"strconv"
	"strings"
	"time"
)

const MaxUserShelves = 25

// Shelf is a named, per-user collection of books. "Favourites" is the first
// system-provided shelf; user-created shelves (with their own management UI)
// are a natural future extension of this same table.
type Shelf struct {
	ID         int64
	Username   string
	Slug       string
	Name       string
	IsSystem   bool
	Visibility string
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
		`INSERT INTO shelves (username, slug, name, is_system, visibility, created_at) VALUES (?, ?, ?, 1, 'private', ?)`,
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
		`SELECT id, username, slug, name, is_system, visibility FROM shelves WHERE username = ? ORDER BY is_system DESC, name`,
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
		if err := rows.Scan(&sh.ID, &sh.Username, &sh.Slug, &sh.Name, &isSystem, &sh.Visibility); err != nil {
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
		`SELECT id, username, slug, name, is_system, visibility FROM shelves WHERE id = ?`, id,
	).Scan(&sh.ID, &sh.Username, &sh.Slug, &sh.Name, &isSystem, &sh.Visibility)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	sh.IsSystem = isSystem != 0
	return &sh, nil
}

// GetOwnedShelf returns a shelf owned by username, or nil when it does not
// exist. It centralizes the owner scope required by private shelves.
func (d *DB) GetOwnedShelf(username string, id int64) (*Shelf, error) {
	var sh Shelf
	var isSystem int
	err := d.sql.QueryRow(
		`SELECT id, username, slug, name, is_system, visibility FROM shelves WHERE id = ? AND username = ?`, id, username,
	).Scan(&sh.ID, &sh.Username, &sh.Slug, &sh.Name, &isSystem, &sh.Visibility)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	sh.IsSystem = isSystem != 0
	return &sh, nil
}

// CreateShelf creates a private non-system shelf for username. The caller is
// responsible for checking the effective own_shelves permission.
func (d *DB) CreateShelf(username, name string) (*Shelf, error) {
	name = strings.TrimSpace(name)
	if name == "" {
		return nil, fmt.Errorf("shelf name is required")
	}
	if len(name) > 100 {
		return nil, fmt.Errorf("shelf name must be 100 characters or fewer")
	}
	tx, err := d.sql.Begin()
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()
	var count int
	if err := tx.QueryRow(`SELECT COUNT(*) FROM shelves WHERE username = ? AND is_system = 0`, username).Scan(&count); err != nil {
		return nil, err
	}
	if count >= MaxUserShelves {
		return nil, fmt.Errorf("you can create at most %d shelves", MaxUserShelves)
	}
	var exists int
	if err := tx.QueryRow(`SELECT EXISTS(SELECT 1 FROM shelves WHERE username = ? AND is_system = 0 AND name = ? COLLATE NOCASE)`, username, name).Scan(&exists); err != nil {
		return nil, err
	}
	if exists != 0 {
		return nil, fmt.Errorf("a shelf with that name already exists")
	}
	slug, err := shelfSlug(tx, username, name)
	if err != nil {
		return nil, err
	}
	result, err := tx.Exec(`INSERT INTO shelves (username, slug, name, visibility, created_at) VALUES (?, ?, ?, 'private', ?)`, username, slug, name, time.Now().Unix())
	if err != nil {
		return nil, fmt.Errorf("create shelf: %w", err)
	}
	id, err := result.LastInsertId()
	if err != nil {
		return nil, err
	}
	if err := tx.Commit(); err != nil {
		return nil, err
	}
	return &Shelf{ID: id, Username: username, Slug: slug, Name: name, Visibility: "private"}, nil
}

func (d *DB) RenameShelf(username string, id int64, name string) error {
	name = strings.TrimSpace(name)
	if name == "" {
		return fmt.Errorf("shelf name is required")
	}
	if len(name) > 100 {
		return fmt.Errorf("shelf name must be 100 characters or fewer")
	}
	result, err := d.sql.Exec(`UPDATE shelves SET name = ? WHERE id = ? AND username = ? AND is_system = 0`, name, id, username)
	if err != nil {
		if strings.Contains(err.Error(), "idx_shelves_owner_name") || strings.Contains(err.Error(), "UNIQUE constraint failed") {
			return fmt.Errorf("a shelf with that name already exists")
		}
		return err
	}
	changed, err := result.RowsAffected()
	if err != nil {
		return err
	}
	if changed == 0 {
		return fmt.Errorf("shelf not found")
	}
	return nil
}

func (d *DB) DeleteShelf(username string, id int64) error {
	tx, err := d.sql.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	result, err := tx.Exec(`DELETE FROM shelves WHERE id = ? AND username = ? AND is_system = 0`, id, username)
	if err != nil {
		return err
	}
	changed, err := result.RowsAffected()
	if err != nil {
		return err
	}
	if changed == 0 {
		return fmt.Errorf("shelf not found")
	}
	if _, err := tx.Exec(`DELETE FROM shelf_books WHERE shelf_id = ?`, id); err != nil {
		return err
	}
	return tx.Commit()
}

type shelfSlugQueryer interface {
	QueryRow(string, ...any) *sql.Row
}

func shelfSlug(q shelfSlugQueryer, username, name string) (string, error) {
	var b strings.Builder
	for _, r := range strings.ToLower(name) {
		if (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') {
			b.WriteRune(r)
		} else if b.Len() > 0 && !strings.HasSuffix(b.String(), "-") {
			b.WriteByte('-')
		}
	}
	base := strings.Trim(b.String(), "-")
	if base == "" {
		base = "shelf"
	}
	for n := 1; ; n++ {
		slug := base
		if n > 1 {
			slug += "-" + strconv.Itoa(n)
		}
		var exists bool
		if err := q.QueryRow(`SELECT EXISTS(SELECT 1 FROM shelves WHERE username = ? AND slug = ?)`, username, slug).Scan(&exists); err != nil {
			return "", err
		}
		if !exists {
			return slug, nil
		}
	}
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

// ShelfMemberships returns the given user's shelf membership for displayed
// books. Joining shelves scopes the result to the owner without adding one
// query parameter per shelf.
func (d *DB) ShelfMemberships(username string, bookIDs []int64) (map[int64]map[int64]bool, error) {
	memberships := make(map[int64]map[int64]bool)
	if len(bookIDs) == 0 {
		return memberships, nil
	}
	args := make([]any, 1, len(bookIDs)+1)
	args[0] = username
	for _, bookID := range bookIDs {
		args = append(args, bookID)
	}
	placeholders := func(n int) string {
		return strings.TrimRight(strings.Repeat("?,", n), ",")
	}
	rows, err := d.sql.Query(
		`SELECT shelf_books.shelf_id, shelf_books.book_id FROM shelf_books JOIN shelves ON shelves.id = shelf_books.shelf_id WHERE shelves.username = ? AND shelf_books.book_id IN (`+placeholders(len(bookIDs))+`)`,
		args...,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	for rows.Next() {
		var shelfID, bookID int64
		if err := rows.Scan(&shelfID, &bookID); err != nil {
			return nil, err
		}
		if memberships[shelfID] == nil {
			memberships[shelfID] = make(map[int64]bool)
		}
		memberships[shelfID][bookID] = true
	}
	return memberships, rows.Err()
}
