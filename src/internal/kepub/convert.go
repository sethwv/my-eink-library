// Package kepub converts EPUB files to Kobo's kepub format on the fly,
// using kepubify as a library rather than shelling out to a bundled binary.
package kepub

import (
	"archive/zip"
	"bytes"
	"context"
	"encoding/xml"
	"fmt"
	"io"
	"io/fs"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/pgaskin/kepubify/v4/kepub"
)

// Metadata contains library metadata that can fill missing Calibre fields in
// the generated KEPUB's package document.
type Metadata struct {
	Series      string
	SeriesIndex float64
}

// ConvertFile reads an EPUB from path and writes a kepub-converted version to w.
// The source file is opened read-only and never modified.
func ConvertFile(ctx context.Context, w io.Writer, path string, metadata *Metadata) error {
	zr, err := zip.OpenReader(path)
	if err != nil {
		return fmt.Errorf("open epub: %w", err)
	}
	defer zr.Close()

	var source fs.FS = zr
	if metadata != nil {
		source, err = calibreMetadataOverlay(&zr.Reader, *metadata)
		if err != nil {
			return err
		}
	}

	c := kepub.NewConverter()
	if err := c.Convert(ctx, w, source); err != nil {
		return fmt.Errorf("convert to kepub: %w", err)
	}
	return nil
}

type containerXML struct {
	Rootfiles struct {
		Rootfile []struct {
			FullPath string `xml:"full-path,attr"`
		} `xml:"rootfile"`
	} `xml:"rootfiles"`
}

type overlayFS struct {
	fs.FS
	opfPath string
	opf     []byte
}

func (f overlayFS) Open(name string) (fs.File, error) {
	if name == f.opfPath {
		return &overlayFile{Reader: bytes.NewReader(f.opf), name: name, size: int64(len(f.opf))}, nil
	}
	return f.FS.Open(name)
}

func (f overlayFS) ReadDir(name string) ([]fs.DirEntry, error) {
	return fs.ReadDir(f.FS, name)
}

type overlayFile struct {
	*bytes.Reader
	name string
	size int64
}

func (f *overlayFile) Close() error { return nil }

func (f *overlayFile) Stat() (fs.FileInfo, error) {
	return overlayFileInfo{name: f.name, size: f.size}, nil
}

type overlayFileInfo struct {
	name string
	size int64
}

func (f overlayFileInfo) Name() string       { return f.name }
func (f overlayFileInfo) Size() int64        { return f.size }
func (f overlayFileInfo) Mode() fs.FileMode  { return 0 }
func (f overlayFileInfo) ModTime() time.Time { return time.Time{} }
func (f overlayFileInfo) IsDir() bool        { return false }
func (f overlayFileInfo) Sys() any           { return nil }

var metadataCloseRE = regexp.MustCompile(`(?i)</(?:[[:alnum:]_-]+:)?metadata\s*>`)
var metadataSelfClosingRE = regexp.MustCompile(`(?is)<(?:[[:alnum:]_-]+:)?metadata\b[^>]*/\s*>`)

func calibreMetadataOverlay(zr *zip.Reader, metadata Metadata) (fs.FS, error) {
	containerFile, err := zr.Open("META-INF/container.xml")
	if err != nil {
		return nil, fmt.Errorf("read EPUB container: %w", err)
	}
	defer containerFile.Close()

	var container containerXML
	if err := xml.NewDecoder(containerFile).Decode(&container); err != nil {
		return nil, fmt.Errorf("parse EPUB container: %w", err)
	}
	if len(container.Rootfiles.Rootfile) == 0 || container.Rootfiles.Rootfile[0].FullPath == "" {
		return nil, fmt.Errorf("parse EPUB container: no package document")
	}
	opfPath := container.Rootfiles.Rootfile[0].FullPath
	opfFile, err := zr.Open(opfPath)
	if err != nil {
		return nil, fmt.Errorf("read EPUB package document: %w", err)
	}
	opf, err := io.ReadAll(opfFile)
	opfFile.Close()
	if err != nil {
		return nil, fmt.Errorf("read EPUB package document: %w", err)
	}

	enriched, err := addCalibreSeriesMetadata(opf, metadata)
	if err != nil {
		return nil, err
	}
	if bytes.Equal(enriched, opf) {
		return zr, nil
	}
	return overlayFS{FS: zr, opfPath: opfPath, opf: enriched}, nil
}

func addCalibreSeriesMetadata(opf []byte, metadata Metadata) ([]byte, error) {
	hasSeries, hasIndex, err := existingCalibreSeriesMetadata(opf)
	if err != nil {
		return nil, fmt.Errorf("parse EPUB package metadata: %w", err)
	}

	var additions strings.Builder
	if !hasSeries && strings.TrimSpace(metadata.Series) != "" {
		additions.WriteString(`<meta name="calibre:series" content="` + escapeXML(metadata.Series) + `"/>`)
	}
	if !hasIndex && metadata.SeriesIndex > 0 {
		additions.WriteString(`<meta name="calibre:series_index" content="` + strconv.FormatFloat(metadata.SeriesIndex, 'f', -1, 64) + `"/>`)
	}
	if additions.Len() == 0 {
		return opf, nil
	}

	if loc := metadataCloseRE.FindIndex(opf); loc != nil {
		result := make([]byte, 0, len(opf)+additions.Len())
		result = append(result, opf[:loc[0]]...)
		result = append(result, additions.String()...)
		result = append(result, opf[loc[0]:]...)
		return result, nil
	}
	loc := metadataSelfClosingRE.FindIndex(opf)
	if loc == nil {
		return nil, fmt.Errorf("parse EPUB package metadata: no metadata element")
	}
	result := make([]byte, 0, len(opf)+additions.Len())
	result = append(result, opf[:loc[0]]...)
	start := bytes.TrimSuffix(opf[loc[0]:loc[1]], []byte("/>"))
	result = append(result, start...)
	result = append(result, '>')
	result = append(result, additions.String()...)
	result = append(result, "</metadata>"...)
	result = append(result, opf[loc[1]:]...)
	return result, nil
}

func existingCalibreSeriesMetadata(opf []byte) (hasSeries, hasIndex bool, err error) {
	decoder := xml.NewDecoder(bytes.NewReader(opf))
	for {
		token, err := decoder.Token()
		if err == io.EOF {
			return hasSeries, hasIndex, nil
		}
		if err != nil {
			return false, false, err
		}
		start, ok := token.(xml.StartElement)
		if !ok || start.Name.Local != "meta" {
			continue
		}
		var name, content string
		for _, attr := range start.Attr {
			switch attr.Name.Local {
			case "name":
				name = attr.Value
			case "content":
				content = attr.Value
			}
		}
		if strings.TrimSpace(content) == "" {
			continue
		}
		switch name {
		case "calibre:series":
			hasSeries = true
		case "calibre:series_index":
			hasIndex = true
		}
	}
}

func escapeXML(value string) string {
	var escaped strings.Builder
	_ = xml.EscapeText(&escaped, []byte(value))
	return escaped.String()
}
