package index

import (
	"database/sql"
	"fmt"
	"strings"
)

// EnrichmentCandidate is the minimal data needed to search Hardcover for a
// book and decide whether to fill in blanks.
type EnrichmentCandidate struct {
	ID         int64
	Title      string
	Author     string
	Identifier string
}

// HardcoverFields is everything a Hardcover match can contribute to a book,
// passed to ApplyEnrichment. Blank/zero fields mean "Hardcover didn't have
// this", not "clear the existing value" — see ApplyEnrichment for how each
// field is merged.
type HardcoverFields struct {
	Title         string
	Series        string
	SeriesIndex   float64
	PublishedDate string
	Description   string
	Genres        []string
	Publisher     string
	Pages         int
	ISBN          string
	Rating        float64
}

// BooksNeedingEnrichment returns up to limit books that haven't been checked
// against Hardcover yet (enrichment status = ”) and are missing series or
// release-date metadata — the only fields auto-fill ever touches. Already
// `done`/`no_match`/`error` books are skipped so a restart resumes instead
// of reprocessing the whole library. Both the status and the series/date
// blankness checks look at the merged (book_enrichment-over-books) value.
func (d *DB) BooksNeedingEnrichment(limit int) ([]EnrichmentCandidate, error) {
	rows, err := d.sql.Query(`
		SELECT b.id, b.title, b.author, COALESCE(b.identifier, '') FROM books b
		LEFT JOIN book_enrichment be ON be.book_id = b.id
		WHERE COALESCE(be.status, '') = ''
		AND (
			COALESCE(be.series, b.series) IS NULL OR COALESCE(be.series, b.series) = ''
			OR COALESCE(be.published_date, b.published_date) IS NULL OR COALESCE(be.published_date, b.published_date) = ''
		)
		ORDER BY b.id
		LIMIT ?`, limit)
	if err != nil {
		return nil, fmt.Errorf("list books needing enrichment: %w", err)
	}
	defer rows.Close()

	var out []EnrichmentCandidate
	for rows.Next() {
		var c EnrichmentCandidate
		if err := rows.Scan(&c.ID, &c.Title, &c.Author, &c.Identifier); err != nil {
			return nil, err
		}
		out = append(out, c)
	}
	return out, rows.Err()
}

// SetEnrichmentStatus records the outcome of an enrichment attempt for a
// book, so it isn't retried every scan/queue pass. Valid statuses: "done",
// "no_match", "error".
func (d *DB) SetEnrichmentStatus(bookID int64, status string) error {
	_, err := d.sql.Exec(`
		INSERT INTO book_enrichment (book_id, status, updated_at) VALUES (?, ?, strftime('%s','now'))
		ON CONFLICT(book_id) DO UPDATE SET status = excluded.status, updated_at = excluded.updated_at`,
		bookID, status)
	return err
}

// EnrichmentStats summarizes enrichment progress for the admin page.
type EnrichmentStats struct {
	Pending int // status = '' and still missing series/date
	Done    int
	NoMatch int
	Errored int
}

func (d *DB) GetEnrichmentStats() (EnrichmentStats, error) {
	var s EnrichmentStats
	err := d.sql.QueryRow(`
		SELECT
			COUNT(*) FILTER (WHERE COALESCE(be.status, '') = '' AND (
				COALESCE(be.series, b.series) IS NULL OR COALESCE(be.series, b.series) = ''
				OR COALESCE(be.published_date, b.published_date) IS NULL OR COALESCE(be.published_date, b.published_date) = ''
			)),
			COUNT(*) FILTER (WHERE be.status = 'done'),
			COUNT(*) FILTER (WHERE be.status = 'no_match'),
			COUNT(*) FILTER (WHERE be.status = 'error')
		FROM books b
		LEFT JOIN book_enrichment be ON be.book_id = b.id`).Scan(&s.Pending, &s.Done, &s.NoMatch, &s.Errored)
	return s, err
}

// mergedFields is the book's current merged (book_enrichment-over-books)
// values for every field ApplyEnrichment/OverrideMetadata can touch — the
// same values a listing read would see.
type mergedFields struct {
	title         string
	series        string
	seriesIndex   float64
	publishedDate string
	description   string
	genres        string // raw CSV, as stored
	publisher     string
	pages         int64
	isbn          string
	rating        float64
}

func (d *DB) currentEnrichmentMerged(bookID int64) (mergedFields, error) {
	var m mergedFields
	var title, series, publishedDate, description, genres, publisher, isbn sql.NullString
	var seriesIndex, rating sql.NullFloat64
	var pages sql.NullInt64
	err := d.sql.QueryRow(`
		SELECT `+effectiveTitle+`, `+effectiveSeries+`, `+effectiveSeriesIndex+`, `+effectivePublishedDate+`,
			`+effectiveDescription+`, be.genres, `+effectivePublisher+`, be.pages, be.isbn, be.rating
		FROM books b LEFT JOIN book_enrichment be ON be.book_id = b.id
		WHERE b.id = ?`, bookID).Scan(&title, &series, &seriesIndex, &publishedDate, &description, &genres, &publisher, &pages, &isbn, &rating)
	if err != nil {
		return m, err
	}
	m.title = title.String
	m.series = series.String
	m.seriesIndex = seriesIndex.Float64
	m.publishedDate = publishedDate.String
	m.description = description.String
	m.genres = genres.String
	m.publisher = publisher.String
	m.pages = pages.Int64
	m.isbn = isbn.String
	m.rating = rating.Float64
	return m, nil
}

