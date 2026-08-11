// Package chaptarr is a minimal read-only client for a self-hosted Chaptarr
// instance (https://github.com/Chaptarr/chaptarr, a Readarr fork for
// audiobook/ebook libraries). Chaptarr has no independently documented REST
// API; the shapes decoded here (GET /api/v1/book, /api/v1/author,
// /api/v1/bookfile, all X-Api-Key-authenticated) were verified directly
// against a live Chaptarr instance during development — see bookDocument,
// authorDocument, and bookFileDocument's doc comments for what's actually
// confirmed vs. inferred.
package chaptarr

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"regexp"
	"strings"
	"sync"
	"time"
)

// Client is safe for concurrent use. baseURL/apiKey/enabled are mutable
// (see SetConfig) so the admin Integrations page can turn Chaptarr on/off
// or change its connection details without a server restart, the same
// shape as internal/hardcover.Client.
type Client struct {
	http *http.Client

	mu      sync.Mutex
	baseURL string
	apiKey  string
	enabled bool
}

// New creates a Client using the given base URL (e.g.
// "http://chaptarr.local:8978", no trailing slash required), API key, and
// enabled state, as loaded from users.IntegrationSettings at startup. Use
// SetConfig to update any of these at runtime.
func New(enabled bool, baseURL, apiKey string) *Client {
	return &Client{
		baseURL: strings.TrimRight(baseURL, "/"),
		apiKey:  apiKey,
		enabled: enabled,
		http:    &http.Client{Timeout: 30 * time.Second},
	}
}

// SetConfig updates the client's enabled state, base URL, and API key in
// place — called after the admin Integrations page saves a change.
func (c *Client) SetConfig(enabled bool, baseURL, apiKey string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.enabled = enabled
	c.baseURL = strings.TrimRight(baseURL, "/")
	c.apiKey = apiKey
}

