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

// Scan walks libraryPath for .epub files and upserts them into the index,
// then deletes rows for files that no longer exist. Per-file parse errors
// are logged and stored on the row rather than aborting the scan. Records
// last_scan_at/last_scan_duration_ms in the meta table on success.
func (d *DB) Scan(libraryPath string, saver CoverSaver) error {
	start := time.Now()
	if err := d.scan(libraryPath, saver); err != nil {
		return err
	}

	_ = d.SetMeta("last_scan_at", strconv.FormatInt(time.Now().Unix(), 10))
	_ = d.SetMeta("last_scan_duration_ms", strconv.FormatInt(time.Since(start).Milliseconds(), 10))
	return nil
}

func (d *DB) scan(libraryPath string, saver CoverSaver) error {
	seen := make(map[string]bool)

	err := filepath.WalkDir(libraryPath, func(path string, entry fs.DirEntry, err error) error {
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

		rel, err := filepath.Rel(libraryPath, path)
		if err != nil {
			rel = path
		}
		seen[rel] = true

		info, err := entry.Info()
		if err != nil {
			log.Printf("scan: stat error for %s: %v", rel, err)
			return nil
		}

		if err := d.upsertIfChanged(rel, path, info, saver); err != nil {
			log.Printf("scan: upsert error for %s: %v", rel, err)
		}
		return nil
	})
	if err != nil {
		return fmt.Errorf("walk library: %w", err)
	}

	if err := d.pruneMissing(seen); err != nil {
		return fmt.Errorf("prune missing: %w", err)
	}

	return nil
}

func (d *DB) upsertIfChanged(relPath, fullPath string, info fs.FileInfo, saver CoverSaver) error {
	size := info.Size()
	mtime := info.ModTime().Unix()

	var existingID int64
	var existingSize, existingMtime int64
	err := d.sql.QueryRow(`SELECT id, file_size, file_mtime FROM books WHERE file_path = ?`, relPath).
		Scan(&existingID, &existingSize, &existingMtime)
	switch {
	case err == sql.ErrNoRows:
		// new file, proceed to parse+insert
	case err != nil:
		return err
	default:
		if existingSize == size && existingMtime == mtime {
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
			UPDATE books SET file_size=?, file_mtime=?, title=?, sort_title=?, author=?, sort_author=?,
				series=?, series_index=?, description=?, language=?, publisher=?, published_date=?, identifier=?,
				cover_path=?, has_cover=?, updated_at=?, parse_error=?
			WHERE id=?`,
			size, mtime, title, sortTitle, author, sortAuthor,
			nullableString(series), nullableFloat(seriesIndex, series != ""), nullableString(description),
			nullableString(language), nullableString(publisher), nullableString(publishedDate), nullableString(identifier),
			nullableString(coverPath), boolToInt(hasCover), now, nullableString(parseErrStr),
			existingID,
		)
		return err
	}

	res, err := d.sql.Exec(`
		INSERT INTO books (file_path, file_size, file_mtime, title, sort_title, author, sort_author,
			series, series_index, description, language, publisher, published_date, identifier,
			cover_path, has_cover, added_at, updated_at, parse_error)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		relPath, size, mtime, title, sortTitle, author, sortAuthor,
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

func (d *DB) pruneMissing(seen map[string]bool) error {
	rows, err := d.sql.Query(`SELECT id, file_path FROM books`)
	if err != nil {
		return err
	}
	var stale []int64
	for rows.Next() {
		var id int64
		var path string
		if err := rows.Scan(&id, &path); err != nil {
			rows.Close()
			return err
		}
		if !seen[path] {
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

// DeleteByPath removes the indexed row for a single file path, used by the fsnotify watcher.
func (d *DB) DeleteByPath(relPath string) error {
	_, err := d.sql.Exec(`DELETE FROM books WHERE file_path = ?`, relPath)
	return err
}

// UpsertPath re-scans a single file, used by the fsnotify watcher.
func (d *DB) UpsertPath(libraryPath, relPath string, saver CoverSaver) error {
	fullPath := filepath.Join(libraryPath, relPath)
	info, err := os.Stat(fullPath)
	if err != nil {
		return err
	}
	return d.upsertIfChanged(relPath, fullPath, info, saver)
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
