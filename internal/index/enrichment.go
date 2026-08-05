package index

import "fmt"

// EnrichmentCandidate is the minimal data needed to search Hardcover for a
// book and decide whether to fill in blanks.
type EnrichmentCandidate struct {
	ID     int64
	Title  string
	Author string
}

// BooksNeedingEnrichment returns up to limit books that haven't been checked
// against Hardcover yet (enrichment_status = ”) and are missing series or
// release-date metadata — the only fields auto-fill ever touches. Already
// `done`/`no_match`/`error` books are skipped so a restart resumes instead
// of reprocessing the whole library.
func (d *DB) BooksNeedingEnrichment(limit int) ([]EnrichmentCandidate, error) {
	rows, err := d.sql.Query(`
		SELECT id, title, author FROM books
		WHERE enrichment_status = ''
		AND (series IS NULL OR series = '' OR published_date IS NULL OR published_date = '')
		ORDER BY id
		LIMIT ?`, limit)
	if err != nil {
		return nil, fmt.Errorf("list books needing enrichment: %w", err)
	}
	defer rows.Close()

	var out []EnrichmentCandidate
	for rows.Next() {
		var c EnrichmentCandidate
		if err := rows.Scan(&c.ID, &c.Title, &c.Author); err != nil {
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
	_, err := d.sql.Exec(`UPDATE books SET enrichment_status = ? WHERE id = ?`, status, bookID)
	return err
}

// EnrichmentStats summarizes enrichment progress for the admin page.
type EnrichmentStats struct {
	Pending int // enrichment_status = '' and still missing series/date
	Done    int
	NoMatch int
	Errored int
}

func (d *DB) GetEnrichmentStats() (EnrichmentStats, error) {
	var s EnrichmentStats
	err := d.sql.QueryRow(`
		SELECT
			COUNT(*) FILTER (WHERE enrichment_status = '' AND (series IS NULL OR series = '' OR published_date IS NULL OR published_date = '')),
			COUNT(*) FILTER (WHERE enrichment_status = 'done'),
			COUNT(*) FILTER (WHERE enrichment_status = 'no_match'),
			COUNT(*) FILTER (WHERE enrichment_status = 'error')
		FROM books`).Scan(&s.Pending, &s.Done, &s.NoMatch, &s.Errored)
	return s, err
}

// FillBlankMetadata sets series/series_index/published_date only where the
// book's current value is blank — the auto-fill path, which never
// overwrites data that's already there.
func (d *DB) FillBlankMetadata(bookID int64, series string, seriesIndex float64, publishedDate string) error {
	_, err := d.sql.Exec(`
		UPDATE books SET
			series = CASE WHEN series IS NULL OR series = '' THEN ? ELSE series END,
			series_index = CASE WHEN series IS NULL OR series = '' THEN ? ELSE series_index END,
			published_date = CASE WHEN published_date IS NULL OR published_date = '' THEN ? ELSE published_date END
		WHERE id = ?`,
		nullIfEmpty(series), seriesIndex, nullIfEmpty(publishedDate), bookID)
	return err
}

// OverrideMetadata unconditionally sets title/series/series_index/published_date
// — the manual, confirmed-by-a-human override path. Blank strings passed in
// mean "leave this field alone" (the confirm form only submits fields the
// admin chose to accept), not "clear it".
func (d *DB) OverrideMetadata(bookID int64, title, series string, seriesIndex float64, publishedDate string) error {
	_, err := d.sql.Exec(`
		UPDATE books SET
			title = CASE WHEN ? != '' THEN ? ELSE title END,
			series = CASE WHEN ? != '' THEN ? ELSE series END,
			series_index = CASE WHEN ? != '' THEN ? ELSE series_index END,
			published_date = CASE WHEN ? != '' THEN ? ELSE published_date END
		WHERE id = ?`,
		title, title, series, series, series, seriesIndex, publishedDate, publishedDate, bookID)
	return err
}

func nullIfEmpty(s string) any {
	if s == "" {
		return nil
	}
	return s
}
