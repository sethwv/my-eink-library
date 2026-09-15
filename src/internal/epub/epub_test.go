package epub

import (
	"archive/zip"
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

const sampleContainerXML = `<?xml version="1.0"?>
<container xmlns="urn:oasis:names:tc:opendocument:xmlns:container" version="1.0">
  <rootfiles>
    <rootfile full-path="OEBPS/content.opf" media-type="application/oebps-package+xml"/>
  </rootfiles>
</container>`

const opfEPUB3 = `<?xml version="1.0" encoding="UTF-8"?>
<package xmlns="http://www.idpf.org/2007/opf" version="3.0" unique-identifier="bookid">
  <metadata xmlns:dc="http://purl.org/dc/elements/1.1/">
    <dc:title>The Great Test</dc:title>
    <dc:creator>Jane Doe</dc:creator>
    <dc:language>en</dc:language>
    <dc:publisher>Test Press</dc:publisher>
    <dc:date>2020-01-01</dc:date>
    <dc:identifier id="bookid">urn:uuid:1234</dc:identifier>
    <meta name="calibre:series" content="Test Series"/>
    <meta name="calibre:series_index" content="2"/>
  </metadata>
  <manifest>
    <item id="cover-img" href="cover.jpg" media-type="image/jpeg" properties="cover-image"/>
    <item id="chap1" href="chap1.xhtml" media-type="application/xhtml+xml"/>
  </manifest>
  <spine><itemref idref="chap1"/></spine>
</package>`

const opfEPUB2 = `<?xml version="1.0" encoding="UTF-8"?>
<package xmlns="http://www.idpf.org/2007/opf" version="2.0" unique-identifier="bookid">
  <metadata xmlns:dc="http://purl.org/dc/elements/1.1/" xmlns:opf="http://www.idpf.org/2007/opf">
    <dc:title>An Older Book</dc:title>
    <dc:creator opf:role="aut">John Smith</dc:creator>
    <meta name="cover" content="cover-img"/>
  </metadata>
  <manifest>
    <item id="cover-img" href="images/cover.png" media-type="image/png"/>
    <item id="chap1" href="chap1.xhtml" media-type="application/xhtml+xml"/>
  </manifest>
  <spine><itemref idref="chap1"/></spine>
</package>`

func buildEpub(t *testing.T, opf string, includeCover bool) string {
	t.Helper()
	dir := t.TempDir()
	epubPath := filepath.Join(dir, "book.epub")

	f, err := os.Create(epubPath)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()

	zw := zip.NewWriter(f)
	writeFile := func(name string, content []byte) {
		w, err := zw.Create(name)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := w.Write(content); err != nil {
			t.Fatal(err)
		}
	}

	writeFile("mimetype", []byte("application/epub+zip"))
	writeFile("META-INF/container.xml", []byte(sampleContainerXML))
	writeFile("OEBPS/content.opf", []byte(opf))
	writeFile("OEBPS/chap1.xhtml", []byte("<html><body>hi</body></html>"))
	if includeCover {
		// Not a real JPEG/PNG, just enough bytes to prove extraction works end to end.
		writeFile("OEBPS/cover.jpg", []byte("fake-jpeg-bytes"))
		writeFile("OEBPS/images/cover.png", []byte("fake-png-bytes"))
	}

	if err := zw.Close(); err != nil {
		t.Fatal(err)
	}
	return epubPath
}

func TestParseFile_EPUB3(t *testing.T) {
	path := buildEpub(t, opfEPUB3, true)

	m, err := ParseFile(path)
	if err != nil {
		t.Fatalf("ParseFile: %v", err)
	}

	if m.Title != "The Great Test" {
		t.Errorf("Title = %q, want %q", m.Title, "The Great Test")
	}
	if m.SortTitle != "great test" {
		t.Errorf("SortTitle = %q, want %q", m.SortTitle, "great test")
	}
	if m.Author != "Jane Doe" {
		t.Errorf("Author = %q, want %q", m.Author, "Jane Doe")
	}
	if m.SortAuthor != "doe, jane" {
		t.Errorf("SortAuthor = %q, want %q", m.SortAuthor, "doe, jane")
	}
	if m.Series != "Test Series" {
		t.Errorf("Series = %q, want %q", m.Series, "Test Series")
	}
	if m.SeriesIndex != 2 {
		t.Errorf("SeriesIndex = %v, want 2", m.SeriesIndex)
	}
	if !bytes.Equal(m.CoverData, []byte("fake-jpeg-bytes")) {
		t.Errorf("CoverData = %q, want fake-jpeg-bytes", m.CoverData)
	}
	if m.CoverMediaType != "image/jpeg" {
		t.Errorf("CoverMediaType = %q, want image/jpeg", m.CoverMediaType)
	}
}

func TestParseFile_EPUB2(t *testing.T) {
	path := buildEpub(t, opfEPUB2, true)

	m, err := ParseFile(path)
	if err != nil {
		t.Fatalf("ParseFile: %v", err)
	}

	if m.Title != "An Older Book" {
		t.Errorf("Title = %q, want %q", m.Title, "An Older Book")
	}
	if m.SortTitle != "older book" {
		t.Errorf("SortTitle = %q, want %q", m.SortTitle, "older book")
	}
	if m.SortAuthor != "smith, john" {
		t.Errorf("SortAuthor = %q, want %q", m.SortAuthor, "smith, john")
	}
	if !bytes.Equal(m.CoverData, []byte("fake-png-bytes")) {
		t.Errorf("CoverData = %q, want fake-png-bytes", m.CoverData)
	}
	if m.CoverMediaType != "image/png" {
		t.Errorf("CoverMediaType = %q, want image/png", m.CoverMediaType)
	}
}

func TestParseFile_NoCover(t *testing.T) {
	path := buildEpub(t, opfEPUB3, false)

	m, err := ParseFile(path)
	if err != nil {
		t.Fatalf("ParseFile: %v", err)
	}
	if m.CoverData != nil {
		t.Errorf("expected no cover data, got %d bytes", len(m.CoverData))
	}
}

// TestParseFile_XMLVersion1_1 covers EPUBs from tools that emit "<?xml
// version="1.1"?>" for otherwise-1.0-compatible OPF/container documents.
// Go's encoding/xml rejects any declared version other than 1.0 outright,
// so without normalizeXMLVersion this would fail with "xml: unsupported
// version \"1.1\"" instead of parsing normally.
func TestParseFile_XMLVersion1_1(t *testing.T) {
	opf := strings.Replace(opfEPUB3, `<?xml version="1.0" encoding="UTF-8"?>`, `<?xml version="1.1" encoding="UTF-8"?>`, 1)
	path := buildEpub(t, opf, true)

	m, err := ParseFile(path)
	if err != nil {
		t.Fatalf("ParseFile: %v", err)
	}
	if m.Title != "The Great Test" {
		t.Errorf("Title = %q, want %q", m.Title, "The Great Test")
	}
}

func TestParseFile_Malformed(t *testing.T) {
	dir := t.TempDir()
	badPath := filepath.Join(dir, "not-an-epub.epub")
	if err := os.WriteFile(badPath, []byte("this is not a zip file at all"), 0o644); err != nil {
		t.Fatal(err)
	}

	if _, err := ParseFile(badPath); err == nil {
		t.Error("expected error parsing malformed file, got nil")
	}
}

func TestParseFile_MissingOPF(t *testing.T) {
	dir := t.TempDir()
	epubPath := filepath.Join(dir, "no-opf.epub")

	f, err := os.Create(epubPath)
	if err != nil {
		t.Fatal(err)
	}
	zw := zip.NewWriter(f)
	w, _ := zw.Create("mimetype")
	w.Write([]byte("application/epub+zip"))
	zw.Close()
	f.Close()

	if _, err := ParseFile(epubPath); err == nil {
		t.Error("expected error for epub missing container.xml, got nil")
	}
}
