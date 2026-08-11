package chaptarr

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestClient_Enabled(t *testing.T) {
	if (&Client{}).Enabled() {
		t.Error("expected a zero-value client to be disabled")
	}
	if !New(true, "http://localhost:8978", "a-key").Enabled() {
		t.Error("expected an enabled client with URL+key to be enabled")
	}
	if New(true, "", "a-key").Enabled() {
		t.Error("expected a client with no base URL to be disabled")
	}
	if New(true, "http://localhost:8978", "").Enabled() {
		t.Error("expected a client with no API key to be disabled")
	}
	if New(false, "http://localhost:8978", "a-key").Enabled() {
		t.Error("expected enabled=false to be disabled regardless of URL/key")
	}
	var nilClient *Client
	if nilClient.Enabled() {
		t.Error("expected a nil client to be disabled")
	}
}

func TestClient_SetConfigTakesEffectImmediately(t *testing.T) {
	c := New(false, "", "")
	if c.Enabled() {
		t.Fatal("expected a freshly-created disabled client to be disabled")
	}
	c.SetConfig(true, "http://localhost:8978", "a-key")
	if !c.Enabled() {
		t.Error("expected SetConfig to enable the client")
	}
}

// TestListBooks_ against a stub server modeled on a real Chaptarr
// instance's response shapes, verified live during development (see
// client.go's package doc comment): GET /api/v1/author (id->authorName),
// GET /api/v1/book (title/authorId/seriesTitle "Name #Index"/genres/
// ratings.value/hasFiles/id, no path), GET /api/v1/bookfile?authorId=N
// (bookId->path, 400s with no filter at all).
func newStubChaptarrServer(t *testing.T) *httptest.Server {
	t.Helper()
	mux := http.NewServeMux()
	mux.HandleFunc("/api/v1/author", func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(`[{"id": 1, "authorName": "Brandon Sanderson"}]`))
	})
	mux.HandleFunc("/api/v1/book", func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(`[
			{"id": 1, "title": "Mistborn", "authorId": 1, "seriesTitle": "The Mistborn Saga #1", "genres": ["Fantasy"], "ratings": {"value": 4.5}, "hasFiles": true, "hardcoverBookId": "hc:123456"},
			{"id": 2, "title": "No File Yet", "authorId": 1, "seriesTitle": "The Mistborn Saga #2", "genres": ["Fantasy"], "hasFiles": false}
		]`))
	})
	mux.HandleFunc("/api/v1/bookfile", func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Query().Get("authorId") == "" {
			w.WriteHeader(http.StatusBadRequest)
			w.Write([]byte(`{"message": "authorId, bookId, bookFileIds or unmapped must be provided"}`))
			return
		}
		w.Write([]byte(`[{"bookId": 1, "path": "/library/Brandon Sanderson/Mistborn/Mistborn.epub"}]`))
	})
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	return srv
}

func TestListBooks_ParsesSeriesTitleAuthorAndPaths(t *testing.T) {
	srv := newStubChaptarrServer(t)

	c := New(true, srv.URL, "test-key")
	books, err := c.ListBooks(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	// Only the hasFiles=true, path-resolved book should be returned — book
	// id 2 has no file yet and can't be path-matched.
	if len(books) != 1 {
		t.Fatalf("got %d books, want 1 (the one with a resolvable file path): %+v", len(books), books)
	}
	b := books[0]
	if b.Title != "Mistborn" || len(b.Authors) != 1 || b.Authors[0] != "Brandon Sanderson" {
		t.Errorf("book = %+v, unexpected title/author", b)
	}
	if b.Series != "The Mistborn Saga" || b.SeriesIndex != 1 {
		t.Errorf("book = %+v, want seriesTitle \"The Mistborn Saga #1\" parsed into name+index", b)
	}
	if b.Rating != 4.5 {
		t.Errorf("Rating = %v, want 4.5", b.Rating)
	}
	if len(b.Paths) != 1 || b.Paths[0] != "/library/Brandon Sanderson/Mistborn/Mistborn.epub" {
		t.Errorf("Paths = %v, unexpected", b.Paths)
	}
	if b.HardcoverID != "123456" {
		t.Errorf("HardcoverID = %q, want %q from hardcoverBookId \"hc:123456\"", b.HardcoverID, "123456")
	}
}

func TestParseHardcoverID(t *testing.T) {
	tests := []struct{ in, want string }{
		{"hc:1686204", "1686204"},
		{"HC:1686204", "1686204"},
		{"gr:123334051-ebook", ""},
		{"", ""},
		{"hc:", ""},
	}
	for _, tt := range tests {
		if got := parseHardcoverID(tt.in); got != tt.want {
			t.Errorf("parseHardcoverID(%q) = %q, want %q", tt.in, got, tt.want)
		}
	}
}

func TestListBooks_SendsAPIKeyOnEveryRequest(t *testing.T) {
	var gotKeys []string
	mux := http.NewServeMux()
	mux.HandleFunc("/api/v1/author", func(w http.ResponseWriter, r *http.Request) {
		gotKeys = append(gotKeys, r.Header.Get("X-Api-Key"))
		w.Write([]byte(`[{"id": 1, "authorName": "Brandon Sanderson"}]`))
	})
	mux.HandleFunc("/api/v1/book", func(w http.ResponseWriter, r *http.Request) {
		gotKeys = append(gotKeys, r.Header.Get("X-Api-Key"))
		w.Write([]byte(`[{"id": 1, "title": "Mistborn", "authorId": 1, "hasFiles": true}]`))
	})
	mux.HandleFunc("/api/v1/bookfile", func(w http.ResponseWriter, r *http.Request) {
		gotKeys = append(gotKeys, r.Header.Get("X-Api-Key"))
		w.Write([]byte(`[{"bookId": 1, "path": "/library/Mistborn.epub"}]`))
	})
	srv := httptest.NewServer(mux)
	defer srv.Close()

	c := New(true, srv.URL, "test-key")
	if _, err := c.ListBooks(context.Background()); err != nil {
		t.Fatal(err)
	}
	if len(gotKeys) != 3 {
		t.Fatalf("got %d requests, want 3 (author, book, bookfile)", len(gotKeys))
	}
	for _, k := range gotKeys {
		if k != "test-key" {
			t.Errorf("X-Api-Key = %q, want %q on every request", k, "test-key")
		}
	}
}

func TestListBooks_NoBaseURL(t *testing.T) {
	c := New(true, "", "test-key")
	if _, err := c.ListBooks(context.Background()); err == nil {
		t.Error("expected an error with no base URL configured")
	}
}

func TestParseSeriesTitle(t *testing.T) {
	tests := []struct {
		in        string
		wantName  string
		wantIndex float64
	}{
		{"Mistborn #1", "Mistborn", 1},
		{"Blood and Ash #6.5", "Blood and Ash", 6.5},
		{"The Hitchhiker’s Guide to the Galaxy #2", "The Hitchhiker’s Guide to the Galaxy", 2},
		{"No Index Here", "No Index Here", 0},
		{"", "", 0},
	}
	for _, tt := range tests {
		name, index := parseSeriesTitle(tt.in)
		if name != tt.wantName || index != tt.wantIndex {
			t.Errorf("parseSeriesTitle(%q) = (%q, %v), want (%q, %v)", tt.in, name, index, tt.wantName, tt.wantIndex)
		}
	}
}
