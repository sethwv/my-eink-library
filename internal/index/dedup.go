package index

import (
	"database/sql"
	"time"

	"github.com/sethwv/my-eink-library/internal/epub"
)

// insertLocation records fullPath's file as an additional known location of
// bookID (a scan-time title+author match), rather than inserting a new
// books row. Backfills bookID's cover from meta if it doesn't have one yet.
func (d *DB) insertLocation(bookID int64, root, relPath string, size, mtime int64, saver CoverSaver, hasCover bool, meta *epub.Metadata) error {
	if _, err := d.sql.Exec(`
		INSERT INTO book_locations (book_id, library_root, file_path, file_size, file_mtime, added_at)
		VALUES (?, ?, ?, ?, ?, ?)`,
		bookID, root, relPath, size, mtime, time.Now().Unix()); err != nil {
		return err
	}
	if !hasCover || saver == nil {
		return nil
	}
	var existingHasCover bool
	if err := d.sql.QueryRow(`SELECT has_cover FROM books WHERE id = ?`, bookID).Scan(&existingHasCover); err != nil || existingHasCover {
		return nil
	}
	if p, err := saver.SaveCover(bookID, meta.CoverData, meta.CoverMediaType); err == nil {
		_, _ = d.sql.Exec(`UPDATE books SET cover_path=?, has_cover=1 WHERE id=?`, p, bookID)
	}
	return nil
}

// pickCanonical decides which of two book rows should survive a merge. The
// copy in the lower-indexed configured library root wins (position in
// LIBRARY_PATH's comma-separated list -- first-listed root beats later
// ones); ties fall back to earlier added_at, then lower id.
func (d *DB) pickCanonical(idA, idB int64, libraryRoots []string) (keepID, dropID int64, err error) {
	rootIndex := make(map[string]int, len(libraryRoots))
	for i, r := range libraryRoots {
		rootIndex[r] = i
	}

	type info struct {
		id      int64
		root    string
		addedAt int64
	}
	load := func(id int64) (info, error) {
		var root string
		var addedAt int64
		if err := d.sql.QueryRow(`SELECT library_root, added_at FROM books WHERE id = ?`, id).Scan(&root, &addedAt); err != nil {
			return info{}, err
		}
		return info{id, root, addedAt}, nil
	}

	a, err := load(idA)
	if err != nil {
		return 0, 0, err
	}
	b, err := load(idB)
	if err != nil {
		return 0, 0, err
	}

	rank := func(i info) int {
		if idx, ok := rootIndex[i.root]; ok {
			return idx
		}
		return len(libraryRoots)
	}

	ra, rb := rank(a), rank(b)
	switch {
	case ra != rb:
		if ra < rb {
			return a.id, b.id, nil
		}
		return b.id, a.id, nil
	case a.addedAt != b.addedAt:
		if a.addedAt < b.addedAt {
			return a.id, b.id, nil
		}
		return b.id, a.id, nil
	default:
		if a.id < b.id {
			return a.id, b.id, nil
		}
		return b.id, a.id, nil
	}
}

// MergeBooks folds dropID into keepID: moves book_locations and shelf_books
// rows, adds dropID's own canonical file as a new location under keepID,
// and deletes the dropID books row (book_enrichment cascades via its own
// FOREIGN KEY ... ON DELETE CASCADE). keepID's own book_enrichment/cover/
// metadata are left untouched -- the surviving book wins on conflicting
// fields.
func (d *DB) MergeBooks(keepID, dropID int64) error {
	if keepID == dropID {
		return nil
	}

	var root, path string
	var size, mtime int64
	err := d.sql.QueryRow(`SELECT library_root, file_path, file_size, file_mtime FROM books WHERE id = ?`, dropID).
		Scan(&root, &path, &size, &mtime)
	if err == sql.ErrNoRows {
		return nil // already merged away by an earlier pass
	}
	if err != nil {
		return err
	}

	if _, err := d.sql.Exec(`
		INSERT OR IGNORE INTO book_locations (book_id, library_root, file_path, file_size, file_mtime, added_at)
		VALUES (?, ?, ?, ?, ?, strftime('%s','now'))`,
		keepID, root, path, size, mtime); err != nil {
		return err
	}

	if _, err := d.sql.Exec(`UPDATE book_locations SET book_id = ? WHERE book_id = ?`, keepID, dropID); err != nil {
		return err
	}

	if _, err := d.sql.Exec(`
		INSERT OR IGNORE INTO shelf_books (shelf_id, book_id, added_at)
		SELECT shelf_id, ?, added_at FROM shelf_books WHERE book_id = ?`, keepID, dropID); err != nil {
		return err
	}
	if _, err := d.sql.Exec(`DELETE FROM shelf_books WHERE book_id = ?`, dropID); err != nil {
		return err
	}

	_, err = d.sql.Exec(`DELETE FROM books WHERE id = ?`, dropID)
	return err
}

