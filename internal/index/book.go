package index

// Book is a single indexed row from the books table.
type Book struct {
	ID          int64
	FilePath    string
	FileSize    int64
	FileMtime   int64
	Title       string
	SortTitle   string
	Author      string
	SortAuthor  string
	Series      string
	SeriesIndex float64
	Description string
	Language    string
	Publisher   string
	PublishedAt string
	Identifier  string
	CoverPath   string
	HasCover    bool
	AddedAt     int64
	UpdatedAt   int64
	ParseError  string

	// Hardcover-only fields: no EPUB-scanned equivalent, so these come
	// straight from book_enrichment with no COALESCE fallback.
	Genres []string
	Pages  int
	ISBN   string
	Rating float64
}
