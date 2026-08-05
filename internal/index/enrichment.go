package index

import (
	"database/sql"
	"fmt"
)

// EnrichmentCandidate is the minimal data needed to search Hardcover for a
// book and decide whether to fill in blanks.
type EnrichmentCandidate struct {
	ID         int64
	Title      string
	Author     string
	Identifier string
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

// currentMerged returns the book's current merged (book_enrichment-over-
// books) series/series_index/published_date, the same values a listing read
// would see.
func (d *DB) currentMerged(bookID int64) (series string, seriesIndex float64, publishedDate string, err error) {
	var s, p sql.NullString
	var si sql.NullFloat64
	err = d.sql.QueryRow(`
		SELECT COALESCE(be.series, b.series), COALESCE(be.series_index, b.series_index), COALESCE(be.published_date, b.published_date)
		FROM books b LEFT JOIN book_enrichment be ON be.book_id = b.id
		WHERE b.id = ?`, bookID).Scan(&s, &si, &p)
	return s.String, si.Float64, p.String, err
}

func (d *DB) upsertEnrichment(bookID int64, series string, seriesIndex float64, publishedDate, status string) error {
	_, err := d.sql.Exec(`
		INSERT INTO book_enrichment (book_id, series, series_index, published_date, status, updated_at)
		VALUES (?, ?, ?, ?, ?, strftime('%s','now'))
		ON CONFLICT(book_id) DO UPDATE SET
			series = excluded.series,
			series_index = excluded.series_index,
			published_date = excluded.published_date,
			status = excluded.status,
			updated_at = excluded.updated_at`,
		bookID, nullIfEmpty(series), seriesIndex, nullIfEmpty(publishedDate), status)
	return err
}

// FillBlankMetadata sets series/series_index/published_date in
// book_enrichment only where the book's current merged (book_enrichment-over-
// books) value is blank — the auto-fill path, which never overwrites data
// that's already there, whether that data came from the EPUB or a prior
// enrichment. Also marks the book "done".
func (d *DB) FillBlankMetadata(bookID int64, series string, seriesIndex float64, publishedDate string) error {
	curSeries, curSeriesIndex, curPublishedDate, err := d.currentMerged(bookID)
	if err != nil {
		return err
	}

	finalSeries, finalSeriesIndex, finalDate := curSeries, curSeriesIndex, curPublishedDate
	if curSeries == "" {
		finalSeries, finalSeriesIndex = series, seriesIndex
	}
	if curPublishedDate == "" {
		finalDate = publishedDate
	}

	return d.upsertEnrichment(bookID, finalSeries, finalSeriesIndex, finalDate, "done")
}

// OverrideMetadata unconditionally sets series/series_index/published_date
// in book_enrichment (and title directly on books) — the manual,
// confirmed-by-a-human override path. Blank strings passed in mean "leave
// this field alone" (the confirm form only submits fields the admin chose
// to accept), not "clear it". Title isn't enrichment-derived data subject to
// the rescan-clobber problem, so it stays a direct books update.
func (d *DB) OverrideMetadata(bookID int64, title, series string, seriesIndex float64, publishedDate string) error {
	if title != "" {
		if _, err := d.sql.Exec(`UPDATE books SET title = ? WHERE id = ?`, title, bookID); err != nil {
			return err
		}
	}

	curSeries, curSeriesIndex, curPublishedDate, err := d.currentMerged(bookID)
	if err != nil {
		return err
	}

	finalSeries, finalSeriesIndex, finalDate := curSeries, curSeriesIndex, curPublishedDate
	if series != "" {
		finalSeries, finalSeriesIndex = series, seriesIndex
	}
	if publishedDate != "" {
		finalDate = publishedDate
	}

	return d.upsertEnrichment(bookID, finalSeries, finalSeriesIndex, finalDate, "done")
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
