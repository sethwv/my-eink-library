package hardcover

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

// overrideEndpointForTest points the package-level endpoint at a test
// server for the duration of t, restoring the real URL afterward.
func overrideEndpointForTest(t *testing.T, url string) {
	t.Helper()
	original := endpoint
	endpoint = url
	t.Cleanup(func() { endpoint = original })
}

func TestSearch_SendsBearerTokenAndVariables(t *testing.T) {
	var gotAuth string
	var gotBody map[string]any
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotAuth = r.Header.Get("Authorization")
		json.NewDecoder(r.Body).Decode(&gotBody)
		w.Write([]byte(`{"data":{"search":{"ids":[1],"results":{"hits":[{"document":{"id":"1","title":"Mistborn","author_names":["Brandon Sanderson"],"release_date":"2006-01-01","featured_series":{"position":1,"series":{"name":"The Mistborn Saga"}}}}]}}}}`))
	}))
	defer srv.Close()

	c := New("test-token")
	c.http = srv.Client()
	overrideEndpointForTest(t, srv.URL)

	matches, err := c.Search(context.Background(), "Mistborn", "Brandon Sanderson")
	if err != nil {
		t.Fatal(err)
	}
	if gotAuth != "Bearer test-token" {
		t.Errorf("Authorization = %q, want %q", gotAuth, "Bearer test-token")
	}
	variables, _ := gotBody["variables"].(map[string]any)
	if variables["q"] != "Mistborn Brandon Sanderson" {
		t.Errorf("variables[q] = %v, want %q", variables["q"], "Mistborn Brandon Sanderson")
	}
	if len(matches) != 1 {
		t.Fatalf("got %d matches, want 1", len(matches))
	}
	m := matches[0]
	if m.Title != "Mistborn" || m.Series != "The Mistborn Saga" || m.SeriesIndex != 1 || m.ReleaseDate != "2006-01-01" {
		t.Errorf("match = %+v, unexpected fields", m)
	}
}

func TestSearch_SurfacesGraphQLErrors(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(`{"errors":[{"message":"rate limited"}]}`))
	}))
	defer srv.Close()

	c := New("test-token")
	c.http = srv.Client()
	overrideEndpointForTest(t, srv.URL)

	if _, err := c.Search(context.Background(), "Mistborn", "Brandon Sanderson"); err == nil {
		t.Fatal("expected an error from a GraphQL errors response")
	} else if !strings.Contains(err.Error(), "rate limited") {
		t.Errorf("error = %v, want it to mention %q", err, "rate limited")
	}
}

func TestClient_Enabled(t *testing.T) {
	if (&Client{}).Enabled() {
		t.Error("expected a client with no token to be disabled")
	}
	if !New("a-token").Enabled() {
		t.Error("expected a client with a token to be enabled")
	}
	var nilClient *Client
	if nilClient.Enabled() {
		t.Error("expected a nil client to be disabled")
	}
}

func TestClient_ThrottlesRequests(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(`{"data":{"search":{"ids":[],"results":{"hits":[]}}}}`))
	}))
	defer srv.Close()

	c := New("test-token")
	c.http = srv.Client()
	overrideEndpointForTest(t, srv.URL)

	start := time.Now()
	if _, err := c.Search(context.Background(), "a", "b"); err != nil {
		t.Fatal(err)
	}
	if _, err := c.Search(context.Background(), "c", "d"); err != nil {
		t.Fatal(err)
	}
	if elapsed := time.Since(start); elapsed < minInterval {
		t.Errorf("two calls took %v, want at least %v (rate-limit spacing)", elapsed, minInterval)
	}
}
