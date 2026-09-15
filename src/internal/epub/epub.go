package epub

import (
	"archive/zip"
	"fmt"
)

// Metadata is the set of fields derived from an EPUB's embedded OPF package document.
type Metadata struct {
	Title         string
	SortTitle     string
	Author        string
	SortAuthor    string
	Series        string
	SeriesIndex   float64
	Description   string
	Language      string
	Publisher     string
	PublishedDate string
	Identifier    string

	CoverData      []byte
	CoverMediaType string
}

// ParseFile opens an EPUB file read-only and extracts its metadata and cover image.
// The file itself is never modified.
func ParseFile(path string) (*Metadata, error) {
	zr, err := zip.OpenReader(path)
	if err != nil {
		return nil, fmt.Errorf("open epub: %w", err)
	}
	defer zr.Close()

	return parseZip(&zr.Reader)
}

func parseZip(zr *zip.Reader) (*Metadata, error) {
	opfPath, err := findOPFPath(zr)
	if err != nil {
		return nil, err
	}

	pkg, err := parseOPF(zr, opfPath)
	if err != nil {
		return nil, err
	}

	m := buildMetadata(pkg)
	if m.Title == "" {
		return nil, fmt.Errorf("no title found in opf")
	}

	if item := findCoverHref(pkg); item != nil {
		data, mimeType, err := extractCover(zr, opfPath, item)
		if err == nil {
			m.CoverData = data
			m.CoverMediaType = mimeType
		}
	}

	return &m, nil
}
