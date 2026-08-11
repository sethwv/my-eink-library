package index

import (
	"database/sql"
	"fmt"
	"strings"
)

// EnrichmentCandidate is the minimal data needed to search Hardcover (or
// match against Chaptarr, by FilePath) for a book and decide whether to
// fill in blanks.
type EnrichmentCandidate struct {
	ID         int64
	Title      string
	Author     string
	Identifier string
	FilePath   string
}

// Enrichment source values stored in book_enrichment.source, surfaced on
// the Edit Metadata page so an admin can see which integration (if any)
// currently supplies a book's fields. SourceChaptarr takes precedence over
// SourceHardcover in the background queue (see internal/web's
// RunEnrichmentQueue/RunChaptarrQueue) — a book Chaptarr already claimed is
// never touched by the Hardcover pass, since both gate on the same
// needsEnrichmentWhere status=” check.
const (
	SourceHardcover = "hardcover"
	SourceChaptarr  = "chaptarr"
	SourceManual    = "manual"
)

// HardcoverFields is everything a Hardcover or Chaptarr match can
// contribute to a book, passed to ApplyEnrichment. Blank/zero fields mean
// "the source didn't have this", not "clear the existing value" — see
// ApplyEnrichment for how each field is merged. Named for Hardcover (the
// original and richer of the two sources) but reused for Chaptarr too,
// since both integrations contribute to the same merged field set.
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

// needsEnrichmentWhere gates both BooksNeedingEnrichment and
// GetEnrichmentStats: a book is still a candidate if it hasn't been checked
// yet (status = ”) and is missing any field Hardcover can contribute —
// series, release date, description, genres, publisher, page count, ISBN,
// rating, or a cover image. Kept as one shared fragment so the candidate
// query and the stats query can't drift apart.
const needsEnrichmentWhere = `COALESCE(be.status, '') = ''
	AND (
		COALESCE(be.series, b.series) IS NULL OR COALESCE(be.series, b.series) = ''
		OR COALESCE(be.published_date, b.published_date) IS NULL OR COALESCE(be.published_date, b.published_date) = ''
		OR COALESCE(be.description, b.description) IS NULL OR COALESCE(be.description, b.description) = ''
		OR be.genres IS NULL OR be.genres = ''
		OR COALESCE(be.publisher, b.publisher) IS NULL OR COALESCE(be.publisher, b.publisher) = ''
		OR be.pages IS NULL OR be.pages = 0
		OR be.isbn IS NULL OR be.isbn = ''
		OR be.rating IS NULL OR be.rating = 0
		OR b.has_cover = 0
	)`

