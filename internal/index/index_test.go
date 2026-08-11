package index

import (
	"archive/zip"
	"fmt"
	"os"
	"path/filepath"
	"testing"
)

const testContainerXML = `<?xml version="1.0"?>
<container xmlns="urn:oasis:names:tc:opendocument:xmlns:container" version="1.0">
  <rootfiles><rootfile full-path="content.opf" media-type="application/oebps-package+xml"/></rootfiles>
</container>`

func testOPF(title, author string) string {
	return `<?xml version="1.0" encoding="UTF-8"?>
<package xmlns="http://www.idpf.org/2007/opf" version="3.0">
  <metadata xmlns:dc="http://purl.org/dc/elements/1.1/">
    <dc:title>` + title + `</dc:title>
    <dc:creator>` + author + `</dc:creator>
  </metadata>
  <manifest><item id="c1" href="c1.xhtml" media-type="application/xhtml+xml"/></manifest>
  <spine><itemref idref="c1"/></spine>
</package>`
}

func writeTestEpub(t *testing.T, path, title, author string) {
	t.Helper()
	writeTestEpubOPF(t, path, testOPF(title, author))
}

func testOPFWithSeries(title, author, series string, seriesIndex int) string {
	return `<?xml version="1.0" encoding="UTF-8"?>
<package xmlns="http://www.idpf.org/2007/opf" version="3.0">
  <metadata xmlns:dc="http://purl.org/dc/elements/1.1/">
    <dc:title>` + title + `</dc:title>
    <dc:creator>` + author + `</dc:creator>
    <meta name="calibre:series" content="` + series + `"/>
    <meta name="calibre:series_index" content="` + fmt.Sprint(seriesIndex) + `"/>
  </metadata>
  <manifest><item id="c1" href="c1.xhtml" media-type="application/xhtml+xml"/></manifest>
  <spine><itemref idref="c1"/></spine>
</package>`
}

func writeTestEpubWithSeries(t *testing.T, path, title, author, series string, seriesIndex int) {
	t.Helper()
	writeTestEpubOPF(t, path, testOPFWithSeries(title, author, series, seriesIndex))
}

func testOPFWithDate(title, author, date string) string {
	dateTag := ""
	if date != "" {
		dateTag = `<dc:date>` + date + `</dc:date>`
	}
	return `<?xml version="1.0" encoding="UTF-8"?>
<package xmlns="http://www.idpf.org/2007/opf" version="3.0">
  <metadata xmlns:dc="http://purl.org/dc/elements/1.1/">
    <dc:title>` + title + `</dc:title>
    <dc:creator>` + author + `</dc:creator>
    ` + dateTag + `
  </metadata>
  <manifest><item id="c1" href="c1.xhtml" media-type="application/xhtml+xml"/></manifest>
  <spine><itemref idref="c1"/></spine>
</package>`
}

func writeTestEpubWithDate(t *testing.T, path, title, author, date string) {
	t.Helper()
	writeTestEpubOPF(t, path, testOPFWithDate(title, author, date))
}

func writeTestEpubOPF(t *testing.T, path, opf string) {
	t.Helper()
	f, err := os.Create(path)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	zw := zip.NewWriter(f)
	for name, content := range map[string]string{
		"META-INF/container.xml": testContainerXML,
		"content.opf":            opf,
		"c1.xhtml":               "<html><body>hi</body></html>",
	} {
		w, err := zw.Create(name)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := w.Write([]byte(content)); err != nil {
			t.Fatal(err)
		}
	}
	if err := zw.Close(); err != nil {
		t.Fatal(err)
	}
}

func openTestDB(t *testing.T) *DB {
	t.Helper()
	dbPath := filepath.Join(t.TempDir(), "index.db")
	db, err := Open(dbPath)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	return db
}

