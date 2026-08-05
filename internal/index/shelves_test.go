package index

import (
	"path/filepath"
	"testing"
)

func TestEnsureSystemShelf_CreatesAndReuses(t *testing.T) {
	db := openTestDB(t)

	id1, err := db.EnsureSystemShelf("alice", "favourites", "Favourites")
	if err != nil {
		t.Fatal(err)
	}
	if id1 == 0 {
		t.Fatal("expected non-zero shelf id")
	}

	id2, err := db.EnsureSystemShelf("alice", "favourites", "Favourites")
	if err != nil {
		t.Fatal(err)
	}
	if id1 != id2 {
		t.Errorf("EnsureSystemShelf not idempotent: got %d then %d", id1, id2)
	}

	// Different user gets their own shelf, same slug.
	id3, err := db.EnsureSystemShelf("bob", "favourites", "Favourites")
	if err != nil {
		t.Fatal(err)
	}
	if id3 == id1 {
		t.Error("expected different users to get different shelf ids")
	}
}

func TestShelfBooks_AddRemoveIsOn(t *testing.T) {
	db := openTestDB(t)
	shelfID, err := db.EnsureSystemShelf("alice", "favourites", "Favourites")
	if err != nil {
		t.Fatal(err)
	}

	on, err := db.IsBookOnShelf(shelfID, 42)
	if err != nil {
		t.Fatal(err)
	}
	if on {
		t.Error("expected book not on shelf initially")
	}

	if err := db.AddBookToShelf(shelfID, 42); err != nil {
		t.Fatal(err)
	}
	on, err = db.IsBookOnShelf(shelfID, 42)
	if err != nil {
		t.Fatal(err)
	}
	if !on {
		t.Error("expected book on shelf after AddBookToShelf")
	}

	// Adding again is a no-op, not an error.
	if err := db.AddBookToShelf(shelfID, 42); err != nil {
		t.Fatal(err)
	}

	if err := db.RemoveBookFromShelf(shelfID, 42); err != nil {
		t.Fatal(err)
	}
	on, err = db.IsBookOnShelf(shelfID, 42)
	if err != nil {
		t.Fatal(err)
	}
	if on {
		t.Error("expected book not on shelf after RemoveBookFromShelf")
	}
}

func TestShelfBookIDs(t *testing.T) {
	db := openTestDB(t)
	shelfID, err := db.EnsureSystemShelf("alice", "favourites", "Favourites")
	if err != nil {
		t.Fatal(err)
	}

	for _, id := range []int64{1, 2, 3} {
		if err := db.AddBookToShelf(shelfID, id); err != nil {
			t.Fatal(err)
		}
	}

	ids, err := db.ShelfBookIDs(shelfID)
	if err != nil {
		t.Fatal(err)
	}
	if len(ids) != 3 || !ids[1] || !ids[2] || !ids[3] {
		t.Errorf("ShelfBookIDs = %v, want {1,2,3}", ids)
	}
}

func TestFilter_ShelfID(t *testing.T) {
	libDir := t.TempDir()
	writeTestEpub(t, filepath.Join(libDir, "b1.epub"), "Zebra Tales", "Amy Zed")
	writeTestEpub(t, filepath.Join(libDir, "b2.epub"), "Banana Republic", "Bob Young")

	db := openTestDB(t)
	if err := db.Scan(libDir, nil); err != nil {
		t.Fatal(err)
	}

	books, err := db.List(SortTitle, false, 1, 10, Filter{})
	if err != nil {
		t.Fatal(err)
	}
	if len(books) != 2 {
		t.Fatalf("setup: expected 2 books, got %d", len(books))
	}

	shelfID, err := db.EnsureSystemShelf("alice", "favourites", "Favourites")
	if err != nil {
		t.Fatal(err)
	}
	if err := db.AddBookToShelf(shelfID, books[0].ID); err != nil {
		t.Fatal(err)
	}

	favorited, err := db.List(SortTitle, false, 1, 10, Filter{ShelfID: shelfID})
	if err != nil {
		t.Fatal(err)
	}
	if len(favorited) != 1 || favorited[0].ID != books[0].ID {
		t.Errorf("Filter{ShelfID} = %+v, want just book %d", favorited, books[0].ID)
	}

	count, err := db.Count(Filter{ShelfID: shelfID})
	if err != nil {
		t.Fatal(err)
	}
	if count != 1 {
		t.Errorf("Count(Filter{ShelfID}) = %d, want 1", count)
	}
}