// Enabled reports whether Chaptarr is turned on and has both a base URL and
// API key configured — callers should skip matching entirely (not error)
// when this is false, since the integration is optional.
func (c *Client) Enabled() bool {
	if c == nil {
		return false
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.enabled && c.baseURL != "" && c.apiKey != ""
}

func (c *Client) config() (baseURL, apiKey string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.baseURL, c.apiKey
}

// Book is a single title Chaptarr is tracking with an on-disk file, with
// just the fields this app needs for path-based matching and metadata
// fill-in. Only books Chaptarr reports as having a file are ever returned
// by ListBooks — one with no file can't be path-matched anyway.
type Book struct {
	ID          int
	Title       string
	Authors     []string
	Series      string
	SeriesIndex float64
	Genres      []string
	Rating      float64 // Chaptarr's ratings.value, e.g. sourced from Hardcover/Goodreads
	// Paths are every on-disk file path Chaptarr reports for this book
	// (one per format/edition it's tracking).
	Paths []string
}

// bookDocument mirrors the fields GET /api/v1/book actually returns, as
// observed against a live instance. Notably: no per-book file path (see
// bookFileDocument, a separate endpoint) and no non-empty "overview"
// description in practice (the field exists but was empty on every book
// checked, likely populated only when Chaptarr has separately fetched
// jacket copy) — Book has no Description field as a result; Genres is the
// richer signal this integration actually contributes.
type bookDocument struct {
	ID          int      `json:"id"`
	Title       string   `json:"title"`
	AuthorID    int      `json:"authorId"`
	SeriesTitle string   `json:"seriesTitle"` // e.g. "Mistborn #1" — see parseSeriesTitle
	Genres      []string `json:"genres"`
	HasFiles    bool     `json:"hasFiles"`
	Ratings     struct {
		Value float64 `json:"value"`
	} `json:"ratings"`
}

// authorDocument mirrors GET /api/v1/author's per-author fields.
type authorDocument struct {
	ID         int    `json:"id"`
	AuthorName string `json:"authorName"`
}

// bookFileDocument mirrors GET /api/v1/bookfile's per-file fields. This
// endpoint requires an authorId, bookId, or bookFileIds filter — it 400s
// with no filter at all — so ListBooks calls it once per author that has
// at least one book with files, not once globally.
type bookFileDocument struct {
	BookID int    `json:"bookId"`
	Path   string `json:"path"`
}

// seriesTitleRE splits Chaptarr's combined "<series name> #<index>" field
// (confirmed format, including fractional indices like "#6.5" for
// novellas/interludes) back into name and index.
var seriesTitleRE = regexp.MustCompile(`^(.*)\s+#([0-9]+(?:\.[0-9]+)?)$`)

func parseSeriesTitle(s string) (name string, index float64) {
	m := seriesTitleRE.FindStringSubmatch(strings.TrimSpace(s))
	if m == nil {
		return strings.TrimSpace(s), 0
	}
	var idx float64
	fmt.Sscanf(m[2], "%f", &idx)
	return strings.TrimSpace(m[1]), idx
}

func (c *Client) get(ctx context.Context, path string, out any) error {
	baseURL, apiKey := c.config()
	if baseURL == "" {
		return fmt.Errorf("chaptarr: no base URL configured")
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, baseURL+path, nil)
	if err != nil {
		return fmt.Errorf("chaptarr: build request: %w", err)
	}
	req.Header.Set("X-Api-Key", apiKey)
	req.Header.Set("Accept", "application/json")

	resp, err := c.http.Do(req)
	if err != nil {
		return fmt.Errorf("chaptarr: request failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("chaptarr: unexpected status %d for %s", resp.StatusCode, path)
	}
	if err := json.NewDecoder(resp.Body).Decode(out); err != nil {
		return fmt.Errorf("chaptarr: decode response for %s: %w", path, err)
	}
	return nil
}

// ListBooks fetches every book Chaptarr has an on-disk file for, with
// author names resolved and file paths attached. This is a handful of
// requests, not one per book: one for the full book list, one for the full
// author list (build once, id->name lookup), then one GET /api/v1/bookfile
// call per distinct author that has at least one book with files (that
// endpoint requires an authorId/bookId filter, so it can't be fetched in a
// single global call).
func (c *Client) ListBooks(ctx context.Context) ([]Book, error) {
	var authorDocs []authorDocument
	if err := c.get(ctx, "/api/v1/author", &authorDocs); err != nil {
		return nil, err
	}
	authorNames := make(map[int]string, len(authorDocs))
	for _, a := range authorDocs {
		authorNames[a.ID] = a.AuthorName
	}

	var bookDocs []bookDocument
	if err := c.get(ctx, "/api/v1/book", &bookDocs); err != nil {
		return nil, err
	}

	withFiles := bookDocs[:0:0]
	authorIDs := map[int]bool{}
	for _, d := range bookDocs {
		if !d.HasFiles {
			continue
		}
		withFiles = append(withFiles, d)
		authorIDs[d.AuthorID] = true
	}

	pathsByBookID := map[int][]string{}
	for authorID := range authorIDs {
		var files []bookFileDocument
		if err := c.get(ctx, fmt.Sprintf("/api/v1/bookfile?authorId=%d", authorID), &files); err != nil {
			return nil, err
		}
		for _, f := range files {
			if f.Path != "" {
				pathsByBookID[f.BookID] = append(pathsByBookID[f.BookID], f.Path)
			}
		}
	}

	books := make([]Book, 0, len(withFiles))
	for _, d := range withFiles {
		paths := pathsByBookID[d.ID]
		if len(paths) == 0 {
			// hasFiles said yes but the per-author bookfile fetch didn't
			// turn up a path for this specific book — nothing to match on.
			continue
		}
		series, seriesIndex := parseSeriesTitle(d.SeriesTitle)
		b := Book{
			ID:          d.ID,
			Title:       d.Title,
			Series:      series,
			SeriesIndex: seriesIndex,
			Genres:      d.Genres,
			Rating:      d.Ratings.Value,
			Paths:       paths,
		}
		if name := authorNames[d.AuthorID]; name != "" {
			b.Authors = []string{name}
		}
		books = append(books, b)
	}
	return books, nil
}
