package index

import (
	"database/sql"
	"fmt"
	"sort"
	"strings"

	"github.com/swvn/eink-library/internal/epub"
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

const bookColumns = `b.id, b.library_root, b.file_path, b.file_size, b.file_mtime, ` + effectiveTitle + `, b.sort_title, b.author, b.sort_author,
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
	Search               string // LIKE across title/author/series
	Author               string // exact match, or one " & "-delimited component of a multi-author byline — see buildWhereClause
	Series               string // exact match
	ShelfID              int64  // books on this shelf (0 = unset)
	AddedAfter           int64  // unix seconds; books with added_at >= this (0 = unset)
	HideNoChaptarrMatch  bool   // exclude books with a confirmed chaptarr_status = 'no_match'
	HideNoHardcoverMatch bool   // exclude books with a confirmed hardcover_status = 'no_match'
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
		// A book's author column can hold multiple people joined by " & "
		// (see epub.CleanAuthorNames, which guarantees that joiner for
		// anything scanned after this feature shipped) — match f.Author as
		// either the whole column or one of its " & "-delimited components,
		// not just an exact whole-string match, so following a link for one
		// co-author on a multi-author book finds it. Legacy books indexed
		// before this feature (a different joiner, or un-normalized name
		// order) need a "Force full reimport" to pick this up, same as any
		// other EPUB-parsing fix in this codebase.
		esc := escapeLike(f.Author)
		conds = append(conds, `(b.author = ? OR b.author LIKE ? ESCAPE '\' OR b.author LIKE ? ESCAPE '\' OR b.author LIKE ? ESCAPE '\')`)
		args = append(args, f.Author, esc+" & %", "% & "+esc, "% & "+esc+" & %")
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
	conds = append(conds, hideMatchConds(f)...)

	if len(conds) == 0 {
		return "", nil
	}
	return "WHERE " + strings.Join(conds, " AND "), args
}

// hideMatchConds returns the WHERE fragments (no placeholders needed) for
// the admin "hide no-Chaptarr-match"/"hide no-Hardcover-match" settings —
// shared by buildWhereClause and the ListAuthors/ListSeries browse-index
// queries, both of which join book_enrichment as `be` (see bookFrom).
func hideMatchConds(f Filter) []string {
	var conds []string
	if f.HideNoChaptarrMatch {
		conds = append(conds, `COALESCE(be.chaptarr_status, '') != 'no_match'`)
	}
	if f.HideNoHardcoverMatch {
		conds = append(conds, `COALESCE(be.hardcover_status, '') != 'no_match'`)
	}
	return conds
}

// NameCount is one entry in an author/series browse-index page.
type NameCount struct {
	Name  string
	Count int
}

// ListAuthors returns every individual author with how many books they
// have, ordered by the same last-name-first normalization used for book
// listings. A book with multiple authors (see epub.CleanAuthorNames, which
// guarantees they're " & "-joined in the stored author column) contributes
// to every one of its authors' counts here, rather than the whole
// multi-person byline showing up as one combined browse-index entry — e.g.
// "P.C. Cast & Kristin Cast" becomes two separate rows, "P.C. Cast" and
// "Kristin Cast", each counting that book. Splitting happens in Go, not
// SQL (SQLite has no portable string-split), by re-running each grouped
// raw byline through epub.CleanAuthorNames — which also means this always
// shows normalized ("Last, First" → "First Last") individual names even
// for a book indexed before this feature shipped, without requiring a
// reimport (reimporting is still needed for the *stored* column itself,
// and thus for Filter.Author's component matching, to be consistent).
func (d *DB) ListAuthors(f Filter) ([]NameCount, error) {
	conds := append([]string{`b.author != ''`}, hideMatchConds(f)...)
	rows, err := d.sql.Query(fmt.Sprintf(`
		SELECT b.author, COUNT(*) FROM %s
		WHERE %s
		GROUP BY b.author`, bookFrom, strings.Join(conds, " AND ")))
	if err != nil {
		return nil, fmt.Errorf("list authors: %w", err)
	}
	defer rows.Close()

	counts := make(map[string]int)     // keyed by lowercase normalized name
	display := make(map[string]string) // lowercase -> first-seen display casing
	for rows.Next() {
		var rawAuthor string
		var n int
		if err := rows.Scan(&rawAuthor, &n); err != nil {
			return nil, err
		}
		for _, name := range epub.CleanAuthorNames([]string{rawAuthor}) {
			key := strings.ToLower(name)
			counts[key] += n
			if _, ok := display[key]; !ok {
				display[key] = name
			}
		}
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}

	out := make([]NameCount, 0, len(counts))
	for key, n := range counts {
		out = append(out, NameCount{Name: display[key], Count: n})
	}
	sort.Slice(out, func(i, j int) bool {
		return epub.SortAuthorName(out[i].Name) < epub.SortAuthorName(out[j].Name)
	})
	return out, nil
}

// escapeLike escapes SQLite LIKE metacharacters (%, _, and the escape
// character itself) in s, for use with `LIKE ? ESCAPE '\'` — needed when a
// value being matched (an author name) might itself contain a literal % or
// _ that shouldn't be treated as a wildcard.
func escapeLike(s string) string {
	s = strings.ReplaceAll(s, `\`, `\\`)
	s = strings.ReplaceAll(s, `%`, `\%`)
	s = strings.ReplaceAll(s, `_`, `\_`)
	return s
}

// ListSeries returns every distinct series with how many books it has,
// ordered alphabetically.
func (d *DB) ListSeries(f Filter) ([]NameCount, error) {
	conds := append([]string{
		fmt.Sprintf(`%s IS NOT NULL`, effectiveSeries),
		fmt.Sprintf(`%s != ''`, effectiveSeries),
	}, hideMatchConds(f)...)
	rows, err := d.sql.Query(fmt.Sprintf(`
		SELECT %s AS series_name, COUNT(*) FROM %s
		WHERE %s
		GROUP BY %s
		ORDER BY series_name`, effectiveSeries, bookFrom, strings.Join(conds, " AND "), effectiveSeries))
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

// Location is one known copy of a book on disk: its library root and the
// root-relative file path.
type Location struct {
	LibraryRoot string
	FilePath    string
}

// LocationsForBooks returns every extra known copy (beyond each book's own
// canonical library_root/file_path) for the given book IDs, batched into a
// single query -- same batching style as ShelfBookIDs, used to build a
// per-book map without an N+1 query per card.
func (d *DB) LocationsForBooks(ids []int64) (map[int64][]Location, error) {
	out := make(map[int64][]Location)
	if len(ids) == 0 {
		return out, nil
	}

	placeholders := strings.TrimRight(strings.Repeat("?,", len(ids)), ",")
	args := make([]any, len(ids))
	for i, id := range ids {
		args[i] = id
	}

	rows, err := d.sql.Query(fmt.Sprintf(`
		SELECT book_id, library_root, file_path FROM book_locations
		WHERE book_id IN (%s)`, placeholders), args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	for rows.Next() {
		var bookID int64
		var loc Location
		if err := rows.Scan(&bookID, &loc.LibraryRoot, &loc.FilePath); err != nil {
			return nil, err
		}
		out[bookID] = append(out[bookID], loc)
	}
	return out, rows.Err()
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
		&b.ID, &b.LibraryRoot, &b.FilePath, &b.FileSize, &b.FileMtime, &b.Title, &b.SortTitle, &b.Author, &b.SortAuthor,
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
