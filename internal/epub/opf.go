// Package epub extracts metadata and cover images from EPUB files using
// only their embedded OPF package document and manifest — no sidecar files.
package epub

import (
	"archive/zip"
	"encoding/xml"
	"fmt"
	"io"
	"path"
	"regexp"
	"strconv"
	"strings"
)

// xmlVersionPattern matches an XML declaration's version attribute, so a
// declared "1.1" can be downgraded to "1.0" before parsing. Go's
// encoding/xml hard-rejects any version other than 1.0 in the XML
// declaration, but the handful of EPUB-producing tools that emit "1.1"
// don't actually use any 1.1-only feature in the OPF/container documents
// they generate — they're just mislabeled 1.0 XML, so relabeling is safe
// and avoids failing to index an otherwise well-formed book.
var xmlVersionPattern = regexp.MustCompile(`^(\s*<\?xml[^>]*\bversion\s*=\s*["'])1\.1(["'])`)

func normalizeXMLVersion(data []byte) []byte {
	return xmlVersionPattern.ReplaceAll(data, []byte("${1}1.0${2}"))
}

const dcNS = "http://purl.org/dc/elements/1.1/"

type containerXML struct {
	Rootfiles struct {
		Rootfile []struct {
			FullPath string `xml:"full-path,attr"`
		} `xml:"rootfile"`
	} `xml:"rootfiles"`
}

type opfPackage struct {
	Metadata opfMetadata `xml:"metadata"`
	Manifest opfManifest `xml:"manifest"`
	Guide    opfGuide    `xml:"guide"`
}

type opfMetadata struct {
	Title       []string        `xml:"http://purl.org/dc/elements/1.1/ title"`
	Creator     []string        `xml:"http://purl.org/dc/elements/1.1/ creator"`
	Identifier  []opfIdentifier `xml:"http://purl.org/dc/elements/1.1/ identifier"`
	Language    []string        `xml:"http://purl.org/dc/elements/1.1/ language"`
	Publisher   []string        `xml:"http://purl.org/dc/elements/1.1/ publisher"`
	Date        []string        `xml:"http://purl.org/dc/elements/1.1/ date"`
	Description []string        `xml:"http://purl.org/dc/elements/1.1/ description"`
	Meta        []opfMeta       `xml:"meta"`
}

// opfIdentifier is a single dc:identifier element. scheme (typically
// opf:scheme="ISBN"/"ASIN"/etc.) distinguishes an ISBN/ASIN from a Calibre
// UUID or other identifier scheme sharing the same element. The struct tag
// omits a namespace so it matches the attribute by local name regardless of
// which prefix (or none) the source EPUB used for it.
type opfIdentifier struct {
	Scheme string `xml:"scheme,attr"`
	Value  string `xml:",chardata"`
}

type opfMeta struct {
	Name     string `xml:"name,attr"`
	Content  string `xml:"content,attr"`
	Property string `xml:"property,attr"`
	Value    string `xml:",chardata"`
}

type opfManifest struct {
	Items []opfItem `xml:"item"`
}

type opfItem struct {
	ID         string `xml:"id,attr"`
	Href       string `xml:"href,attr"`
	MediaType  string `xml:"media-type,attr"`
	Properties string `xml:"properties,attr"`
}

type opfGuide struct {
	References []opfReference `xml:"reference"`
}

type opfReference struct {
	Type string `xml:"type,attr"`
	Href string `xml:"href,attr"`
}

// findOPFPath reads META-INF/container.xml to locate the root OPF package document.
func findOPFPath(zr *zip.Reader) (string, error) {
	f, err := openInZip(zr, "META-INF/container.xml")
	if err != nil {
		return "", fmt.Errorf("container.xml: %w", err)
	}
	defer f.Close()

	data, err := io.ReadAll(f)
	if err != nil {
		return "", fmt.Errorf("read container.xml: %w", err)
	}

	var c containerXML
	if err := xml.Unmarshal(normalizeXMLVersion(data), &c); err != nil {
		return "", fmt.Errorf("parse container.xml: %w", err)
	}
	if len(c.Rootfiles.Rootfile) == 0 || c.Rootfiles.Rootfile[0].FullPath == "" {
		return "", fmt.Errorf("no rootfile in container.xml")
	}
	return c.Rootfiles.Rootfile[0].FullPath, nil
}

func parseOPF(zr *zip.Reader, opfPath string) (*opfPackage, error) {
	f, err := openInZip(zr, opfPath)
	if err != nil {
		return nil, fmt.Errorf("open opf %s: %w", opfPath, err)
	}
	defer f.Close()

	data, err := io.ReadAll(f)
	if err != nil {
		return nil, fmt.Errorf("read opf: %w", err)
	}

	var pkg opfPackage
	if err := xml.Unmarshal(normalizeXMLVersion(data), &pkg); err != nil {
		return nil, fmt.Errorf("parse opf xml: %w", err)
	}
	return &pkg, nil
}

func openInZip(zr *zip.Reader, name string) (io.ReadCloser, error) {
	for _, f := range zr.File {
		if f.Name == name {
			return f.Open()
		}
	}
	return nil, fmt.Errorf("not found in archive: %s", name)
}

func metaValue(meta []opfMeta, name string) string {
	for _, m := range meta {
		if m.Name == name {
			return m.Content
		}
	}
	return ""
}

