package index

import (
	"path/filepath"
	"testing"
)

func TestBooksNeedingEnrichment_OnlyMissingFieldsUnprocessed(t *testing.T) {
	libDir := t.TempDir()
	writeTestEpub(t, filepath.Join(libDir, "b1.epub"), "No Series Or Date", "Amy Zed")
	writeTestEpubWithSeries(t, filepath.Join(libDir, "b2.epub"), "Has Series Only", "Amy Zed", "A Saga", 1)
	writeTestEpubWithDate(t, filepath.Join(libDir, "b3.epub"), "Has Date Only", "Amy Zed", "2020-01-01")

	db := openTestDB(t)
	if err := db.Scan(libDir, nil); err != nil {
		t.Fatal(err)
	}

	// All three are candidates: each is missing at least one of
	// series/published_date (auto-fill's "either field blank" test), even
	// though each has the *other* field already filled.
	candidates, err := db.BooksNeedingEnrichment(10)
	if err != nil {
		t.Fatal(err)
	}
	if len(candidates) != 3 {
		t.Fatalf("got %d candidates, want 3 (each missing at least one field): %+v", len(candidates), candidates)
	}

	if err := db.SetEnrichmentStatus(candidates[0].ID, "done"); err != nil {
		t.Fatal(err)
	}
	candidates, err = db.BooksNeedingEnrichment(10)
	if err != nil {
		t.Fatal(err)
	}
	if len(candidates) != 2 {
		t.Errorf("got %d candidates after marking one done, want 2", len(candidates))
	}
}

func TestGetEnrichmentStats(t *testing.T) {
	libDir := t.TempDir()
	writeTestEpub(t, filepath.Join(libDir, "b1.epub"), "Book One", "Amy Zed")
	writeTestEpub(t, filepath.Join(libDir, "b2.epub"), "Book Two", "Amy Zed")
	writeTestEpub(t, filepath.Join(libDir, "b3.epub"), "Book Three", "Amy Zed")

	db := openTestDB(t)
	if err := db.Scan(libDir, nil); err != nil {
		t.Fatal(err)
	}

	candidates, err := db.BooksNeedingEnrichment(10)
	if err != nil {
		t.Fatal(err)
	}
	if len(candidates) != 3 {
		t.Fatalf("got %d candidates, want 3", len(candidates))
	}
	if err := db.SetEnrichmentStatus(candidates[0].ID, "done"); err != nil {
		t.Fatal(err)
	}
	if err := db.SetEnrichmentStatus(candidates[1].ID, "no_match"); err != nil {
		t.Fatal(err)
	}
	if err := db.SetEnrichmentStatus(candidates[2].ID, "error"); err != nil {
		t.Fatal(err)
	}

	stats, err := db.GetEnrichmentStats()
	if err != nil {
		t.Fatal(err)
	}
	if stats.Pending != 0 || stats.Done != 1 || stats.NoMatch != 1 || stats.Errored != 1 {
		t.Errorf("stats = %+v, want {Pending:0 Done:1 NoMatch:1 Errored:1}", stats)
	}
}

func TestFillBlankMetadata_OnlyFillsBlanks(t *testing.T) {
	libDir := t.TempDir()
	writeTestEpubWithSeries(t, filepath.Join(libDir, "b1.epub"), "Has Series Already", "Amy Zed", "Existing Saga", 2)

	db := openTestDB(t)
	if err := db.Scan(libDir, nil); err != nil {
		t.Fatal(err)
	}
	books, err := db.List(SortTitle, false, 1, 10, Filter{})
	if err != nil {
		t.Fatal(err)
	}
	id := books[0].ID

	if err := db.FillBlankMetadata(id, "New Series", 9, "2021-06-01"); err != nil {
		t.Fatal(err)
	}

	got, err := db.Get(id)
	if err != nil {
		t.Fatal(err)
	}
	if got.Series != "Existing Saga" || got.SeriesIndex != 2 {
		t.Errorf("expected existing series to be left alone, got Series=%q SeriesIndex=%v", got.Series, got.SeriesIndex)
	}
	if got.PublishedAt != "2021-06-01" {
		t.Errorf("PublishedAt = %q, want the fill-in value since it was blank", got.PublishedAt)
	}
}

func TestOverrideMetadata_OverwritesExisting(t *testing.T) {
	libDir := t.TempDir()
	writeTestEpubWithSeries(t, filepath.Join(libDir, "b1.epub"), "Old Title", "Amy Zed", "Old Saga", 1)

	db := openTestDB(t)
	if err := db.Scan(libDir, nil); err != nil {
		t.Fatal(err)
	}
	books, err := db.List(SortTitle, false, 1, 10, Filter{})
	if err != nil {
		t.Fatal(err)
	}
	id := books[0].ID

	if err := db.OverrideMetadata(id, "New Title", "New Saga", 5, "2022-03-01"); err != nil {
		t.Fatal(err)
	}

	got, err := db.Get(id)
	if err != nil {
		t.Fatal(err)
	}
	if got.Title != "New Title" || got.Series != "New Saga" || got.SeriesIndex != 5 || got.PublishedAt != "2022-03-01" {
		t.Errorf("got %+v, want overridden fields to all be applied", got)
	}
}

func TestOverrideMetadata_BlankFieldsLeftAlone(t *testing.T) {
	libDir := t.TempDir()
	writeTestEpubWithSeries(t, filepath.Join(libDir, "b1.epub"), "Keep This Title", "Amy Zed", "Keep Saga", 1)

	db := openTestDB(t)
	if err := db.Scan(libDir, nil); err != nil {
		t.Fatal(err)
	}
	books, err := db.List(SortTitle, false, 1, 10, Filter{})
	if err != nil {
		t.Fatal(err)
	}
	id := books[0].ID

	// Empty strings mean "the admin didn't choose to override this field".
	if err := db.OverrideMetadata(id, "", "", 0, "2022-03-01"); err != nil {
		t.Fatal(err)
	}

	got, err := db.Get(id)
	if err != nil {
		t.Fatal(err)
	}
	if got.Title != "Keep This Title" || got.Series != "Keep Saga" || got.SeriesIndex != 1 {
		t.Errorf("got %+v, want title/series/index left alone", got)
	}
	if got.PublishedAt != "2022-03-01" {
		t.Errorf("PublishedAt = %q, want the one field that was set", got.PublishedAt)
	}
}
