package index

import (
	"database/sql"
	"fmt"
	"strings"
)

// SortKey identifies which column a listing should be ordered by.
type SortKey string

const (
	SortTitle    SortKey = "title"
	SortAuthor   SortKey = "author"
	SortAdded    SortKey = "added"
	SortSeries   SortKey = "series"
	SortReleased SortKey = "released"
)

// effectiveSeries/effectiveSeriesIndex/effectivePublishedDate/effectiveTitle/
// effectiveDescription/effectivePublisher are the Hardcover-enrichment-over-
// EPUB-scan merged values: book_enrichment wins when present, falling back
// to the EPUB-scanned books column otherwise. Title is included here (rather
// than written directly to books.title) specifically so an automatic
// Hardcover title overwrite survives a later rescan — a rescan only ever
// rewrites books.* from fresh EPUB parsing, never book_enrichment.
const (
	effectiveSeries        = `COALESCE(be.series, b.series)`
	effectiveSeriesIndex   = `COALESCE(be.series_index, b.series_index)`
	effectivePublishedDate = `COALESCE(be.published_date, b.published_date)`
	effectiveTitle         = `COALESCE(be.title, b.title)`
	effectiveDescription   = `COALESCE(be.description, b.description)`
	effectivePublisher     = `COALESCE(be.publisher, b.publisher)`
)

var sortColumns = map[SortKey]string{
	SortTitle:    "b.sort_title",
	SortAuthor:   "b.sort_author",
	SortAdded:    "b.added_at",
	SortSeries:   effectiveSeries + ", " + effectiveSeriesIndex,
	SortReleased: effectivePublishedDate,
}

const bookColumns = `b.id, b.file_path, b.file_size, b.file_mtime, ` + effectiveTitle + `, b.sort_title, b.author, b.sort_author,
	` + effectiveSeries + `, ` + effectiveSeriesIndex + `, ` + effectiveDescription + `, b.language, ` + effectivePublisher + `, ` + effectivePublishedDate + `, b.identifier,
	b.cover_path, b.has_cover, b.added_at, b.updated_at, b.parse_error,
	be.genres, be.pages, be.isbn, be.rating`

const bookFrom = `books b LEFT JOIN book_enrichment be ON be.book_id = b.id`

// Filter narrows a book listing. Zero value matches every book. Combine
// multiple non-empty fields with AND. This is the extension point for future
// filter kinds (e.g. a user-created shelf/favorite id) without another
// List/Count signature change — add a field here and a clause in
// buildWhereClause.
type Filter struct {
	Search     string // LIKE across title/author/series
	Author     string // exact match
	Series     string // exact match
	ShelfID    int64  // books on this shelf (0 = unset)
	AddedAfter int64  // unix seconds; books with added_at >= this (0 = unset)
}

// List returns a page of books ordered by sort/dir, narrowed by f.
func (d *DB) List(sort SortKey, descending bool, page, pageSize int, f Filter) ([]Book, error) {
	col, ok := sortColumns[sort]
	if !ok {
		col = sortColumns[SortTitle]
	}
	dir := "ASC"
	if descending {
		dir = "DESC"
	}
	if page < 1 {
		page = 1
	}
	if pageSize < 1 {
		pageSize = 48
	}
	offset := (page - 1) * pageSize

	where, args := buildWhereClause(f)
	orderBy := fmt.Sprintf("%s %s", col, dir)
	if sort == SortReleased {
		// A missing release date should always sink to the bottom of the
		// list, whichever direction the visible date column is sorted in.
		orderBy = fmt.Sprintf("(%s IS NULL OR %s = ''), %s", effectivePublishedDate, effectivePublishedDate, orderBy)
	}
	query := fmt.Sprintf(`SELECT %s FROM %s %s ORDER BY %s, b.id ASC LIMIT ? OFFSET ?`, bookColumns, bookFrom, where, orderBy)
	args = append(args, pageSize, offset)

	rows, err := d.sql.Query(query, args...)
	if err != nil {
		return nil, fmt.Errorf("list books: %w", err)
	}
	defer rows.Close()

	return scanBooks(rows)
}

// Count returns how many books match f (Filter{} matches everything).
func (d *DB) Count(f Filter) (int, error) {
	where, args := buildWhereClause(f)
	var n int
	err := d.sql.QueryRow(fmt.Sprintf(`SELECT COUNT(*) FROM %s %s`, bookFrom, where), args...).Scan(&n)
	return n, err
}