// buildMetadata derives the display Metadata from a parsed OPF package.
func buildMetadata(pkg *opfPackage) Metadata {
	m := Metadata{}

	if len(pkg.Metadata.Title) > 0 {
		m.Title = strings.TrimSpace(pkg.Metadata.Title[0])
	}
	if len(pkg.Metadata.Creator) > 0 {
		authors := cleanAuthorNames(pkg.Metadata.Creator)
		m.Author = strings.Join(authors, " & ")
		if len(authors) > 0 {
			m.SortAuthor = sortAuthorName(authors[0])
		}
	}
	if len(pkg.Metadata.Language) > 0 {
		m.Language = strings.TrimSpace(pkg.Metadata.Language[0])
	}
	if len(pkg.Metadata.Publisher) > 0 {
		m.Publisher = strings.TrimSpace(pkg.Metadata.Publisher[0])
	}
	if len(pkg.Metadata.Date) > 0 {
		m.PublishedDate = strings.TrimSpace(pkg.Metadata.Date[0])
	}
	if len(pkg.Metadata.Description) > 0 {
		m.Description = strings.TrimSpace(pkg.Metadata.Description[0])
	}
	if len(pkg.Metadata.Identifier) > 0 {
		// Prefer an identifier explicitly scheme-tagged as an ISBN/ASIN over
		// whatever happens to be first (often a Calibre UUID); fall back to
		// the first identifier when nothing is scheme-tagged.
		m.Identifier = strings.TrimSpace(pkg.Metadata.Identifier[0].Value)
		for _, id := range pkg.Metadata.Identifier {
			switch strings.ToUpper(strings.TrimSpace(id.Scheme)) {
			case "ISBN", "ISBN-13", "ISBN-10", "ASIN", "MOBI-ASIN":
				m.Identifier = strings.TrimSpace(id.Value)
			}
		}
	}

	// Calibre's series convention, the de facto standard for EPUB series metadata.
	m.Series = strings.TrimSpace(metaValue(pkg.Metadata.Meta, "calibre:series"))
	if idx := metaValue(pkg.Metadata.Meta, "calibre:series_index"); idx != "" {
		if f, err := strconv.ParseFloat(strings.TrimSpace(idx), 64); err == nil {
			m.SeriesIndex = f
		}
	}

	m.SortTitle = sortTitleFor(m.Title)

	return m
}

// sortTitleFor strips a leading English article and casefolds for stable ordering.
func sortTitleFor(title string) string {
	lower := strings.ToLower(strings.TrimSpace(title))
	for _, article := range []string{"a ", "an ", "the "} {
		if strings.HasPrefix(lower, article) {
			return strings.TrimSpace(lower[len(article):])
		}
	}
	return lower
}

// authorSplitRE splits a single dc:creator string on the separators a
// multi-author byline might use — "&", "/", ";", or the word "and" (word-
// boundary so it doesn't match inside a name like "Anderson"). A plain
// comma is deliberately excluded: it's reserved for the "Last, First"
// single-name shape normalizeAuthorName handles, not a multi-author list
// separator (EPUB metadata puts each author in its own dc:creator element
// far more often than it comma-separates several into one).
var authorSplitRE = regexp.MustCompile(`(?i)\s*(?:&|;|/|\band\b)\s*`)

func splitAuthorNames(raw string) []string {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return nil
	}
	var names []string
	for _, p := range authorSplitRE.Split(raw, -1) {
		if p = strings.TrimSpace(p); p != "" {
			names = append(names, p)
		}
	}
	return names
}

// normalizeAuthorName converts a single already-split name from "Last,
// First" to "First Last" for a consistent display form, and collapses
// internal whitespace. A name with no comma (already "First Last", or a
// single mononym) passes through unchanged apart from whitespace collapse.
func normalizeAuthorName(name string) string {
	name = strings.Join(strings.Fields(name), " ")
	if strings.Count(name, ",") == 1 {
		parts := strings.SplitN(name, ",", 2)
		last, first := strings.TrimSpace(parts[0]), strings.TrimSpace(parts[1])
		if last != "" && first != "" {
			return first + " " + last
		}
	}
	return name
}

// cleanAuthorNames turns raw dc:creator values (each element may itself
// embed multiple names — see splitAuthorNames) into a de-duplicated,
// normalized, order-preserving list for display. Dedup is an exact
// case-insensitive match on the normalized name, not fuzzy — two spellings
// of the same person that aren't otherwise identical (e.g. a full legal
// name vs. a pen name, "Geneva Lee Albin" vs "Geneva Lee") won't be merged,
// since a fuzzier heuristic risks wrongly conflating distinct co-authors
// who happen to share a surname (e.g. real sibling authors "P.C. Cast" and
// "Kristin Cast"). That class of duplicate needs an external identity
// source (Hardcover/Chaptarr author matching), not local EPUB parsing.
func cleanAuthorNames(creators []string) []string {
	seen := make(map[string]bool, len(creators))
	names := make([]string, 0, len(creators))
	for _, c := range creators {
		for _, part := range splitAuthorNames(c) {
			name := normalizeAuthorName(part)
			if name == "" {
				continue
			}
			key := strings.ToLower(name)
			if seen[key] {
				continue
			}
			seen[key] = true
			names = append(names, name)
		}
	}
	return names
}

// sortAuthorName converts "First Last" to "Last, First"; leaves "Last, First" as-is.
func sortAuthorName(name string) string {
	name = strings.TrimSpace(name)
	if strings.Contains(name, ",") {
		return strings.ToLower(name)
	}
	parts := strings.Fields(name)
	if len(parts) < 2 {
		return strings.ToLower(name)
	}
	last := parts[len(parts)-1]
	first := strings.Join(parts[:len(parts)-1], " ")
	return strings.ToLower(last + ", " + first)
}

// resolveHref resolves a manifest href relative to the OPF file's directory.
func resolveHref(opfPath, href string) string {
	if href == "" {
		return ""
	}
	dir := path.Dir(opfPath)
	if dir == "." {
		return href
	}
	return path.Join(dir, href)
}
