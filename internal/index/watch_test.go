package index

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestWatcher_DetectsNewAndRemovedFiles(t *testing.T) {
	libDir := t.TempDir()
	db := openTestDB(t)
	if err := db.Scan(libDir, nil); err != nil {
		t.Fatal(err)
	}

	w, err := NewWatcher(libDir, db, nil)
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go w.Run(ctx)

	bookPath := filepath.Join(libDir, "new.epub")
	writeTestEpub(t, bookPath, "New Book", "Author")

	waitFor(t, 5*time.Second, func() bool {
		n, _ := db.Count(Filter{})
		return n == 1
	}, "book to appear in index after create")

	if err := os.Remove(bookPath); err != nil {
		t.Fatal(err)
	}

	waitFor(t, 5*time.Second, func() bool {
		n, _ := db.Count(Filter{})
		return n == 0
	}, "book to disappear from index after delete")
}

func waitFor(t *testing.T, timeout time.Duration, cond func() bool, desc string) {
	t.Helper()
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		if cond() {
			return
		}
		time.Sleep(50 * time.Millisecond)
	}
	t.Fatalf("timed out waiting for: %s", desc)
}