func buildWhereClause(f Filter) (string, []any) {
	var conds []string
	var args []any

	if f.Search != "" {
		term := "%" + f.Search + "%"
		conds = append(conds, fmt.Sprintf(`(b.title LIKE ? OR b.author LIKE ? OR %s LIKE ?)`, effectiveSeries))
		args = append(args, term, term, term)
	}
	if f.Author != "" {
		conds = append(conds, `b.author = ?`)
		args = append(args, f.Author)
	}
	if f.Series != "" {
		conds = append(conds, fmt.Sprintf(`%s = ?`, effectiveSeries))
		args = append(args, f.Series)
	}
	if f.ShelfID != 0 {
		conds = append(conds, `EXISTS (SELECT 1 FROM shelf_books sb WHERE sb.book_id = b.id AND sb.shelf_id = ?)`)
		args = append(args, f.ShelfID)
	}
	if f.AddedAfter != 0 {
		conds = append(conds, `b.added_at >= ?`)
		args = append(args, f.AddedAfter)
	}

	if len(conds) == 0 {
		return "", nil
	}
	return "WHERE " + strings.Join(conds, " AND "), args
}

// NameCount is one entry in an author/series browse-index page.
type NameCount struct {
	Name  string
	Count int
}

// ListAuthors returns every distinct author with how many books they have,
// ordered by the same sort_author normalization used for book listings.
func (d *DB) ListAuthors() ([]NameCount, error) {
	rows, err := d.sql.Query(`
		SELECT author, COUNT(*) FROM books
		WHERE author != ''
		GROUP BY author
		ORDER BY MIN(sort_author)`)
	if err != nil {
		return nil, fmt.Errorf("list authors: %w", err)
	}
	defer rows.Close()
	return scanNameCounts(rows)
}

// ListSeries returns every distinct series with how many books it has,
// ordered alphabetically.
func (d *DB) ListSeries() ([]NameCount, error) {
	rows, err := d.sql.Query(fmt.Sprintf(`
		SELECT %s AS series_name, COUNT(*) FROM %s
		WHERE %s IS NOT NULL AND %s != ''
		GROUP BY %s
		ORDER BY series_name`, effectiveSeries, bookFrom, effectiveSeries, effectiveSeries, effectiveSeries))
	if err != nil {
		return nil, fmt.Errorf("list series: %w", err)
	}
	defer rows.Close()
	return scanNameCounts(rows)
}

func scanNameCounts(rows *sql.Rows) ([]NameCount, error) {
	var out []NameCount
	for rows.Next() {
		var nc NameCount
		if err := rows.Scan(&nc.Name, &nc.Count); err != nil {
			return nil, err
		}
		out = append(out, nc)
	}
	return out, rows.Err()
}

// Get returns a single book by id.
func (d *DB) Get(id int64) (*Book, error) {
	query := fmt.Sprintf(`SELECT %s FROM %s WHERE b.id = ?`, bookColumns, bookFrom)
	row := d.sql.QueryRow(query, id)
	b, err := scanBook(row)
	if err != nil {
		if err == sql.ErrNoRows {
			return nil, nil
		}
		return nil, err
	}
	return b, nil
}

type rowScanner interface {
	Scan(dest ...any) error
}

func scanBook(row rowScanner) (*Book, error) {
	var b Book
	var series, description, language, publisher, publishedAt, identifier, coverPath, parseError sql.NullString
	var seriesIndex sql.NullFloat64
	var genres, isbn sql.NullString
	var pages sql.NullInt64
	var rating sql.NullFloat64
	var hasCover int

	err := row.Scan(
		&b.ID, &b.FilePath, &b.FileSize, &b.FileMtime, &b.Title, &b.SortTitle, &b.Author, &b.SortAuthor,
		&series, &seriesIndex, &description, &language, &publisher, &publishedAt, &identifier,
		&coverPath, &hasCover, &b.AddedAt, &b.UpdatedAt, &parseError,
		&genres, &pages, &isbn, &rating,
	)
	if err != nil {
		return nil, err
	}

	b.Series = series.String
	b.SeriesIndex = seriesIndex.Float64
	b.Description = description.String
	b.Language = language.String
	b.Publisher = publisher.String
	b.PublishedAt = publishedAt.String
	b.Identifier = identifier.String
	b.CoverPath = coverPath.String
	b.HasCover = hasCover != 0
	b.ParseError = parseError.String
	b.Genres = splitCSV(genres.String)
	b.Pages = int(pages.Int64)
	b.ISBN = isbn.String
	b.Rating = rating.Float64

	return &b, nil
}

func scanBooks(rows *sql.Rows) ([]Book, error) {
	var books []Book
	for rows.Next() {
		b, err := scanBook(rows)
		if err != nil {
			return nil, err
		}
		books = append(books, *b)
	}
	return books, rows.Err()
}
