package index

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"time"

	"github.com/sethwv/my-eink-library/internal/chaptarr"
)

// LoadChaptarrCache implements chaptarr.CacheStore.
func (d *DB) LoadChaptarrCache(scope string) ([]chaptarr.Book, time.Time, error) {
	var data string
	var refreshedAt int64
	err := d.sql.QueryRow(`SELECT books_json, refreshed_at FROM chaptarr_catalog_cache WHERE scope = ?`, scope).Scan(&data, &refreshedAt)
	if err == sql.ErrNoRows {
		return nil, time.Time{}, nil
	}
	if err != nil {
		return nil, time.Time{}, fmt.Errorf("load chaptarr cache: %w", err)
	}
	var books []chaptarr.Book
	if err := json.Unmarshal([]byte(data), &books); err != nil {
		return nil, time.Time{}, fmt.Errorf("decode chaptarr cache: %w", err)
	}
	return books, time.Unix(refreshedAt, 0), nil
}

// SaveChaptarrCache implements chaptarr.CacheStore.
func (d *DB) SaveChaptarrCache(scope string, books []chaptarr.Book, refreshedAt time.Time) error {
	data, err := json.Marshal(books)
	if err != nil {
		return fmt.Errorf("encode chaptarr cache: %w", err)
	}
	_, err = d.sql.Exec(`INSERT INTO chaptarr_catalog_cache (scope, refreshed_at, books_json) VALUES (?, ?, ?)
		ON CONFLICT(scope) DO UPDATE SET refreshed_at = excluded.refreshed_at, books_json = excluded.books_json`, scope, refreshedAt.Unix(), data)
	if err != nil {
		return fmt.Errorf("save chaptarr cache: %w", err)
	}
	return nil
}

// ClearChaptarrCache implements chaptarr.CacheStore.
func (d *DB) ClearChaptarrCache(scope string) error {
	_, err := d.sql.Exec(`DELETE FROM chaptarr_catalog_cache WHERE scope = ?`, scope)
	return err
}
