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

var sortColumns = map[SortKey]string{
	SortTitle:    "sort_title",
	SortAuthor:   "sort_author",
	SortAdded:    "added_at",
	SortSeries:   "series, series_index",
	SortReleased: "published_date",
}

const bookColumns = `id, file_path, file_size, file_mtime, title, sort_title, author, sort_author,
	series, series_index, description, language, publisher, published_date, identifier,
	cover_path, has_cover, added_at, updated_at, parse_error`

// Filter narrows a book listing. Zero value matches every book. Combine
// multiple non-empty fields with AND. This is the extension point for future
// filter kinds (e.g. a user-created shelf/favorite id) without another
// List/Count signature change — add a field here and a clause in
// buildWhereClause.
type Filter struct {
	Search string // LIKE across title/author/series
	Author string // exact match
	Series string // exact match
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
	query := fmt.Sprintf(`SELECT %s FROM books %s ORDER BY %s %s, id ASC LIMIT ? OFFSET ?`, bookColumns, where, col, dir)
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
	err := d.sql.QueryRow(fmt.Sprintf(`SELECT COUNT(*) FROM books %s`, where), args...).Scan(&n)
	return n, err
}

func buildWhereClause(f Filter) (string, []any) {
	var conds []string
	var args []any

	if f.Search != "" {
		term := "%" + f.Search + "%"
		conds = append(conds, `(title LIKE ? OR author LIKE ? OR series LIKE ?)`)
		args = append(args, term, term, term)
	}
	if f.Author != "" {
		conds = append(conds, `author = ?`)
		args = append(args, f.Author)
	}
	if f.Series != "" {
		conds = append(conds, `series = ?`)
		args = append(args, f.Series)
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
	rows, err := d.sql.Query(`
		SELECT series, COUNT(*) FROM books
		WHERE series IS NOT NULL AND series != ''
		GROUP BY series
		ORDER BY series`)
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
	query := fmt.Sprintf(`SELECT %s FROM books WHERE id = ?`, bookColumns)
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
	var hasCover int

	err := row.Scan(
		&b.ID, &b.FilePath, &b.FileSize, &b.FileMtime, &b.Title, &b.SortTitle, &b.Author, &b.SortAuthor,
		&series, &seriesIndex, &description, &language, &publisher, &publishedAt, &identifier,
		&coverPath, &hasCover, &b.AddedAt, &b.UpdatedAt, &parseError,
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