func TestScan_IndexesBooks(t *testing.T) {
	libDir := t.TempDir()
	writeTestEpub(t, filepath.Join(libDir, "book1.epub"), "Book One", "Author A")
	writeTestEpub(t, filepath.Join(libDir, "book2.epub"), "The Book Two", "Author B")

	db := openTestDB(t)
	if err := db.Scan(libDir, nil); err != nil {
		t.Fatalf("Scan: %v", err)
	}

	n, err := db.Count(Filter{})
	if err != nil {
		t.Fatal(err)
	}
	if n != 2 {
		t.Fatalf("Count = %d, want 2", n)
	}

	books, err := db.List(SortTitle, false, 1, 10, Filter{})
	if err != nil {
		t.Fatal(err)
	}
	if len(books) != 2 {
		t.Fatalf("List returned %d books, want 2", len(books))
	}
	if books[0].Title != "Book One" {
		t.Errorf("first book by sort_title = %q, want %q", books[0].Title, "Book One")
	}
}

func TestScan_PrunesDeletedFiles(t *testing.T) {
	libDir := t.TempDir()
	bookPath := filepath.Join(libDir, "book1.epub")
	writeTestEpub(t, bookPath, "Book One", "Author A")

	db := openTestDB(t)
	if err := db.Scan(libDir, nil); err != nil {
		t.Fatal(err)
	}
	if n, _ := db.Count(Filter{}); n != 1 {
		t.Fatalf("Count = %d, want 1", n)
	}

	if err := os.Remove(bookPath); err != nil {
		t.Fatal(err)
	}
	if err := db.Scan(libDir, nil); err != nil {
		t.Fatal(err)
	}
	if n, _ := db.Count(Filter{}); n != 0 {
		t.Fatalf("Count after prune = %d, want 0", n)
	}
}

func TestScan_SkipsUnchangedFiles(t *testing.T) {
	libDir := t.TempDir()
	writeTestEpub(t, filepath.Join(libDir, "book1.epub"), "Book One", "Author A")

	db := openTestDB(t)
	if err := db.Scan(libDir, nil); err != nil {
		t.Fatal(err)
	}
	before, err := db.List(SortTitle, false, 1, 10, Filter{})
	if err != nil {
		t.Fatal(err)
	}

	if err := db.Scan(libDir, nil); err != nil {
		t.Fatal(err)
	}
	after, err := db.List(SortTitle, false, 1, 10, Filter{})
	if err != nil {
		t.Fatal(err)
	}

	if before[0].ID != after[0].ID || before[0].UpdatedAt != after[0].UpdatedAt {
		t.Errorf("unchanged file was re-upserted: before=%+v after=%+v", before[0], after[0])
	}
}

func TestScan_MalformedEpubStillIndexed(t *testing.T) {
	libDir := t.TempDir()
	if err := os.WriteFile(filepath.Join(libDir, "bad.epub"), []byte("not a zip"), 0o644); err != nil {
		t.Fatal(err)
	}

	db := openTestDB(t)
	if err := db.Scan(libDir, nil); err != nil {
		t.Fatal(err)
	}

	books, err := db.List(SortTitle, false, 1, 10, Filter{})
	if err != nil {
		t.Fatal(err)
	}
	if len(books) != 1 {
		t.Fatalf("expected 1 book (fallback row), got %d", len(books))
	}
	if books[0].ParseError == "" {
		t.Error("expected parse_error to be set for malformed epub")
	}
	if books[0].Title != "bad.epub" {
		t.Errorf("Title = %q, want fallback filename %q", books[0].Title, "bad.epub")
	}
}