// BooksNeedingEnrichment returns up to limit books that haven't been checked
// against Hardcover yet (enrichment status = ”) and are missing at least one
// field auto-fill can contribute (see needsEnrichmentWhere). Already
// `done`/`no_match`/`error` books are skipped so a restart resumes instead
// of reprocessing the whole library.
func (d *DB) BooksNeedingEnrichment(limit int) ([]EnrichmentCandidate, error) {
	rows, err := d.sql.Query(`
		SELECT b.id, b.title, b.author, COALESCE(b.identifier, ''), b.file_path FROM books b
		LEFT JOIN book_enrichment be ON be.book_id = b.id
		WHERE `+needsEnrichmentWhere+`
		ORDER BY b.id
		LIMIT ?`, limit)
	if err != nil {
		return nil, fmt.Errorf("list books needing enrichment: %w", err)
	}
	defer rows.Close()

	var out []EnrichmentCandidate
	for rows.Next() {
		var c EnrichmentCandidate
		if err := rows.Scan(&c.ID, &c.Title, &c.Author, &c.Identifier, &c.FilePath); err != nil {
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
	Pending int // still a candidate per needsEnrichmentWhere
	Done    int
	NoMatch int
	Errored int
}

func (d *DB) GetEnrichmentStats() (EnrichmentStats, error) {
	var s EnrichmentStats
	err := d.sql.QueryRow(`
		SELECT
			COUNT(*) FILTER (WHERE `+needsEnrichmentWhere+`),
			COUNT(*) FILTER (WHERE be.status = 'done'),
			COUNT(*) FILTER (WHERE be.status = 'no_match'),
			COUNT(*) FILTER (WHERE be.status = 'error')
		FROM books b
		LEFT JOIN book_enrichment be ON be.book_id = b.id`).Scan(&s.Pending, &s.Done, &s.NoMatch, &s.Errored)
	return s, err
}

// mergedFields is the book's current merged (book_enrichment-over-books)
// values for every field ApplyEnrichment/SaveMetadata can touch — the
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

// GetEnrichmentSource returns the book_enrichment.source value for bookID
// ("hardcover"/"chaptarr"/"manual"/"" for not-yet-enriched), for display on
// the Edit Metadata page. Returns "" with no error if the book has no
// book_enrichment row yet.
func (d *DB) GetEnrichmentSource(bookID int64) (string, error) {
	var source sql.NullString
	err := d.sql.QueryRow(`SELECT source FROM book_enrichment WHERE book_id = ?`, bookID).Scan(&source)
	if err == sql.ErrNoRows {
		return "", nil
	}
	if err != nil {
		return "", err
	}
	return source.String, nil
}

func (d *DB) upsertEnrichment(bookID int64, f mergedFields, status, source string) error {
	_, err := d.sql.Exec(`
		INSERT INTO book_enrichment (book_id, title, series, series_index, published_date, description, genres, publisher, pages, isbn, rating, status, source, updated_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, strftime('%s','now'))
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
			source = excluded.source,
			updated_at = excluded.updated_at`,
		bookID, nullIfEmpty(f.title), nullIfEmpty(f.series), f.seriesIndex, nullIfEmpty(f.publishedDate),
		nullIfEmpty(f.description), nullIfEmpty(f.genres), nullIfEmpty(f.publisher),
		nullIfZeroInt(f.pages), nullIfEmpty(f.isbn), nullIfZeroFloat(f.rating), status, source)
	return err
}

// placeholderDescriptionMaxLen is the length below which an existing
// description is considered too thin to be worth keeping over Hardcover's
// (most EPUB "descriptions" that short are a blurb fragment, not real
// jacket copy).
const placeholderDescriptionMaxLen = 40

// descriptionIsPlaceholder reports whether cur is worth replacing with
// Hardcover's description even though it isn't blank — either it's short
// enough to be a stub, or it's just the book's own title repeated back.
func descriptionIsPlaceholder(cur, title string) bool {
	cur = strings.TrimSpace(cur)
	if cur == "" {
		return true
	}
	if len(cur) < placeholderDescriptionMaxLen {
		return true
	}
	if strings.EqualFold(cur, strings.TrimSpace(title)) {
		return true
	}
	return false
}

// ApplyEnrichment is the background queue's auto-fill path for a confident
// Hardcover match. Fields differ in how eagerly they trust Hardcover over
// what's already there:
//   - Title: always overwritten (Hardcover's title is trusted over the
//     EPUB's OPF metadata).
//   - Series/series index: overwritten if currently blank, or if Hardcover's
//     series differs from the current value — a confident match means
//     Hardcover's series/index is trusted over a stale or wrong EPUB value.
//   - Description: overwritten if blank or "placeholder-like" (short, or
//     just the title repeated), otherwise a substantial existing
//     description is left alone.
//   - Genres: always replaced with Hardcover's list when Hardcover returned
//     any (EPUB genre tags are rarely as good).
//   - Publisher/pages/isbn/rating: always take Hardcover's value when it has
//     one — EPUB OPF metadata for these is typically worse or absent.
//
// Also marks the book "done" with the given source (SourceHardcover or
// SourceChaptarr — whichever integration produced hc).
func (d *DB) ApplyEnrichment(bookID int64, hc HardcoverFields, source string) error {
	cur, err := d.currentEnrichmentMerged(bookID)
	if err != nil {
		return err
	}

	final := cur
	if hc.Title != "" {
		final.title = hc.Title
	}
	if hc.Series != "" && (cur.series == "" || !strings.EqualFold(cur.series, hc.Series)) {
		final.series, final.seriesIndex = hc.Series, hc.SeriesIndex
	}
	if cur.publishedDate == "" {
		final.publishedDate = hc.PublishedDate
	}
	if hc.Description != "" && descriptionIsPlaceholder(cur.description, final.title) {
		final.description = hc.Description
	}
	if len(hc.Genres) > 0 {
		final.genres = joinCSV(hc.Genres)
	}
	if hc.Publisher != "" {
		final.publisher = hc.Publisher
	}
	if hc.Pages != 0 {
		final.pages = int64(hc.Pages)
	}
	if hc.ISBN != "" {
		final.isbn = hc.ISBN
	}
	if hc.Rating != 0 {
		final.rating = hc.Rating
	}

	return d.upsertEnrichment(bookID, final, "done", source)
}

// MetadataFields is every field the Edit Metadata page can write to
// book_enrichment. A blank string, zero float, or zero int means "leave this
// field alone" — the same convention ApplyEnrichment/SaveMetadata used
// before it — not "clear it".
type MetadataFields struct {
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

// SaveMetadata unconditionally sets whichever non-blank/non-zero fields are
// present in f into book_enrichment — the manual, human-edited path (the
// Edit Metadata page), so unlike ApplyEnrichment there's no "only if
// currently blank/placeholder" guard: an admin typing a value into the form
// always wins. Title/series/etc. are written into book_enrichment (not
// directly to books) so they survive a later rescan, the same as
// ApplyEnrichment's automatic path. Also marks the book "done".
func (d *DB) SaveMetadata(bookID int64, f MetadataFields) error {
	cur, err := d.currentEnrichmentMerged(bookID)
	if err != nil {
		return err
	}

	final := cur
	if f.Title != "" {
		final.title = f.Title
	}
	if f.Series != "" {
		final.series, final.seriesIndex = f.Series, f.SeriesIndex
	}
	if f.PublishedDate != "" {
		final.publishedDate = f.PublishedDate
	}
	if f.Description != "" {
		final.description = f.Description
	}
	if len(f.Genres) > 0 {
		final.genres = joinCSV(f.Genres)
	}
	if f.Publisher != "" {
		final.publisher = f.Publisher
	}
	if f.Pages != 0 {
		final.pages = int64(f.Pages)
	}
	if f.ISBN != "" {
		final.isbn = f.ISBN
	}
	if f.Rating != 0 {
		final.rating = f.Rating
	}

	return d.upsertEnrichment(bookID, final, "done", SourceManual)
}

// SetCover updates a book's cover image path directly on the books table
// (covers aren't part of book_enrichment — see internal/index/scan.go's
// scan-time cover write, which this mirrors), marking has_cover so the
// existing Cover handler serves it.
func (d *DB) SetCover(bookID int64, coverPath string) error {
	_, err := d.sql.Exec(`UPDATE books SET cover_path = ?, has_cover = 1 WHERE id = ?`, coverPath, bookID)
	return err
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
