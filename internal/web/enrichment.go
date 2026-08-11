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

	"github.com/swvn/eink-library/internal/chaptarr"
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
// `done`/`no_match`/`error` books. Idles (rather than returning) while
// Hardcover is disabled, so enabling it from the admin Integrations page
// after startup — no token configured, or the toggle switched off — makes
// this loop start processing without a server restart. The client's own
// rate limiter spaces out the actual HTTP calls, so this loop doesn't need
// its own throttling beyond an idle backoff when there's nothing to do.
func (s *Server) RunEnrichmentQueue(ctx context.Context) {
	for {
		select {
		case <-ctx.Done():
			return
		default:
		}

		if !s.Hardcover.Enabled() {
			sleepOrDone(ctx, idlePollInterval)
			continue
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
		best, ok := hardcover.BestConfidentMatch(matches, c.Title, c.Author)
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
		if err := s.DB.ApplyEnrichment(c.ID, fields, index.SourceHardcover); err != nil {
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

// chaptarrBatchSize is how many enrichment candidates RunChaptarrQueue pulls
// per pass — larger than RunEnrichmentQueue's one-at-a-time since matching
// is a single ListBooks fetch checked against every candidate locally, not
// one API call per book.
const chaptarrBatchSize = 200

// RunChaptarrQueue is Chaptarr's equivalent of RunEnrichmentQueue: matches
// still-pending books against Chaptarr's tracked library by file path (see
// chaptarr.MatchByPath) and, on a match, applies Chaptarr's metadata with
// source=SourceChaptarr. Chaptarr's precedence over Hardcover falls out of
// both queues sharing the same needsEnrichmentWhere status=” gate — once
// this pass marks a book "done", RunEnrichmentQueue's own candidate query
// skips it, so a book Chaptarr claims is never subsequently touched by
// Hardcover. A book Chaptarr has no path match for is left with status=”
// (not "no_match" — that status is reserved for "checked and confirmed no
// match", which this pass can't assert given only a local path heuristic)
// so it falls through to the Hardcover pass, or a future rescan/Chaptarr
// pass, unchanged.
func (s *Server) RunChaptarrQueue(ctx context.Context) {
	for {
		select {
		case <-ctx.Done():
			return
		default:
		}

		if !s.Chaptarr.Enabled() {
			sleepOrDone(ctx, idlePollInterval)
			continue
		}

		candidates, err := s.DB.BooksNeedingEnrichment(chaptarrBatchSize)
		if err != nil {
			log.Printf("chaptarr queue: list candidates: %v", err)
			sleepOrDone(ctx, idlePollInterval)
			continue
		}
		if len(candidates) == 0 {
			sleepOrDone(ctx, idlePollInterval)
			continue
		}

		chBooks, err := s.Chaptarr.ListBooks(ctx)
		if err != nil {
			log.Printf("chaptarr queue: list books: %v", err)
			sleepOrDone(ctx, idlePollInterval)
			continue
		}

		matchedAny := false
		for _, c := range candidates {
			select {
			case <-ctx.Done():
				return
			default:
			}

			match, ok := chaptarr.MatchByPath(chBooks, c.FilePath)
			if !ok {
				continue
			}
			matchedAny = true

			fields := index.HardcoverFields{
				Title:       match.Title,
				Series:      match.Series,
				SeriesIndex: match.SeriesIndex,
				Genres:      match.Genres,
				Rating:      match.Rating,
			}
			if err := s.DB.ApplyEnrichment(c.ID, fields, index.SourceChaptarr); err != nil {
				log.Printf("chaptarr queue: apply enrichment failed for book %d: %v", c.ID, err)
			}
		}

		if !matchedAny {
			sleepOrDone(ctx, idlePollInterval)
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

// matchCandidate is one search/match hit (Hardcover or Chaptarr) shown in
// the Edit Metadata page's picker, plus whether it's the currently-selected
// one (its fields are being used as the edit form's defaults). Source-
// agnostic: Hardcover's manual/auto search and Chaptarr's path match both
// render through the same template card.
type matchCandidate struct {
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
// book's own title/author, or a manual free-text search) or a Chaptarr
// path match, letting the admin pick one of the resulting candidates to use
// as new defaults before saving. Nothing from a search is written to the
// database until Save is submitted.
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
	source, err := s.DB.GetEnrichmentSource(id)
	if err != nil {
		log.Printf("edit-metadata: load source for book %d: %v", id, err)
	}

	mode := r.URL.Query().Get("mode") // "", "check", "manual", or "chaptarr"
	manualQuery := r.URL.Query().Get("q")
	selectedID := r.URL.Query().Get("selected")

	data := map[string]any{
		"Title":           "Edit Metadata",
		"BookID":          id,
		"CurrentTitle":    book.Title,
		"Source":          source,
		"Enabled":         s.Hardcover.Enabled(),
		"ChaptarrEnabled": s.Chaptarr.Enabled(),
		"Mode":            mode,
		"ManualQuery":     manualQuery,
		"FormTitle":       book.Title,
		"FormSeries":      book.Series,
		"FormPublished":   book.PublishedAt,
		"FormDesc":        book.Description,
		"FormGenres":      strings.Join(book.Genres, ", "),
		"FormPublisher":   book.Publisher,
		"FormPages":       formatIntOrBlank(book.Pages),
		"FormISBN":        book.ISBN,
		"FormRating":      formatFloatOrBlank(book.Rating),
		"CoverURL":        "/covers/" + strconv.FormatInt(id, 10),
	}
	if book.SeriesIndex != 0 {
		data["FormSeriesIndex"] = strconv.FormatFloat(book.SeriesIndex, 'f', -1, 64)
	}

	switch {
	case mode == "chaptarr" && s.Chaptarr.Enabled():
		s.runChaptarrSearch(r.Context(), data, id, book.FilePath, selectedID)
	case mode != "" && s.Hardcover.Enabled():
		s.runHardcoverSearch(r.Context(), data, id, book, mode, manualQuery, selectedID)
	}

	mergeInto(data, base)
	render(w, "book_edit_metadata.html", data)
}

// runHardcoverSearch runs mode's Hardcover search ("check" uses the book's
// own title/author/identifier, anything else is a manual free-text query),
// builds the candidate picker list, and — if selectedID names one of the
// results — overlays its fields onto the edit form's defaults.
func (s *Server) runHardcoverSearch(ctx context.Context, data map[string]any, id int64, book *index.Book, mode, manualQuery, selectedID string) {
	var matches []hardcover.Match
	var searchErr error
	if mode == "check" {
		matches, searchErr = s.Hardcover.Search(ctx, book.Title, book.Author, book.Identifier)
	} else {
		matches, searchErr = s.Hardcover.Search(ctx, manualQuery, "", "")
	}
	if searchErr != nil {
		log.Printf("edit-metadata search failed for book %d: %v", id, searchErr)
		data["SearchError"] = "Hardcover search failed."
	}

	candidates := make([]matchCandidate, 0, len(matches))
	var selected *hardcover.Match
	for i := range matches {
		m := &matches[i]
		c := matchCandidate{
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
		if d, err := s.Hardcover.Detail(ctx, selected.ID); err != nil {
			log.Printf("edit-metadata detail lookup failed for book %d: %v", id, err)
		} else {
			detail = d
		}
		applySelectedDefaults(data, selected, detail)
	}
}

// runChaptarrSearch matches filePath against every book Chaptarr is
// tracking with an on-disk file (see chaptarr.MatchByPath) and, since path
// matching yields at most one plausible candidate (unlike Hardcover's
// ranked text search), shows it as a single-item picker — the admin still
// has to click "Use this match" before it's loaded into the form, same as
// Hardcover's flow.
func (s *Server) runChaptarrSearch(ctx context.Context, data map[string]any, id int64, filePath, selectedID string) {
	books, err := s.Chaptarr.ListBooks(ctx)
	if err != nil {
		log.Printf("edit-metadata chaptarr list failed for book %d: %v", id, err)
		data["SearchError"] = "Chaptarr lookup failed."
		return
	}
	match, ok := chaptarr.MatchByPath(books, filePath)
	if !ok {
		data["Candidates"] = []matchCandidate{}
		return
	}

	candidateID := strconv.Itoa(match.ID)
	c := matchCandidate{
		ID:      candidateID,
		Title:   match.Title,
		Authors: strings.Join(match.Authors, ", "),
		Series:  match.Series,
	}
	if match.SeriesIndex != 0 {
		c.SeriesIndex = strconv.FormatFloat(match.SeriesIndex, 'f', -1, 64)
	}
	if selectedID != "" && candidateID == selectedID {
		c.Selected = true
		data["FormTitle"] = match.Title
		if match.Series != "" {
			data["FormSeries"] = match.Series
			if match.SeriesIndex != 0 {
				data["FormSeriesIndex"] = strconv.FormatFloat(match.SeriesIndex, 'f', -1, 64)
			}
		}
		if len(match.Genres) > 0 {
			data["FormGenres"] = strings.Join(match.Genres, ", ")
		}
		if match.Rating != 0 {
			data["FormRating"] = strconv.FormatFloat(match.Rating, 'f', -1, 64)
		}
	}
	data["Candidates"] = []matchCandidate{c}
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
