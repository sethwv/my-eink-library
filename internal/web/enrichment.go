package web

import (
	"context"
	"fmt"
	"io"
	"log"
	"net"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/swvn/eink-library/internal/hardcover"
	"github.com/swvn/eink-library/internal/index"
)

// idlePollInterval is how long the background enrichment queue waits before
// checking again when there's currently nothing to do (or the last check
// failed) — deliberately coarse, since new candidates only appear when the
// library is rescanned.
const idlePollInterval = 30 * time.Second

// RunEnrichmentQueue processes one book at a time against Hardcover,
// resuming from wherever it left off (progress is tracked in the books
// table, not in memory) so a server restart doesn't reprocess already-
// `done`/`no_match`/`error` books. A no-op if no Hardcover token was
// configured. The client's own rate limiter spaces out the actual HTTP
// calls, so this loop doesn't need its own throttling beyond an idle
// backoff when there's nothing to do.
func (s *Server) RunEnrichmentQueue(ctx context.Context) {
	if !s.Hardcover.Enabled() {
		return
	}
	for {
		select {
		case <-ctx.Done():
			return
		default:
		}

		candidates, err := s.DB.BooksNeedingEnrichment(1)
		if err != nil {
			log.Printf("enrichment queue: list candidates: %v", err)
			sleepOrDone(ctx, idlePollInterval)
			continue
		}
		if len(candidates) == 0 {
			sleepOrDone(ctx, idlePollInterval)
			continue
		}

		c := candidates[0]
		matches, err := s.Hardcover.Search(ctx, c.Title, c.Author, c.Identifier)
		if err != nil {
			log.Printf("enrichment queue: search failed for book %d: %v", c.ID, err)
			if err := s.DB.SetEnrichmentStatus(c.ID, "error"); err != nil {
				log.Printf("enrichment queue: mark error failed for book %d: %v", c.ID, err)
			}
			continue
		}
		best, ok := hardcover.BestConfidentMatch(matches, c.Author)
		if !ok {
			if err := s.DB.SetEnrichmentStatus(c.ID, "no_match"); err != nil {
				log.Printf("enrichment queue: mark no_match failed for book %d: %v", c.ID, err)
			}
			continue
		}

		// Publisher and cover image aren't in the search document (see
		// hardcover.Match), so they need one more request — best-effort: a
		// failure here shouldn't stop the rest of the match (series/date/
		// title/etc.) from applying.
		var detail hardcover.Detail
		if d, err := s.Hardcover.Detail(ctx, best.ID); err != nil {
			log.Printf("enrichment queue: detail lookup failed for book %d: %v", c.ID, err)
		} else {
			detail = d
		}

		fields := index.HardcoverFields{
			Title:         best.Title,
			Series:        best.Series,
			SeriesIndex:   best.SeriesIndex,
			PublishedDate: best.ReleaseDate,
			Description:   best.Description,
			Genres:        best.Genres,
			Publisher:     detail.Publisher,
			Pages:         best.Pages,
			ISBN:          firstISBN(best.ISBNs),
			Rating:        best.Rating,
		}
		if err := s.DB.ApplyEnrichment(c.ID, fields); err != nil {
			log.Printf("enrichment queue: apply enrichment failed for book %d: %v", c.ID, err)
			s.DB.SetEnrichmentStatus(c.ID, "error")
			continue
		}

		// Only ever auto-apply a cover when the book doesn't have one —
		// never silently replace an existing cover. Manual override (any
		// time) lives in the Edit Metadata page.
		if detail.Image != "" {
			if book, err := s.DB.Get(c.ID); err == nil && book != nil && !book.HasCover {
				if err := s.applyCoverFromURL(ctx, c.ID, detail.Image); err != nil {
					log.Printf("enrichment queue: cover fetch failed for book %d: %v", c.ID, err)
				}
			}
		}

		if err := s.DB.SetEnrichmentStatus(c.ID, "done"); err != nil {
			log.Printf("enrichment queue: mark done failed for book %d: %v", c.ID, err)
		}
	}
}

