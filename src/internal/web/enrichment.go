package web

import (
	"context"
	"crypto/tls"
	"fmt"
	"io"
	"log"
	"net"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/sethwv/my-eink-library/internal/chaptarr"
	"github.com/sethwv/my-eink-library/internal/hardcover"
	"github.com/sethwv/my-eink-library/internal/index"
)

// idlePollInterval is how long the background enrichment queue waits before
// checking again when there's currently nothing to do (or the last check
// failed) — deliberately coarse, since new candidates only appear when the
// library is rescanned.
const idlePollInterval = 30 * time.Second

// enrichmentBatchSize is how many enrichment candidates RunEnrichmentQueue
// pulls per pass. Larger than one-at-a-time since Chaptarr matching reuses
// a single ListBooks fetch checked against every candidate locally (not one
// API call per book) — Hardcover's own per-book Search/Detail calls are
// still individually rate-limited by hardcover.Client's own throttle
// regardless of how many candidates are queued up here.
const enrichmentBatchSize = 200

// RunEnrichmentQueue is the single background loop for both metadata
// integrations, resuming from wherever it left off (progress is tracked in
// the books table, not in memory) so a server restart doesn't reprocess
// already-`done`/`no_match`/`error` books. Idles (rather than returning)
// while both integrations are disabled, so enabling either from the admin
// Integrations page after startup makes this loop start processing without
// a server restart.
//
// Both integrations are tried per book, in the same goroutine, in a fixed
// order: Chaptarr first (via processChaptarrMatch), then — only if
// Chaptarr didn't claim the book (disabled, or no path match) — Hardcover's
// own fuzzy search (via processHardcoverMatch). This used to be two
// separate goroutines each independently polling BooksNeedingEnrichment,
// which meant "Chaptarr takes precedence" was only true if its pass
// happened to reach a book before Hardcover's did — a race, not a
// guarantee (confirmed live: a book Chaptarr could match sometimes ended up
// enriched by Hardcover's fuzzy search instead, purely because that
// goroutine's poll happened to win). Doing both in one deterministic
// per-book step removes the race entirely: for any given book, Chaptarr is
// always checked and always wins if it has a match, in every single pass,
// not just often.
//
// Chaptarr's catalog is fetched via ListBooksCached (chaptarr.DefaultCacheTTL,
// currently 12h) rather than a fresh crawl every pass — a full crawl is
// several dozen to several hundred HTTP requests on a real library (see
// chaptarr.Client's doc comment), and most passes don't need current-to-
// the-second data. A candidate that doesn't match a *cached* list is held
// back from the Hardcover fallback and retried once against a forced
// RefreshBooks after the main loop, rather than conceded to Hardcover
// immediately — a stale-cache false negative falling through to Hardcover
// would otherwise silently break the precedence guarantee above for a book
// added to Chaptarr since the last crawl.
func (s *Server) RunEnrichmentQueue(ctx context.Context) {
	for {
		select {
		case <-ctx.Done():
			return
		default:
		}

		if !s.Hardcover.Enabled() && !s.Chaptarr.Enabled() {
			sleepOrDone(ctx, idlePollInterval)
			continue
		}

		candidates, err := s.DB.BooksNeedingEnrichment(enrichmentBatchSize)
		if err != nil {
			log.Printf("enrichment queue: list candidates: %v", err)
			sleepOrDone(ctx, idlePollInterval)
			continue
		}
		if len(candidates) == 0 {
			sleepOrDone(ctx, idlePollInterval)
			continue
		}

		overwriteCover := false
		if settings, err := s.Users.GetIntegrationSettings(); err != nil {
			log.Printf("enrichment queue: load integration settings: %v", err)
		} else {
			overwriteCover = settings.HardcoverOverwriteCover
		}

		var chBooks []chaptarr.Book
		fromCache := false
		if s.Chaptarr.Enabled() {
			var err error
			chBooks, fromCache, err = s.Chaptarr.ListBooksCached(ctx, chaptarr.DefaultCacheTTL)
			if err != nil {
				log.Printf("enrichment queue: chaptarr list books: %v", err)
				// Don't abort the whole pass — Hardcover can still process
				// every candidate below even though Chaptarr's list failed.
			}
		}

		// Candidates that don't match the (possibly cached) Chaptarr list
		// are held back from the Hardcover fallback rather than conceded to
		// it immediately, if that list came from cache — a stale cache
		// saying "no match" doesn't mean Chaptarr genuinely has no match,
		// just that it didn't as of the last crawl (e.g. the book was added
		// to Chaptarr since). Falling through to Hardcover on a false
		// negative would violate Chaptarr's precedence, since a book
		// Hardcover marks done/no_match drops out of BooksNeedingEnrichment
		// for good. See the second loop below for the one bounded re-crawl
		// that resolves this before anything is actually conceded.
		var deferred []index.EnrichmentCandidate
		processedAny := false
		for _, c := range candidates {
			select {
			case <-ctx.Done():
				return
			default:
			}

			if s.processChaptarrMatch(ctx, c, chBooks, overwriteCover) {
				processedAny = true
				continue
			}
			if fromCache && s.Chaptarr.Enabled() {
				deferred = append(deferred, c)
				continue
			}
			// A confident "no path match": either Chaptarr is disabled (in
			// which case chBooks is nil and this is skipped entirely below)
			// or the list we checked against was fresh, not stale cache.
			if chBooks != nil {
				if err := s.DB.SetChaptarrStatus(c.ID, "no_match"); err != nil {
					log.Printf("enrichment queue: mark chaptarr no_match failed for book %d: %v", c.ID, err)
				}
			}
			if s.processHardcoverMatch(ctx, c, overwriteCover) {
				processedAny = true
			}
		}

		if len(deferred) > 0 {
			freshBooks, err := s.Chaptarr.RefreshBooks(ctx)
			if err != nil {
				log.Printf("enrichment queue: chaptarr refresh for deferred candidates: %v", err)
				freshBooks = nil
			}
			for _, c := range deferred {
				if freshBooks != nil && s.processChaptarrMatch(ctx, c, freshBooks, overwriteCover) {
					processedAny = true
					continue
				}
				// freshBooks (unlike the earlier cached chBooks) is never
				// stale, so a miss against it is confident either way.
				if freshBooks != nil {
					if err := s.DB.SetChaptarrStatus(c.ID, "no_match"); err != nil {
						log.Printf("enrichment queue: mark chaptarr no_match failed for book %d: %v", c.ID, err)
					}
				}
				if s.processHardcoverMatch(ctx, c, overwriteCover) {
					processedAny = true
				}
			}
		}

		if !processedAny {
			sleepOrDone(ctx, idlePollInterval)
		}
	}
}

