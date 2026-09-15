package epub

import (
	"archive/zip"
	"io"
	"mime"
	"path"
	"strings"
)

// findCoverHref locates the manifest item representing the cover image,
// trying the EPUB3 properties="cover-image" convention first, then the
// EPUB2 <meta name="cover" content="ID"> convention used by Calibre.
func findCoverHref(pkg *opfPackage) *opfItem {
	for i, item := range pkg.Manifest.Items {
		if hasProperty(item.Properties, "cover-image") {
			return &pkg.Manifest.Items[i]
		}
	}

	coverID := metaValue(pkg.Metadata.Meta, "cover")
	if coverID != "" {
		for i, item := range pkg.Manifest.Items {
			if item.ID == coverID {
				return &pkg.Manifest.Items[i]
			}
		}
	}

	// Last resort: an item whose id/href suggests it's a cover image.
	for i, item := range pkg.Manifest.Items {
		if !strings.HasPrefix(item.MediaType, "image/") {
			continue
		}
		lower := strings.ToLower(item.ID + item.Href)
		if strings.Contains(lower, "cover") {
			return &pkg.Manifest.Items[i]
		}
	}

	return nil
}

func hasProperty(properties, want string) bool {
	for _, p := range strings.Fields(properties) {
		if p == want {
			return true
		}
	}
	return false
}

// extractCover reads the cover image bytes and mimetype out of the zip archive.
func extractCover(zr *zip.Reader, opfPath string, item *opfItem) ([]byte, string, error) {
	if item == nil {
		return nil, "", nil
	}
	full := resolveHref(opfPath, item.Href)
	f, err := openInZip(zr, full)
	if err != nil {
		return nil, "", err
	}
	defer f.Close()

	data, err := io.ReadAll(f)
	if err != nil {
		return nil, "", err
	}

	mimeType := item.MediaType
	if mimeType == "" {
		mimeType = mime.TypeByExtension(path.Ext(full))
	}
	return data, mimeType, nil
}
