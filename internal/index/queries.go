package index

import (
	"database/sql"
	"fmt"
)

// SortKey identifies which column a listing should be ordered by.
type SortKey string

const (
	SortTitle  SortKey = "title"
	SortAuthor SortKey = "author"
	SortAdded  SortKey = "added"
	SortSeries SortKey = "series"
)

var sortColumns = map[SortKey]string{
	SortTitle:  "sort_title",
	SortAuthor: "sort_author",
	SortAdded:  "added_at",
	SortSeries: "series, series_index",
}

const bookColumns = `id, file_path, file_size, file_mtime, title, sort_title, author, sort_author,
	series, series_index, description, language, publisher, published_date, identifier,
	cover_path, has_cover, added_at, updated_at, parse_error`

// List returns a page of books ordered by sort/dir.
func (d *DB) List(sort SortKey, descending bool, page, pageSize int) ([]Book, error) {
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

	query := fmt.Sprintf(`SELECT %s FROM books ORDER BY %s %s, id ASC LIMIT ? OFFSET ?`, bookColumns, col, dir)
	rows, err := d.sql.Query(query, pageSize, offset)
	if err != nil {
		return nil, fmt.Errorf("list books: %w", err)
	}
	defer rows.Close()

	return scanBooks(rows)
}

// Count returns the total number of indexed books.
func (d *DB) Count() (int, error) {
	var n int
	err := d.sql.QueryRow(`SELECT COUNT(*) FROM books`).Scan(&n)
	return n, err
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