// processChaptarrMatch tries to match c against chBooks (Chaptarr's tracked
// library, already fetched once for this whole pass) and, on a match,
// applies it — daisy-chaining a Hardcover.GetByID lookup for the fields
// Chaptarr never has (description, publisher, ISBN, page count) when
// Hardcover is also enabled and the match has a HardcoverID (Chaptarr
// sourced it from Hardcover — see chaptarr.Book), and filling in the cover
// image the same way, only when the book doesn't already have one. See
// mergeChaptarrFields for the merge itself. Chaptarr still gets
// precedence-credit (source stays SourceChaptarr) since it's the one that
// found the match; the daisy-chained Hardcover data is a same-book
// supplement, not a competing match.
//
// Returns true if Chaptarr claimed this book (even if applying/cover-fetch
// hit a partial error) — the caller must not also run Hardcover's fuzzy
// search for it in that case. Returns false (Chaptarr disabled, no path
// match, or the list fetch failed) to signal the Hardcover fallback should
// run instead. A book Chaptarr has no path match for is left with
// status=” here (not "no_match" — that status means "checked and
// confirmed no match", which a local path heuristic alone can't assert) so
// it's free to fall through to processHardcoverMatch in this same pass.
func (s *Server) processChaptarrMatch(ctx context.Context, c index.EnrichmentCandidate, chBooks []chaptarr.Book, overwriteCover bool) bool {
	if !s.Chaptarr.Enabled() || chBooks == nil {
		return false
	}
	match, ok := chaptarr.MatchByPath(chBooks, c.FilePath)
	if !ok {
		return false
	}

	var hc hardcover.Match
	var hcDetail hardcover.Detail
	if s.Hardcover.Enabled() && match.HardcoverID != "" {
		if m, d, err := s.Hardcover.GetByID(ctx, match.HardcoverID); err != nil {
			log.Printf("enrichment queue: hardcover daisy-chain lookup failed for book %d: %v", c.ID, err)
		} else {
			hc, hcDetail = m, d
		}
	}

	fields := mergeChaptarrFields(match, hc, hcDetail)
	if err := s.DB.ApplyEnrichment(c.ID, fields, index.SourceChaptarr); err != nil {
		log.Printf("enrichment queue: chaptarr apply enrichment failed for book %d: %v", c.ID, err)
		return true
	}

	// Auto-apply a cover when the book doesn't have one, or unconditionally
	// when the admin has enabled "always overwrite cover" — see
	// IntegrationSettings.HardcoverOverwriteCover. Manual override (any
	// time) also lives in the Edit Metadata page.
	if hcDetail.Image != "" {
		if book, err := s.DB.Get(c.ID); err == nil && book != nil && (overwriteCover || !book.HasCover) {
			if err := s.applyCoverFromURL(ctx, c.ID, hcDetail.Image); err != nil {
				log.Printf("enrichment queue: chaptarr cover fetch failed for book %d: %v", c.ID, err)
			}
		}
	}

	// Run last: a merge deletes c.ID's books row if another row wins the
	// tiebreak, so the cover-apply above (which still needs c.ID) must
	// finish first.
	if fields.ISBN != "" {
		if err := s.DB.MergeDuplicateISBN(c.ID, s.LibraryPaths); err != nil {
			log.Printf("enrichment queue: isbn merge check failed for book %d: %v", c.ID, err)
		}
	}
	return true
}