func (d *DB) upsertEnrichment(bookID int64, f mergedFields, status string) error {
	_, err := d.sql.Exec(`
		INSERT INTO book_enrichment (book_id, title, series, series_index, published_date, description, genres, publisher, pages, isbn, rating, status, updated_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, strftime('%s','now'))
		ON CONFLICT(book_id) DO UPDATE SET
			title = excluded.title,
			series = excluded.series,
			series_index = excluded.series_index,
			published_date = excluded.published_date,
			description = excluded.description,
			genres = excluded.genres,
			publisher = excluded.publisher,
			pages = excluded.pages,
			isbn = excluded.isbn,
			rating = excluded.rating,
			status = excluded.status,
			updated_at = excluded.updated_at`,
		bookID, nullIfEmpty(f.title), nullIfEmpty(f.series), f.seriesIndex, nullIfEmpty(f.publishedDate),
		nullIfEmpty(f.description), nullIfEmpty(f.genres), nullIfEmpty(f.publisher),
		nullIfZeroInt(f.pages), nullIfEmpty(f.isbn), nullIfZeroFloat(f.rating), status)
	return err
}

// ApplyEnrichment is the background queue's auto-fill path for a confident
// Hardcover match. Title is always overwritten with Hardcover's title (per
// product decision — Hardcover's title is trusted over whatever the EPUB's
// OPF metadata says); every other field is filled in only where the book's
// current merged value is blank, matching the existing series/date
// auto-fill behavior so nothing already present (from the EPUB or a prior
// enrichment) gets clobbered. Also marks the book "done".
func (d *DB) ApplyEnrichment(bookID int64, hc HardcoverFields) error {
	cur, err := d.currentEnrichmentMerged(bookID)
	if err != nil {
		return err
	}

	final := cur
	if hc.Title != "" {
		final.title = hc.Title
	}
	if cur.series == "" {
		final.series, final.seriesIndex = hc.Series, hc.SeriesIndex
	}
	if cur.publishedDate == "" {
		final.publishedDate = hc.PublishedDate
	}
	if cur.description == "" {
		final.description = hc.Description
	}
	if cur.genres == "" && len(hc.Genres) > 0 {
		final.genres = joinCSV(hc.Genres)
	}
	if cur.publisher == "" {
		final.publisher = hc.Publisher
	}
	if cur.pages == 0 {
		final.pages = int64(hc.Pages)
	}
	if cur.isbn == "" {
		final.isbn = hc.ISBN
	}
	if cur.rating == 0 {
		final.rating = hc.Rating
	}

	return d.upsertEnrichment(bookID, final, "done")
}

// OverrideMetadata unconditionally sets title/series/series_index/
// published_date in book_enrichment — the manual, confirmed-by-a-human
// override path (Check Hardcover / Apply admin flow). Blank strings passed
// in mean "leave this field alone" (the confirm form only submits fields the
// admin chose to accept), not "clear it". Title is written into
// book_enrichment (not directly to books) so it survives a later rescan, the
// same as ApplyEnrichment's automatic path.
func (d *DB) OverrideMetadata(bookID int64, title, series string, seriesIndex float64, publishedDate string) error {
	cur, err := d.currentEnrichmentMerged(bookID)
	if err != nil {
		return err
	}

	final := cur
	if title != "" {
		final.title = title
	}
	if series != "" {
		final.series, final.seriesIndex = series, seriesIndex
	}
	if publishedDate != "" {
		final.publishedDate = publishedDate
	}

	return d.upsertEnrichment(bookID, final, "done")
}

// ResetEnrichment clears all Hardcover-derived data for every book, without
// touching books' own EPUB-scanned data at all. BooksNeedingEnrichment then
// naturally picks every book back up on the queue's next pass.
func (d *DB) ResetEnrichment() error {
	_, err := d.sql.Exec(`DELETE FROM book_enrichment`)
	return err
}

func nullIfEmpty(s string) any {
	if s == "" {
		return nil
	}
	return s
}

func nullIfZeroInt(n int64) any {
	if n == 0 {
		return nil
	}
	return n
}

func nullIfZeroFloat(f float64) any {
	if f == 0 {
		return nil
	}
	return f
}

func joinCSV(items []string) string {
	return strings.Join(items, ",")
}

func splitCSV(s string) []string {
	if s == "" {
		return nil
	}
	return strings.Split(s, ",")
}