// firstISBN prefers an ISBN-13 (13 digits) when present, otherwise the first
// ISBN Hardcover returned.
func firstISBN(isbns []string) string {
	for _, i := range isbns {
		if len(strings.ReplaceAll(i, "-", "")) == 13 {
			return i
		}
	}
	if len(isbns) > 0 {
		return isbns[0]
	}
	return ""
}

func sleepOrDone(ctx context.Context, d time.Duration) {
	select {
	case <-ctx.Done():
	case <-time.After(d):
	}
}

// maxCoverBytes caps how much of a remote cover image response is read, so
// a misbehaving or malicious URL can't exhaust memory.
const maxCoverBytes = 20 << 20 // 20MB

// applyCoverFromURL downloads the image at url, resizes/caches it through
// the same thumbnail store the EPUB scanner uses, and points the book's
// cover_path at it.
// coverFetchClient rejects redirects outright rather than following them —
// validateCoverURL only checks the URL the caller gave us, so a redirect
// (e.g. to an internal address) would otherwise bypass that check entirely.
var coverFetchClient = &http.Client{
	Timeout: 15 * time.Second,
	CheckRedirect: func(req *http.Request, via []*http.Request) error {
		return http.ErrUseLastResponse
	},
}

// applyCoverFromURL downloads the image at rawURL, resizes/caches it through
// the same thumbnail store the EPUB scanner uses, and points the book's
// cover_path at it. rawURL ultimately comes from a value round-tripped
// through the Edit Metadata form's hidden field, so it's untrusted input —
// validateCoverURL guards against it being used to make the server fetch an
// internal/private address (SSRF) even though only admins can reach this
// path.
func (s *Server) applyCoverFromURL(ctx context.Context, bookID int64, rawURL string) error {
	if err := validateCoverURL(rawURL); err != nil {
		return fmt.Errorf("refusing to fetch cover url: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, rawURL, nil)
	if err != nil {
		return err
	}
	resp, err := coverFetchClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return &httpStatusError{resp.StatusCode}
	}

	data, err := io.ReadAll(io.LimitReader(resp.Body, maxCoverBytes))
	if err != nil {
		return err
	}

	path, err := s.Covers.SaveCover(bookID, data, resp.Header.Get("Content-Type"))
	if err != nil {
		return err
	}
	return s.DB.SetCover(bookID, path)
}

// validateCoverURL requires an https URL whose host resolves only to public
// (non-loopback, non-link-local, non-private-range) addresses, so a
// tampered hardcover_cover_url form value can't be used to make the server
// fetch an internal service, cloud metadata endpoint, or localhost port.
func validateCoverURL(rawURL string) error {
	u, err := url.Parse(rawURL)
	if err != nil {
		return err
	}
	if u.Scheme != "https" {
		return fmt.Errorf("scheme must be https, got %q", u.Scheme)
	}
	host := u.Hostname()
	if host == "" {
		return fmt.Errorf("missing host")
	}

	addrs, err := net.LookupIP(host)
	if err != nil {
		return fmt.Errorf("resolve host: %w", err)
	}
	if len(addrs) == 0 {
		return fmt.Errorf("host %q did not resolve to any address", host)
	}
	for _, ip := range addrs {
		if !ip.IsGlobalUnicast() || ip.IsPrivate() || ip.IsLoopback() || ip.IsLinkLocalUnicast() || ip.IsLinkLocalMulticast() {
			return fmt.Errorf("host %q resolves to a non-public address (%s)", host, ip)
		}
	}
	return nil
}

type httpStatusError struct{ code int }

func (e *httpStatusError) Error() string {
	return "unexpected status " + strconv.Itoa(e.code)
}

// hcCandidate is one Hardcover search hit shown in the Edit Metadata page's
// picker, plus whether it's the currently-selected one (its fields are
// being used as the edit form's defaults).
type hcCandidate struct {
	ID          string
	Title       string
	Authors     string
	Series      string
	SeriesIndex string
	ReleaseDate string
	Snippet     string
	Selected    bool
}

// BookEditMetadata shows every enrichable/editable field for a book,
// pre-filled with its current merged (book_enrichment-over-books) values,
// and — driven entirely by query params so no JS is required — can run a
// Hardcover search (either an automatic "Check Hardcover" search using the
// book's own title/author, or a manual free-text search) and let the admin
// pick one of up to 5 candidates to use as new defaults before saving.
// Nothing from a search is written to the database until Save is submitted.
func (s *Server) BookEditMetadata(w http.ResponseWriter, r *http.Request) {
	base, _, err := s.baseData(r)
	if err != nil {
		http.Error(w, "failed to load page", http.StatusInternalServerError)
		return
	}
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		http.NotFound(w, r)
		return
	}
	book, err := s.DB.Get(id)
	if err != nil {
		http.Error(w, "failed to load book", http.StatusInternalServerError)
		return
	}
	if book == nil {
		http.NotFound(w, r)
		return
	}

	mode := r.URL.Query().Get("mode") // "", "check", or "manual"
	manualQuery := r.URL.Query().Get("q")
	selectedID := r.URL.Query().Get("selected")

	data := map[string]any{
		"Title":         "Edit Metadata",
		"BookID":        id,
		"CurrentTitle":  book.Title,
		"Enabled":       s.Hardcover.Enabled(),
		"Mode":          mode,
		"ManualQuery":   manualQuery,
		"FormTitle":     book.Title,
		"FormSeries":    book.Series,
		"FormPublished": book.PublishedAt,
		"FormDesc":      book.Description,
		"FormGenres":    strings.Join(book.Genres, ", "),
		"FormPublisher": book.Publisher,
		"FormPages":     formatIntOrBlank(book.Pages),
		"FormISBN":      book.ISBN,
		"FormRating":    formatFloatOrBlank(book.Rating),
		"CoverURL":      "/covers/" + strconv.FormatInt(id, 10),
	}
	if book.SeriesIndex != 0 {
		data["FormSeriesIndex"] = strconv.FormatFloat(book.SeriesIndex, 'f', -1, 64)
	}

	if mode != "" && s.Hardcover.Enabled() {
		var matches []hardcover.Match
		var searchErr error
		if mode == "check" {
			matches, searchErr = s.Hardcover.Search(r.Context(), book.Title, book.Author, book.Identifier)
		} else {
			matches, searchErr = s.Hardcover.Search(r.Context(), manualQuery, "", "")
		}
		if searchErr != nil {
			log.Printf("edit-metadata search failed for book %d: %v", id, searchErr)
			data["SearchError"] = "Hardcover search failed."
		}

		candidates := make([]hcCandidate, 0, len(matches))
		var selected *hardcover.Match
		for i := range matches {
			m := &matches[i]
			c := hcCandidate{
				ID:          m.ID,
				Title:       m.Title,
				Authors:     strings.Join(m.Authors, ", "),
				Series:      m.Series,
				ReleaseDate: m.ReleaseDate,
				Snippet:     snippet(m.Description, 160),
			}
			if m.SeriesIndex != 0 {
				c.SeriesIndex = strconv.FormatFloat(m.SeriesIndex, 'f', -1, 64)
			}
			if selectedID != "" && m.ID == selectedID {
				c.Selected = true
				selected = m
			}
			candidates = append(candidates, c)
		}
		data["Candidates"] = candidates

		if selected != nil {
			var detail hardcover.Detail
			if d, err := s.Hardcover.Detail(r.Context(), selected.ID); err != nil {
				log.Printf("edit-metadata detail lookup failed for book %d: %v", id, err)
			} else {
				detail = d
			}
			applySelectedDefaults(data, selected, detail)
		}
	}

	mergeInto(data, base)
	render(w, "book_edit_metadata.html", data)
}

