// Package kepub converts EPUB files to Kobo's kepub format on the fly,
// using kepubify as a library rather than shelling out to a bundled binary.
package kepub

import (
	"archive/zip"
	"context"
	"fmt"
	"io"

	"github.com/pgaskin/kepubify/v4/kepub"
)

// ConvertFile reads an EPUB from path and writes a kepub-converted version to w.
// The source file is opened read-only and never modified.
func ConvertFile(ctx context.Context, w io.Writer, path string) error {
	zr, err := zip.OpenReader(path)
	if err != nil {
		return fmt.Errorf("open epub: %w", err)
	}
	defer zr.Close()

	c := kepub.NewConverter()
	if err := c.Convert(ctx, w, zr); err != nil {
		return fmt.Errorf("convert to kepub: %w", err)
	}
	return nil
}