// processHardcoverMatch runs Hardcover's fuzzy title/author search for c
// and applies a confident match — the fallback path for whatever Chaptarr
// didn't claim (or when Chaptarr is disabled entirely). Returns true if
// Hardcover was enabled and attempted this candidate, regardless of outcome
// (matched, no_match, or error), so the caller's "did any real work happen
// this pass" bookkeeping is accurate; false only when Hardcover is disabled
// and nothing was attempted at all.
func (s *Server) processHardcoverMatch(ctx context.Context, c index.EnrichmentCandidate, overwriteCover bool) bool {
	if !s.Hardcover.Enabled() {
		return false
	}

	matches, err := s.Hardcover.Search(ctx, c.Title, c.Author, c.Identifier)
	if err != nil {
		log.Printf("enrichment queue: search failed for book %d: %v", c.ID, err)
		if err := s.DB.SetEnrichmentStatus(c.ID, "error"); err != nil {
			log.Printf("enrichment queue: mark error failed for book %d: %v", c.ID, err)
		}
		if err := s.DB.SetHardcoverStatus(c.ID, "error"); err != nil {
			log.Printf("enrichment queue: mark hardcover error failed for book %d: %v", c.ID, err)
		}
		return true
	}
	best, ok := hardcover.BestConfidentMatch(matches, c.Title, c.Author)
	if !ok {
		if err := s.DB.SetEnrichmentStatus(c.ID, "no_match"); err != nil {
			log.Printf("enrichment queue: mark no_match failed for book %d: %v", c.ID, err)
		}
		if err := s.DB.SetHardcoverStatus(c.ID, "no_match"); err != nil {
			log.Printf("enrichment queue: mark hardcover no_match failed for book %d: %v", c.ID, err)
		}
		return true
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
		return true
	}

	// Auto-apply a cover when the book doesn't have one, or unconditionally
	// when the admin has enabled "always overwrite cover" — see
	// IntegrationSettings.HardcoverOverwriteCover. Manual override (any
	// time) also lives in the Edit Metadata page.
	if detail.Image != "" {
		if book, err := s.DB.Get(c.ID); err == nil && book != nil && (overwriteCover || !book.HasCover) {
			if err := s.applyCoverFromURL(ctx, c.ID, detail.Image); err != nil {
				log.Printf("enrichment queue: cover fetch failed for book %d: %v", c.ID, err)
			}
		}
	}

	if err := s.DB.SetEnrichmentStatus(c.ID, "done"); err != nil {
		log.Printf("enrichment queue: mark done failed for book %d: %v", c.ID, err)
	}

	// Run last: a merge deletes c.ID's books row if another row wins the
	// tiebreak, so anything above that still needs c.ID (cover apply,
	// status writes) must finish first.
	if fields.ISBN != "" {
		if err := s.DB.MergeDuplicateISBN(c.ID, s.LibraryPaths); err != nil {
			log.Printf("enrichment queue: isbn merge check failed for book %d: %v", c.ID, err)
		}
	}
	return true
}