// applySelectedDefaults overwrites the edit form's default values with the
// selected Hardcover candidate's fields, wherever the candidate actually has
// a value — a candidate missing e.g. page count shouldn't blank out the
// book's existing page count in the form.
func applySelectedDefaults(data map[string]any, m *hardcover.Match, d hardcover.Detail) {
	data["FormTitle"] = m.Title
	if m.Series != "" {
		data["FormSeries"] = m.Series
		if m.SeriesIndex != 0 {
			data["FormSeriesIndex"] = strconv.FormatFloat(m.SeriesIndex, 'f', -1, 64)
		}
	}
	if m.ReleaseDate != "" {
		data["FormPublished"] = m.ReleaseDate
	}
	if m.Description != "" {
		data["FormDesc"] = m.Description
	}
	if len(m.Genres) > 0 {
		data["FormGenres"] = strings.Join(m.Genres, ", ")
	}
	if d.Publisher != "" {
		data["FormPublisher"] = d.Publisher
	}
	if m.Pages != 0 {
		data["FormPages"] = strconv.Itoa(m.Pages)
	}
	if isbn := firstISBN(m.ISBNs); isbn != "" {
		data["FormISBN"] = isbn
	}
	if m.Rating != 0 {
		data["FormRating"] = strconv.FormatFloat(m.Rating, 'f', -1, 64)
	}
	if d.Image != "" {
		data["SelectedCoverURL"] = d.Image
	}
}

