package index

import (
	"archive/zip"
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
	f, err := os.Create(path)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	zw := zip.NewWriter(f)
	for name, content := range map[string]string{
		"META-INF/container.xml": testContainerXML,
		"content.opf":            testOPF(title, author),
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

	n, err := db.Count()
	if err != nil {
		t.Fatal(err)
	}
	if n != 2 {
		t.Fatalf("Count = %d, want 2", n)
	}

	books, err := db.List(SortTitle, false, 1, 10)
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
	if n, _ := db.Count(); n != 1 {
		t.Fatalf("Count = %d, want 1", n)
	}

	if err := os.Remove(bookPath); err != nil {
		t.Fatal(err)
	}
	if err := db.Scan(libDir, nil); err != nil {
		t.Fatal(err)
	}
	if n, _ := db.Count(); n != 0 {
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
	before, err := db.List(SortTitle, false, 1, 10)
	if err != nil {
		t.Fatal(err)
	}

	if err := db.Scan(libDir, nil); err != nil {
		t.Fatal(err)
	}
	after, err := db.List(SortTitle, false, 1, 10)
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

	books, err := db.List(SortTitle, false, 1, 10)
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