// findBookByTitleAuthor looks up an existing books row with the same
// normalized (title, author), for scan-time duplicate detection. Returns
// ok=false (not an error) if title or author is blank -- too weak a key to
// match on (e.g. a parse-error row whose "title" is just the filename).
func (d *DB) findBookByTitleAuthor(title, author string) (id int64, ok bool, err error) {
	if title == "" || author == "" {
		return 0, false, nil
	}
	err = d.sql.QueryRow(`
		SELECT id FROM books
		WHERE LOWER(TRIM(title)) = LOWER(TRIM(?)) AND LOWER(TRIM(author)) = LOWER(TRIM(?))
		LIMIT 1`, title, author).Scan(&id)
	if err == sql.ErrNoRows {
		return 0, false, nil
	}
	if err != nil {
		return 0, false, err
	}
	return id, true, nil
}

// ConsolidateDuplicateTitles finds existing books rows that share a
// normalized (title, author) and merges every row beyond the pickCanonical
// winner into one. Cheap: a single ordered scan over the (small) books
// table, grouping consecutive equal keys in Go. Catches duplicates that
// predate this feature (e.g. from the earlier multi-root rollout) or that
// scan-time dedup's title/author check missed because both copies were
// already indexed as their own rows before the check existed.
func (d *DB) ConsolidateDuplicateTitles(libraryRoots []string) error {
	rows, err := d.sql.Query(`
		SELECT id, LOWER(TRIM(title)), LOWER(TRIM(author)) FROM books
		WHERE TRIM(title) != '' AND TRIM(author) != ''
		ORDER BY LOWER(TRIM(title)), LOWER(TRIM(author)), id`)
	if err != nil {
		return err
	}
	type row struct {
		id            int64
		title, author string
	}
	var all []row
	for rows.Next() {
		var r row
		if err := rows.Scan(&r.id, &r.title, &r.author); err != nil {
			rows.Close()
			return err
		}
		all = append(all, r)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return err
	}

	i := 0
	for i < len(all) {
		j := i + 1
		for j < len(all) && all[j].title == all[i].title && all[j].author == all[i].author {
			j++
		}
		if j-i > 1 {
			keepID := all[i].id
			for _, r := range all[i+1 : j] {
				k, drop, err := d.pickCanonical(keepID, r.id, libraryRoots)
				if err != nil {
					return err
				}
				if err := d.MergeBooks(k, drop); err != nil {
					return err
				}
				keepID = k
			}
		}
		i = j
	}
	return nil
}

// MergeDuplicateISBN looks for another books row whose book_enrichment.isbn
// matches bookID's, and merges them (per pickCanonical) if found. Intended
// to be called right after ApplyEnrichment fills in an ISBN, to catch
// duplicates whose titles differ too much for title+author matching
// (translations, subtitle variations) but share a real ISBN.
func (d *DB) MergeDuplicateISBN(bookID int64, libraryRoots []string) error {
	var isbn string
	if err := d.sql.QueryRow(`SELECT COALESCE(isbn, '') FROM book_enrichment WHERE book_id = ?`, bookID).Scan(&isbn); err != nil {
		if err == sql.ErrNoRows {
			return nil
		}
		return err
	}
	if isbn == "" {
		return nil
	}

	var otherID int64
	err := d.sql.QueryRow(`
		SELECT book_id FROM book_enrichment
		WHERE isbn = ? AND book_id != ?
		LIMIT 1`, isbn, bookID).Scan(&otherID)
	if err == sql.ErrNoRows {
		return nil
	}
	if err != nil {
		return err
	}

	keepID, dropID, err := d.pickCanonical(bookID, otherID, libraryRoots)
	if err != nil {
		return err
	}
	return d.MergeBooks(keepID, dropID)
}