func TestSearch_MatchesTitleAuthorSeries(t *testing.T) {
	libDir := t.TempDir()
	writeTestEpub(t, filepath.Join(libDir, "b1.epub"), "Zebra Tales", "Amy Zed")
	writeTestEpub(t, filepath.Join(libDir, "b2.epub"), "Banana Republic", "Bob Young")

	db := openTestDB(t)
	if err := db.Scan(libDir, nil); err != nil {
		t.Fatal(err)
	}

	byTitle, err := db.List(SortTitle, false, 1, 10, Filter{Search: "zebra"})
	if err != nil {
		t.Fatal(err)
	}
	if len(byTitle) != 1 || byTitle[0].Title != "Zebra Tales" {
		t.Errorf("title search: got %+v, want 1 match on Zebra Tales", byTitle)
	}

	byAuthor, err := db.List(SortTitle, false, 1, 10, Filter{Search: "young"})
	if err != nil {
		t.Fatal(err)
	}
	if len(byAuthor) != 1 || byAuthor[0].Author != "Bob Young" {
		t.Errorf("author search: got %+v, want 1 match on Bob Young", byAuthor)
	}

	noMatch, err := db.List(SortTitle, false, 1, 10, Filter{Search: "nonexistent"})
	if err != nil {
		t.Fatal(err)
	}
	if len(noMatch) != 0 {
		t.Errorf("expected no matches, got %d", len(noMatch))
	}

	count, err := db.Count(Filter{Search: "zebra"})
	if err != nil {
		t.Fatal(err)
	}
	if count != 1 {
		t.Errorf("CountSearch = %d, want 1", count)
	}
}

func TestFilter_ExactAuthorAndSeries(t *testing.T) {
	libDir := t.TempDir()
	writeTestEpub(t, filepath.Join(libDir, "b1.epub"), "Zebra Tales", "Amy Zed")
	writeTestEpub(t, filepath.Join(libDir, "b2.epub"), "Banana Republic", "Bob Young")

	db := openTestDB(t)
	if err := db.Scan(libDir, nil); err != nil {
		t.Fatal(err)
	}

	byAuthor, err := db.List(SortTitle, false, 1, 10, Filter{Author: "Amy Zed"})
	if err != nil {
		t.Fatal(err)
	}
	if len(byAuthor) != 1 || byAuthor[0].Title != "Zebra Tales" {
		t.Errorf("author filter: got %+v, want 1 match on Zebra Tales", byAuthor)
	}

	// "Zed" alone shouldn't match under an exact filter (unlike LIKE-based search).
	noMatch, err := db.List(SortTitle, false, 1, 10, Filter{Author: "Zed"})
	if err != nil {
		t.Fatal(err)
	}
	if len(noMatch) != 0 {
		t.Errorf("expected exact-match filter to reject partial name, got %d results", len(noMatch))
	}
}

func TestList_ReleasedSortNullsLast(t *testing.T) {
	libDir := t.TempDir()
	writeTestEpubWithDate(t, filepath.Join(libDir, "b1.epub"), "Older Book", "Amy Zed", "2000-01-01")
	writeTestEpubWithDate(t, filepath.Join(libDir, "b2.epub"), "Newer Book", "Amy Zed", "2020-01-01")
	writeTestEpubWithDate(t, filepath.Join(libDir, "b3.epub"), "Undated Book", "Amy Zed", "")

	db := openTestDB(t)
	if err := db.Scan(libDir, nil); err != nil {
		t.Fatal(err)
	}

	ascending, err := db.List(SortReleased, false, 1, 10, Filter{})
	if err != nil {
		t.Fatal(err)
	}
	if len(ascending) != 3 || ascending[len(ascending)-1].Title != "Undated Book" {
		t.Fatalf("ascending release-date order: got %+v, want Undated Book last", titles(ascending))
	}
	if ascending[0].Title != "Older Book" || ascending[1].Title != "Newer Book" {
		t.Errorf("ascending release-date order among dated books: got %+v, want Older Book then Newer Book", titles(ascending))
	}

	descending, err := db.List(SortReleased, true, 1, 10, Filter{})
	if err != nil {
		t.Fatal(err)
	}
	if len(descending) != 3 || descending[len(descending)-1].Title != "Undated Book" {
		t.Fatalf("descending release-date order: got %+v, want Undated Book last", titles(descending))
	}
	if descending[0].Title != "Newer Book" || descending[1].Title != "Older Book" {
		t.Errorf("descending release-date order among dated books: got %+v, want Newer Book then Older Book", titles(descending))
	}
}

