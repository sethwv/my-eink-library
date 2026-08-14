package index

import (
	"database/sql"
	"fmt"
	"io/fs"
	"log"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/swvn/eink-library/internal/epub"
)

// CoverSaver persists a book's extracted cover image and returns a path
// (relative to the thumbnail cache dir) to store in cover_path. Implemented
// by internal/thumbnail; scan works fine with a nil saver (no thumbnails yet).
type CoverSaver interface {
	SaveCover(bookID int64, data []byte, mediaType string) (relPath string, err error)
}

// Scan walks each of libraryRoots for .epub files and upserts them into the
// index, then deletes rows for files that no longer exist in any of them.
// Per-file parse errors are logged and stored on the row rather than
// aborting the scan. Records last_scan_at/last_scan_duration_ms in the meta
// table on success. Files whose size/mtime haven't changed since the last
// scan are skipped without re-parsing; use Reimport to force a full re-parse.
func (d *DB) Scan(libraryRoots []string, saver CoverSaver) error {
	start := time.Now()
	if err := d.scan(libraryRoots, saver, false); err != nil {
		return err
	}

	_ = d.SetMeta("last_scan_at", strconv.FormatInt(time.Now().Unix(), 10))
	_ = d.SetMeta("last_scan_duration_ms", strconv.FormatInt(time.Since(start).Milliseconds(), 10))
	return nil
}

// Reimport is like Scan but re-parses every EPUB regardless of whether its
// file has changed, so an EPUB-metadata-parsing fix (e.g. improved
// identifier extraction) applies to books that are already indexed. Each
// book's added_at is preserved: the UPDATE path used for existing files
// never touches that column, only the INSERT path (new files) does.
func (d *DB) Reimport(libraryRoots []string, saver CoverSaver) error {
	start := time.Now()
	if err := d.scan(libraryRoots, saver, true); err != nil {
		return err
	}

	_ = d.SetMeta("last_scan_at", strconv.FormatInt(time.Now().Unix(), 10))
	_ = d.SetMeta("last_scan_duration_ms", strconv.FormatInt(time.Since(start).Milliseconds(), 10))
	return nil
}

// seenKey identifies a file within a specific library root, so two roots
// that happen to contain the same relative path (e.g. "Author/Book.epub")
// aren't treated as the same book during pruning.
type seenKey struct {
	root string
	rel  string
}

func (d *DB) scan(libraryRoots []string, saver CoverSaver, force bool) error {
	seen := make(map[seenKey]bool)

	for _, root := range libraryRoots {
		err := filepath.WalkDir(root, func(path string, entry fs.DirEntry, err error) error {
			if err != nil {
				log.Printf("scan: walk error at %s: %v", path, err)
				return nil
			}
			if entry.IsDir() {
				return nil
			}
			if !strings.EqualFold(filepath.Ext(entry.Name()), ".epub") {
				return nil
			}

			rel, err := filepath.Rel(root, path)
			if err != nil {
				rel = path
			}
			seen[seenKey{root, rel}] = true

			info, err := entry.Info()
			if err != nil {
				log.Printf("scan: stat error for %s: %v", rel, err)
				return nil
			}

			if err := d.upsertIfChanged(root, rel, path, info, saver, force); err != nil {
				log.Printf("scan: upsert error for %s: %v", rel, err)
			}
			return nil
		})
		if err != nil {
			return fmt.Errorf("walk library %s: %w", root, err)
		}
	}

	if err := d.pruneMissing(seen); err != nil {
		return fmt.Errorf("prune missing: %w", err)
	}

	return nil
}