// snippet trims s to at most n runes, appending an ellipsis if it was
// longer, for the candidate picker's description preview.
func snippet(s string, n int) string {
	r := []rune(strings.TrimSpace(s))
	if len(r) <= n {
		return string(r)
	}
	return string(r[:n]) + "…"
}

func formatIntOrBlank(n int) string {
	if n == 0 {
		return ""
	}
	return strconv.Itoa(n)
}

func formatFloatOrBlank(f float64) string {
	if f == 0 {
		return ""
	}
	return strconv.FormatFloat(f, 'f', -1, 64)
}

// BookEditMetadataSave writes every field the form submitted (blank means
// "leave unchanged", same convention as the rest of the enrichment system)
// and, if the admin picked a Hardcover cover image, downloads and applies it
// — this manual path allows overriding an existing cover, unlike the
// background queue's auto-fill which only ever fills a missing one.
func (s *Server) BookEditMetadataSave(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		http.NotFound(w, r)
		return
	}
	if err := r.ParseForm(); err != nil {
		http.Error(w, "bad form", http.StatusBadRequest)
		return
	}

	fields := index.MetadataFields{
		Title:         r.FormValue("title"),
		Series:        r.FormValue("series"),
		PublishedDate: r.FormValue("published_date"),
		Description:   r.FormValue("description"),
		Publisher:     r.FormValue("publisher"),
		ISBN:          r.FormValue("isbn"),
	}
	if v := r.FormValue("series_index"); v != "" {
		fields.SeriesIndex, _ = strconv.ParseFloat(v, 64)
	}
	if v := r.FormValue("pages"); v != "" {
		if n, err := strconv.Atoi(v); err == nil {
			fields.Pages = n
		}
	}
	if v := r.FormValue("rating"); v != "" {
		fields.Rating, _ = strconv.ParseFloat(v, 64)
	}
	if v := strings.TrimSpace(r.FormValue("genres")); v != "" {
		var genres []string
		for _, g := range strings.Split(v, ",") {
			if g = strings.TrimSpace(g); g != "" {
				genres = append(genres, g)
			}
		}
		fields.Genres = genres
	}

	if err := s.DB.SaveMetadata(id, fields); err != nil {
		http.Error(w, "failed to save changes", http.StatusInternalServerError)
		return
	}
	if err := s.DB.SetEnrichmentStatus(id, "done"); err != nil {
		log.Printf("edit-metadata save: mark done failed for book %d: %v", id, err)
	}

	if r.FormValue("cover_action") == "use_hardcover_cover" {
		if coverURL := r.FormValue("hardcover_cover_url"); coverURL != "" {
			if err := s.applyCoverFromURL(r.Context(), id, coverURL); err != nil {
				log.Printf("edit-metadata save: cover apply failed for book %d: %v", id, err)
			}
		}
	}

	http.Redirect(w, r, "/books/"+strconv.FormatInt(id, 10)+"/edit-metadata", http.StatusSeeOther)
}