func titles(books []Book) []string {
	out := make([]string, len(books))
	for i, b := range books {
		out[i] = b.Title
	}
	return out
}

func TestListAuthorsAndSeries(t *testing.T) {
	libDir := t.TempDir()
	writeTestEpubWithSeries(t, filepath.Join(libDir, "b1.epub"), "Zebra Tales", "Amy Zed", "", 0)
	writeTestEpubWithSeries(t, filepath.Join(libDir, "b2.epub"), "The First Light", "Bob Young", "Light Saga", 1)
	writeTestEpubWithSeries(t, filepath.Join(libDir, "b3.epub"), "The Second Dawn", "Bob Young", "Light Saga", 2)

	db := openTestDB(t)
	if err := db.Scan(libDir, nil); err != nil {
		t.Fatal(err)
	}

	authors, err := db.ListAuthors()
	if err != nil {
		t.Fatal(err)
	}
	if len(authors) != 2 {
		t.Fatalf("ListAuthors: got %d authors, want 2", len(authors))
	}
	byName := map[string]int{}
	for _, a := range authors {
		byName[a.Name] = a.Count
	}
	if byName["Amy Zed"] != 1 || byName["Bob Young"] != 2 {
		t.Errorf("ListAuthors counts = %+v, want Amy Zed:1 Bob Young:2", byName)
	}
}

func TestListAuthors_SplitsMultiAuthorBooksIntoIndividualEntries(t *testing.T) {
	libDir := t.TempDir()
	writeTestEpub(t, filepath.Join(libDir, "b1.epub"), "Assistant to the Villain", "P.C. Cast &amp; Kristin Cast")
	writeTestEpub(t, filepath.Join(libDir, "b2.epub"), "Solo Book", "P.C. Cast")

	db := openTestDB(t)
	if err := db.Scan(libDir, nil); err != nil {
		t.Fatal(err)
	}

	authors, err := db.ListAuthors()
	if err != nil {
		t.Fatal(err)
	}
	byName := map[string]int{}
	for _, a := range authors {
		byName[a.Name] = a.Count
	}
	// "P.C. Cast & Kristin Cast" must not appear as its own combined entry —
	// only the two individual names, each counting the shared book, plus
	// P.C. Cast's second solo book.
	if _, ok := byName["P.C. Cast & Kristin Cast"]; ok {
		t.Errorf("ListAuthors = %+v, did not expect the combined byline as its own entry", byName)
	}
	if byName["P.C. Cast"] != 2 {
		t.Errorf("byName[P.C. Cast] = %d, want 2 (the co-authored book plus the solo book)", byName["P.C. Cast"])
	}
	if byName["Kristin Cast"] != 1 {
		t.Errorf("byName[Kristin Cast] = %d, want 1", byName["Kristin Cast"])
	}
}

func TestFilter_Author_MatchesIndividualCoAuthor(t *testing.T) {
	libDir := t.TempDir()
	writeTestEpub(t, filepath.Join(libDir, "b1.epub"), "Assistant to the Villain", "P.C. Cast &amp; Kristin Cast")
	writeTestEpub(t, filepath.Join(libDir, "b2.epub"), "Unrelated Book", "Amy Zed")

	db := openTestDB(t)
	if err := db.Scan(libDir, nil); err != nil {
		t.Fatal(err)
	}

	got, err := db.List(SortTitle, false, 1, 10, Filter{Author: "Kristin Cast"})
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 || got[0].Title != "Assistant to the Villain" {
		t.Errorf("Filter{Author: \"Kristin Cast\"} = %+v, want the one co-authored book", titles(got))
	}

	got, err = db.List(SortTitle, false, 1, 10, Filter{Author: "P.C. Cast"})
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 || got[0].Title != "Assistant to the Villain" {
		t.Errorf("Filter{Author: \"P.C. Cast\"} = %+v, want the one co-authored book", titles(got))
	}
}
