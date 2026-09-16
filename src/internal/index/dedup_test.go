package index

import (
	"os"
	"path/filepath"
	"testing"
)

func TestScan_DedupsTitleAuthorAcrossRoots(t *testing.T) {
	rootA := t.TempDir()
	rootB := t.TempDir()
	// Same title+author, different filenames/roots -- the duplicate case
	// multi-root LIBRARY_PATH introduces.
	writeTestEpub(t, filepath.Join(rootA, "book.epub"), "Same Book", "Same Author")
	writeTestEpub(t, filepath.Join(rootB, "different-name.epub"), "Same Book", "Same Author")

	db := openTestDB(t)
	if err := db.Scan([]string{rootA, rootB}, nil); err != nil {
		t.Fatalf("Scan: %v", err)
	}

	if n, _ := db.Count(Filter{}); n != 1 {
		t.Fatalf("Count = %d, want 1 (duplicate should be consolidated)", n)
	}

	books, err := db.List(SortTitle, false, 1, 10, Filter{})
	if err != nil {
		t.Fatal(err)
	}
	if len(books) != 1 {
		t.Fatalf("List returned %d books, want 1", len(books))
	}
	if books[0].LibraryRoot != rootA {
		t.Errorf("canonical LibraryRoot = %q, want %q (lower-indexed root should win)", books[0].LibraryRoot, rootA)
	}

	locs, err := db.LocationsForBooks([]int64{books[0].ID})
	if err != nil {
		t.Fatal(err)
	}
	if len(locs[books[0].ID]) != 1 {
		t.Fatalf("LocationsForBooks = %v, want exactly 1 extra location", locs[books[0].ID])
	}
	if locs[books[0].ID][0].LibraryRoot != rootB {
		t.Errorf("extra location root = %q, want %q", locs[books[0].ID][0].LibraryRoot, rootB)
	}
}

func TestScan_PromotesLocationWhenCanonicalFileRemoved(t *testing.T) {
	rootA := t.TempDir()
	rootB := t.TempDir()
	pathA := filepath.Join(rootA, "book.epub")
	pathB := filepath.Join(rootB, "book.epub")
	writeTestEpub(t, pathA, "Same Book", "Same Author")
	writeTestEpub(t, pathB, "Same Book", "Same Author")

	db := openTestDB(t)
	if err := db.Scan([]string{rootA, rootB}, nil); err != nil {
		t.Fatalf("Scan: %v", err)
	}
	if n, _ := db.Count(Filter{}); n != 1 {
		t.Fatalf("Count = %d, want 1", n)
	}

	if err := os.Remove(pathA); err != nil {
		t.Fatal(err)
	}
	if err := db.Scan([]string{rootA, rootB}, nil); err != nil {
		t.Fatalf("Scan after removing canonical file: %v", err)
	}

	if n, _ := db.Count(Filter{}); n != 1 {
		t.Fatalf("Count after canonical removal = %d, want 1 (book should survive via promoted location)", n)
	}
	books, err := db.List(SortTitle, false, 1, 10, Filter{})
	if err != nil {
		t.Fatal(err)
	}
	if len(books) != 1 {
		t.Fatalf("List returned %d books, want 1", len(books))
	}
	if books[0].LibraryRoot != rootB {
		t.Errorf("promoted LibraryRoot = %q, want %q", books[0].LibraryRoot, rootB)
	}
}

func TestConsolidateDuplicateTitles_MergesPreexistingRows(t *testing.T) {
	db := openTestDB(t)

	// Insert two independent books rows directly, bypassing scan-time
	// dedup, to simulate duplicates that predate this feature (e.g. from
	// the earlier multi-root rollout before this consolidation existed).
	insert := func(root, path string, addedAt int64) int64 {
		res, err := db.sql.Exec(`
			INSERT INTO books (library_root, file_path, file_size, file_mtime, title, sort_title, author, sort_author, added_at, updated_at)
			VALUES (?, ?, 0, 0, 'Dup Book', 'dup book', 'Dup Author', 'dup author', ?, ?)`,
			root, path, addedAt, addedAt)
		if err != nil {
			t.Fatal(err)
		}
		id, _ := res.LastInsertId()
		return id
	}
	idHigh := insert("/lib/second", "a.epub", 200)
	idLow := insert("/lib/first", "b.epub", 100)

	// Give the shelf-owned row a shelf membership to confirm it survives the merge.
	if _, err := db.sql.Exec(`INSERT INTO shelves (username, slug, name, created_at) VALUES ('u', 'favourites', 'Favourites', 0)`); err != nil {
		t.Fatal(err)
	}
	if _, err := db.sql.Exec(`INSERT INTO shelf_books (shelf_id, book_id, added_at) VALUES (1, ?, 0)`, idHigh); err != nil {
		t.Fatal(err)
	}

	if err := db.ConsolidateDuplicateTitles([]string{"/lib/first", "/lib/second"}); err != nil {
		t.Fatalf("ConsolidateDuplicateTitles: %v", err)
	}

	if n, _ := db.Count(Filter{}); n != 1 {
		t.Fatalf("Count = %d, want 1", n)
	}
	books, err := db.List(SortTitle, false, 1, 10, Filter{})
	if err != nil {
		t.Fatal(err)
	}
	if books[0].ID != idLow {
		t.Errorf("surviving book id = %d, want %d (lower-indexed root should win)", books[0].ID, idLow)
	}
	if books[0].LibraryRoot != "/lib/first" {
		t.Errorf("surviving LibraryRoot = %q, want /lib/first", books[0].LibraryRoot)
	}

	ids, err := db.ShelfBookIDs(1)
	if err != nil {
		t.Fatal(err)
	}
	if !ids[idLow] {
		t.Errorf("shelf membership was not carried over to the surviving book")
	}
}