func (d *DB) upsertIfChanged(root, relPath, fullPath string, info fs.FileInfo, saver CoverSaver, force bool) error {
	size := info.Size()
	mtime := info.ModTime().Unix()

	var existingID int64
	var existingSize, existingMtime int64
	err := d.sql.QueryRow(`SELECT id, file_size, file_mtime FROM books WHERE library_root = ? AND file_path = ?`, root, relPath).
		Scan(&existingID, &existingSize, &existingMtime)
	switch {
	case err == sql.ErrNoRows:
		// new file, proceed to parse+insert
	case err != nil:
		return err
	default:
		if !force && existingSize == size && existingMtime == mtime {
			return nil // unchanged, skip re-parsing
		}
	}

	now := time.Now().Unix()
	meta, parseErr := epub.ParseFile(fullPath)

	var title, sortTitle, author, sortAuthor, series, description, language, publisher, publishedDate, identifier string
	var seriesIndex float64
	var parseErrStr string

	if parseErr != nil {
		title = filepath.Base(relPath)
		sortTitle = strings.ToLower(title)
		parseErrStr = parseErr.Error()
		log.Printf("scan: failed to parse %s: %v", relPath, parseErr)
	} else {
		title = meta.Title
		sortTitle = meta.SortTitle
		author = meta.Author
		sortAuthor = meta.SortAuthor
		series = meta.Series
		seriesIndex = meta.SeriesIndex
		description = meta.Description
		language = meta.Language
		publisher = meta.Publisher
		publishedDate = meta.PublishedDate
		identifier = meta.Identifier
	}

	var coverPath string
	hasCover := parseErr == nil && meta.CoverData != nil

	if existingID != 0 {
		if hasCover && saver != nil {
			if p, err := saver.SaveCover(existingID, meta.CoverData, meta.CoverMediaType); err == nil {
				coverPath = p
			} else {
				log.Printf("scan: cover save failed for %s: %v", relPath, err)
			}
		}
		_, err := d.sql.Exec(`
			UPDATE books SET library_root=?, file_size=?, file_mtime=?, title=?, sort_title=?, author=?, sort_author=?,
				series=?, series_index=?, description=?, language=?, publisher=?, published_date=?, identifier=?,
				cover_path=?, has_cover=?, updated_at=?, parse_error=?
			WHERE id=?`,
			root, size, mtime, title, sortTitle, author, sortAuthor,
			nullableString(series), nullableFloat(seriesIndex, series != ""), nullableString(description),
			nullableString(language), nullableString(publisher), nullableString(publishedDate), nullableString(identifier),
			nullableString(coverPath), boolToInt(hasCover), now, nullableString(parseErrStr),
			existingID,
		)
		return err
	}

	res, err := d.sql.Exec(`
		INSERT INTO books (library_root, file_path, file_size, file_mtime, title, sort_title, author, sort_author,
			series, series_index, description, language, publisher, published_date, identifier,
			cover_path, has_cover, added_at, updated_at, parse_error)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		root, relPath, size, mtime, title, sortTitle, author, sortAuthor,
		nullableString(series), nullableFloat(seriesIndex, series != ""), nullableString(description),
		nullableString(language), nullableString(publisher), nullableString(publishedDate), nullableString(identifier),
		nullableString(coverPath), boolToInt(hasCover), now, now, nullableString(parseErrStr),
	)
	if err != nil {
		return err
	}

	if hasCover && saver != nil {
		bookID, err := res.LastInsertId()
		if err == nil {
			if p, err := saver.SaveCover(bookID, meta.CoverData, meta.CoverMediaType); err == nil {
				_, _ = d.sql.Exec(`UPDATE books SET cover_path=? WHERE id=?`, p, bookID)
			} else {
				log.Printf("scan: cover save failed for %s: %v", relPath, err)
			}
		}
	}

	return nil
}

func (d *DB) pruneMissing(seen map[seenKey]bool) error {
	rows, err := d.sql.Query(`SELECT id, library_root, file_path FROM books`)
	if err != nil {
		return err
	}
	var stale []int64
	for rows.Next() {
		var id int64
		var root, path string
		if err := rows.Scan(&id, &root, &path); err != nil {
			rows.Close()
			return err
		}
		if !seen[seenKey{root, path}] {
			stale = append(stale, id)
		}
	}
	rows.Close()

	for _, id := range stale {
		if _, err := d.sql.Exec(`DELETE FROM books WHERE id = ?`, id); err != nil {
			return err
		}
	}
	return nil
}

// DeleteByPath removes the indexed row for a single file path within root, used by the fsnotify watcher.
func (d *DB) DeleteByPath(root, relPath string) error {
	_, err := d.sql.Exec(`DELETE FROM books WHERE library_root = ? AND file_path = ?`, root, relPath)
	return err
}

// UpsertPath re-scans a single file within root, used by the fsnotify watcher.
func (d *DB) UpsertPath(root, relPath string, saver CoverSaver) error {
	fullPath := filepath.Join(root, relPath)
	info, err := os.Stat(fullPath)
	if err != nil {
		return err
	}
	return d.upsertIfChanged(root, relPath, fullPath, info, saver, false)
}

func nullableString(s string) any {
	if s == "" {
		return nil
	}
	return s
}

func nullableFloat(f float64, has bool) any {
	if !has {
		return nil
	}
	return f
}

func boolToInt(b bool) int {
	if b {
		return 1
	}
	return 0
}
