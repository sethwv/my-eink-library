package thumbnail

import (
	"bytes"
	"image"
	"image/color"
	"image/jpeg"
	"os"
	"path/filepath"
	"testing"
)

func TestSaveCoverResizesAndWritesJPEG(t *testing.T) {
	store, err := NewStore(t.TempDir(), 40)
	if err != nil {
		t.Fatal(err)
	}

	source := image.NewRGBA(image.Rect(0, 0, 100, 50))
	for y := range 50 {
		for x := range 100 {
			source.Set(x, y, color.RGBA{R: uint8(x), G: uint8(y), B: 128, A: 255})
		}
	}

	var data bytes.Buffer
	if err := jpeg.Encode(&data, source, nil); err != nil {
		t.Fatal(err)
	}

	name, err := store.SaveCover(42, data.Bytes(), "image/jpeg")
	if err != nil {
		t.Fatal(err)
	}
	if name != "42.jpg" {
		t.Errorf("name = %q, want 42.jpg", name)
	}

	f, err := os.Open(filepath.Join(store.dir, name))
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()

	resized, format, err := image.Decode(f)
	if err != nil {
		t.Fatal(err)
	}
	if format != "jpeg" {
		t.Errorf("format = %q, want jpeg", format)
	}
	if got, want := resized.Bounds().Size(), image.Pt(40, 20); got != want {
		t.Errorf("thumbnail size = %v, want %v", got, want)
	}
}

func TestSaveCoverRejectsInvalidImage(t *testing.T) {
	store, err := NewStore(t.TempDir(), 40)
	if err != nil {
		t.Fatal(err)
	}

	name, err := store.SaveCover(42, []byte("not an image"), "image/jpeg")
	if err == nil {
		t.Fatal("SaveCover returned nil error for invalid image")
	}
	if name != "" {
		t.Errorf("name = %q, want empty", name)
	}
	if _, err := os.Stat(filepath.Join(store.dir, "42.jpg")); !os.IsNotExist(err) {
		t.Errorf("thumbnail file exists or could not be checked: %v", err)
	}
}