func TestMergeDuplicateISBN(t *testing.T) {
	db := openTestDB(t)

	insert := func(root, path, title string) int64 {
		res, err := db.sql.Exec(`
			INSERT INTO books (library_root, file_path, file_size, file_mtime, title, sort_title, author, sort_author, added_at, updated_at)
			VALUES (?, ?, 0, 0, ?, ?, 'Author', 'author', 0, 0)`,
			root, path, title, title)
		if err != nil {
			t.Fatal(err)
		}
		id, _ := res.LastInsertId()
		return id
	}
	idA := insert("/lib/a", "one.epub", "Translated Title One")
	idB := insert("/lib/b", "two.epub", "Different Title Two")

	if err := db.ApplyEnrichment(idA, MetadataPatch{ISBN: "9781234567897"}, SourceHardcover); err != nil {
		t.Fatal(err)
	}
	if err := db.ApplyEnrichment(idB, MetadataPatch{ISBN: "9781234567897"}, SourceHardcover); err != nil {
		t.Fatal(err)
	}

	if err := db.MergeDuplicateISBN(idB, []string{"/lib/a", "/lib/b"}); err != nil {
		t.Fatalf("MergeDuplicateISBN: %v", err)
	}

	if n, _ := db.Count(Filter{}); n != 1 {
		t.Fatalf("Count = %d, want 1", n)
	}
	books, err := db.List(SortTitle, false, 1, 10, Filter{})
	if err != nil {
		t.Fatal(err)
	}
	if books[0].ID != idA {
		t.Errorf("surviving book id = %d, want %d (lower-indexed root should win)", books[0].ID, idA)
	}
}

func TestFilter_HideNoMatch(t *testing.T) {
	db := openTestDB(t)

	insert := func(title string) int64 {
		res, err := db.sql.Exec(`
			INSERT INTO books (library_root, file_path, file_size, file_mtime, title, sort_title, author, sort_author, added_at, updated_at)
			VALUES ('/lib', ?, 0, 0, ?, ?, 'Author', 'author', 0, 0)`,
			title+".epub", title, title)
		if err != nil {
			t.Fatal(err)
		}
		id, _ := res.LastInsertId()
		return id
	}
	matched := insert("Matched Book")
	unmatchedChaptarr := insert("Unmatched Chaptarr Book")
	unmatchedHardcover := insert("Unmatched Hardcover Book")

	if err := db.SetChaptarrStatus(matched, "done"); err != nil {
		t.Fatal(err)
	}
	if err := db.SetChaptarrStatus(unmatchedChaptarr, "no_match"); err != nil {
		t.Fatal(err)
	}
	if err := db.SetHardcoverStatus(unmatchedHardcover, "no_match"); err != nil {
		t.Fatal(err)
	}

	n, err := db.Count(Filter{HideNoChaptarrMatch: true})
	if err != nil {
		t.Fatal(err)
	}
	if n != 2 {
		t.Fatalf("Count with HideNoChaptarrMatch = %d, want 2", n)
	}

	n, err = db.Count(Filter{HideNoHardcoverMatch: true})
	if err != nil {
		t.Fatal(err)
	}
	if n != 2 {
		t.Fatalf("Count with HideNoHardcoverMatch = %d, want 2", n)
	}

	n, err = db.Count(Filter{HideNoChaptarrMatch: true, HideNoHardcoverMatch: true})
	if err != nil {
		t.Fatal(err)
	}
	if n != 1 {
		t.Fatalf("Count with both hide filters = %d, want 1", n)
	}

	authors, err := db.ListAuthors(Filter{HideNoChaptarrMatch: true})
	if err != nil {
		t.Fatal(err)
	}
	var total int
	for _, a := range authors {
		total += a.Count
	}
	if total != 2 {
		t.Fatalf("ListAuthors total with HideNoChaptarrMatch = %d, want 2", total)
	}
}
