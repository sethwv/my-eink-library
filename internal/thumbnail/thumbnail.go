// Package thumbnail resizes and caches book cover images to disk.
package thumbnail

import (
	"bytes"
	"fmt"
	"image"
	_ "image/gif"
	"image/jpeg"
	_ "image/jpeg"
	_ "image/png"
	"os"
	"path/filepath"

	"github.com/disintegration/imaging"
)

// Store caches resized JPEG cover thumbnails on disk, keyed by book ID.
type Store struct {
	dir   string
	width int
}

// NewStore creates (if needed) coversDir and returns a Store that resizes
// covers to the given width, preserving aspect ratio.
func NewStore(coversDir string, width int) (*Store, error) {
	if err := os.MkdirAll(coversDir, 0o755); err != nil {
		return nil, fmt.Errorf("create covers dir: %w", err)
	}
	if width < 1 {
		width = 300
	}
	return &Store{dir: coversDir, width: width}, nil
}

// SaveCover decodes, resizes, and writes the cover for bookID, returning the
// filename (relative to the covers dir) to store as cover_path.
func (s *Store) SaveCover(bookID int64, data []byte, mediaType string) (string, error) {
	img, _, err := image.Decode(bytes.NewReader(data))
	if err != nil {
		return "", fmt.Errorf("decode cover: %w", err)
	}

	resized := imaging.Resize(img, s.width, 0, imaging.Lanczos)

	name := fmt.Sprintf("%d.jpg", bookID)
	fullPath := filepath.Join(s.dir, name)

	f, err := os.Create(fullPath)
	if err != nil {
		return "", fmt.Errorf("create thumbnail file: %w", err)
	}
	defer f.Close()

	if err := jpeg.Encode(f, resized, &jpeg.Options{Quality: 82}); err != nil {
		return "", fmt.Errorf("encode thumbnail: %w", err)
	}

	return name, nil
}

// Path returns the full filesystem path for a cover_path value.
func (s *Store) Path(relPath string) string {
	return filepath.Join(s.dir, relPath)
}