// mergeChaptarrFields combines a Chaptarr path match's own (thinner) fields
// with an optional daisy-chained Hardcover lookup's (richer) fields into
// one HardcoverFields for ApplyEnrichment. Chaptarr's value wins wherever
// it has one (it's the higher-precedence source and the one that actually
// matched this book) — Hardcover only fills in what Chaptarr left blank.
// hc/hcDetail may be zero values (Hardcover disabled, no HardcoverID, or
// the lookup failed) — every Hardcover-sourced field then stays blank,
// same as if this were a Chaptarr-only match.
func mergeChaptarrFields(match chaptarr.Book, hc hardcover.Match, hcDetail hardcover.Detail) index.HardcoverFields {
	f := index.HardcoverFields{
		Title:       match.Title,
		Series:      match.Series,
		SeriesIndex: match.SeriesIndex,
		Genres:      match.Genres,
		Rating:      match.Rating,
	}
	if f.Title == "" {
		f.Title = hc.Title
	}
	if f.Series == "" {
		f.Series, f.SeriesIndex = hc.Series, hc.SeriesIndex
	}
	if len(f.Genres) == 0 {
		f.Genres = hc.Genres
	}
	if f.Rating == 0 {
		f.Rating = hc.Rating
	}
	// Chaptarr never has these — always Hardcover's, when a daisy-chained
	// lookup succeeded.
	f.PublishedDate = hc.ReleaseDate
	f.Description = hc.Description
	f.Publisher = hcDetail.Publisher
	f.Pages = hc.Pages
	f.ISBN = firstISBN(hc.ISBNs)
	return f
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

var lookupIP = net.LookupIP

// applyCoverFromURL downloads the image at rawURL, resizes/caches it through
// the same thumbnail store the EPUB scanner uses, and points the book's
// cover_path at it. rawURL ultimately comes from a value round-tripped
// through the Edit Metadata form's hidden field, so it's untrusted input —
// resolveCoverURL pins the request to a vetted public address before it is
// sent, including if the hostname changes DNS records after validation.
func (s *Server) applyCoverFromURL(ctx context.Context, bookID int64, rawURL string) error {
	u, requestHost, serverName, err := resolveCoverURL(rawURL)
	if err != nil {
		return fmt.Errorf("refusing to fetch cover url: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u.String(), nil)
	if err != nil {
		return err
	}
	// Preserve the provider hostname for TLS and HTTP virtual-host routing,
	// while the transport connects only to the vetted IP address in u.Host.
	req.Host = requestHost
	client := &http.Client{
		Timeout: 15 * time.Second,
		Transport: &http.Transport{
			TLSClientConfig: &tls.Config{ServerName: serverName},
		},
		CheckRedirect: func(req *http.Request, via []*http.Request) error {
			return http.ErrUseLastResponse
		},
	}
	resp, err := client.Do(req)
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

// resolveCoverURL returns a request URL whose host is a vetted public IP.
func resolveCoverURL(rawURL string) (*url.URL, string, string, error) {
	u, err := url.Parse(rawURL)
	if err != nil {
		return nil, "", "", err
	}
	if u.Scheme != "https" || u.User != nil {
		return nil, "", "", fmt.Errorf("URL must be an https URL without credentials")
	}
	host := u.Hostname()
	if host == "" {
		return nil, "", "", fmt.Errorf("missing host")
	}
	requestHost := u.Host

	addrs, err := lookupIP(host)
	if err != nil {
		return nil, "", "", fmt.Errorf("resolve host: %w", err)
	}
	if len(addrs) == 0 {
		return nil, "", "", fmt.Errorf("host %q did not resolve to any address", host)
	}
	for _, ip := range addrs {
		if !ip.IsGlobalUnicast() || ip.IsPrivate() || ip.IsLoopback() || ip.IsLinkLocalUnicast() || ip.IsLinkLocalMulticast() {
			return nil, "", "", fmt.Errorf("host %q resolves to a non-public address (%s)", host, ip)
		}
	}

	ip := addrs[0]
	if port := u.Port(); port != "" {
		u.Host = net.JoinHostPort(ip.String(), port)
	} else {
		u.Host = ip.String()
	}
	return u, requestHost, host, nil
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
// Hardcover's flow. Uses RefreshBooks (bypassing the background queue's
// cache — see chaptarr.Client) rather than the cached ListBooksCached,
// since a manual admin-initiated check should never risk showing stale
// results; this also warms the shared cache for the background queue's
// benefit.
func (s *Server) runChaptarrSearch(ctx context.Context, data map[string]any, id int64, filePath, selectedID string) {
	books, err := s.Chaptarr.RefreshBooks(ctx)
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

		var hc hardcover.Match
		var hcDetail hardcover.Detail
		if s.Hardcover.Enabled() && match.HardcoverID != "" {
			if m, d, err := s.Hardcover.GetByID(ctx, match.HardcoverID); err != nil {
				log.Printf("edit-metadata chaptarr daisy-chain lookup failed for book %d: %v", id, err)
			} else {
				hc, hcDetail = m, d
			}
		}
		applyMergedFieldsAsDefaults(data, mergeChaptarrFields(match, hc, hcDetail), hcDetail.Image)
	}
	data["Candidates"] = []matchCandidate{c}
}

// applyMergedFieldsAsDefaults overwrites the edit form's default values
// from f wherever a field is actually set — same "blank means leave alone"
// convention ApplyEnrichment/SaveMetadata use, so a book's existing value
// for a field neither Chaptarr nor a daisy-chained Hardcover lookup had
// isn't blanked out in the form.
func applyMergedFieldsAsDefaults(data map[string]any, f index.HardcoverFields, coverURL string) {
	if f.Title != "" {
		data["FormTitle"] = f.Title
	}
	if f.Series != "" {
		data["FormSeries"] = f.Series
		if f.SeriesIndex != 0 {
			data["FormSeriesIndex"] = strconv.FormatFloat(f.SeriesIndex, 'f', -1, 64)
		}
	}
	if f.PublishedDate != "" {
		data["FormPublished"] = f.PublishedDate
	}
	if f.Description != "" {
		data["FormDesc"] = f.Description
	}
	if len(f.Genres) > 0 {
		data["FormGenres"] = strings.Join(f.Genres, ", ")
	}
	if f.Publisher != "" {
		data["FormPublisher"] = f.Publisher
	}
	if f.Pages != 0 {
		data["FormPages"] = strconv.Itoa(f.Pages)
	}
	if f.ISBN != "" {
		data["FormISBN"] = f.ISBN
	}
	if f.Rating != 0 {
		data["FormRating"] = strconv.FormatFloat(f.Rating, 'f', -1, 64)
	}
	if coverURL != "" {
		data["SelectedCoverURL"] = coverURL
	}
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
