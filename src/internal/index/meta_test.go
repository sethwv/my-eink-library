package index

import "testing"

func TestSetGetMeta(t *testing.T) {
	db := openTestDB(t)

	_, ok, err := db.GetMeta("nonexistent")
	if err != nil {
		t.Fatal(err)
	}
	if ok {
		t.Error("expected ok=false for missing key")
	}

	if err := db.SetMeta("foo", "bar"); err != nil {
		t.Fatal(err)
	}
	value, ok, err := db.GetMeta("foo")
	if err != nil {
		t.Fatal(err)
	}
	if !ok || value != "bar" {
		t.Errorf("GetMeta(foo) = %q, %v, want bar, true", value, ok)
	}

	// SetMeta upserts.
	if err := db.SetMeta("foo", "baz"); err != nil {
		t.Fatal(err)
	}
	value, ok, err = db.GetMeta("foo")
	if err != nil {
		t.Fatal(err)
	}
	if !ok || value != "baz" {
		t.Errorf("GetMeta(foo) after update = %q, %v, want baz, true", value, ok)
	}
}

func TestScan_RecordsLastScanMeta(t *testing.T) {
	libDir := t.TempDir()
	writeTestEpub(t, libDir+"/book1.epub", "Book One", "Author A")

	db := openTestDB(t)
	if err := db.Scan([]string{libDir}, nil); err != nil {
		t.Fatal(err)
	}

	at, ok, err := db.GetMeta("last_scan_at")
	if err != nil {
		t.Fatal(err)
	}
	if !ok || at == "" {
		t.Error("expected last_scan_at to be set after Scan")
	}

	durMs, ok, err := db.GetMeta("last_scan_duration_ms")
	if err != nil {
		t.Fatal(err)
	}
	if !ok {
		t.Error("expected last_scan_duration_ms to be set after Scan")
	}
	_ = durMs
}
