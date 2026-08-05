package web

import (
	"context"
	"log"
	"net/http"
	"strconv"
	"time"

	"github.com/swvn/eink-library/internal/hardcover"
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
		matches, err := s.Hardcover.Search(ctx, c.Title, c.Author)
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
		if err := s.DB.FillBlankMetadata(c.ID, best.Series, best.SeriesIndex, best.ReleaseDate); err != nil {
			log.Printf("enrichment queue: fill metadata failed for book %d: %v", c.ID, err)
			s.DB.SetEnrichmentStatus(c.ID, "error")
			continue
		}
		if err := s.DB.SetEnrichmentStatus(c.ID, "done"); err != nil {
			log.Printf("enrichment queue: mark done failed for book %d: %v", c.ID, err)
		}
	}
}

func sleepOrDone(ctx context.Context, d time.Duration) {
	select {
	case <-ctx.Done():
	case <-time.After(d):
	}
}

// BookHardcoverCheck searches Hardcover for a single book on demand (an
// admin action, bypassing the enrichment_status gate the background queue
// uses) and shows the best confident match next to the book's current
// values for confirmation before anything is applied.
func (s *Server) BookHardcoverCheck(w http.ResponseWriter, r *http.Request) {
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

	data := map[string]any{
		"Title":              "Check Hardcover",
		"BookID":             id,
		"CurrentTitle":       book.Title,
		"CurrentSeries":      book.Series,
		"CurrentSeriesIndex": book.SeriesIndex,
		"CurrentPublished":   book.PublishedAt,
		"Found":              false,
		"Enabled":            s.Hardcover.Enabled(),
	}
	if s.Hardcover.Enabled() {
		matches, err := s.Hardcover.Search(r.Context(), book.Title, book.Author)
		if err != nil {
			log.Printf("hardcover check failed for book %d: %v", id, err)
		} else if best, ok := hardcover.BestConfidentMatch(matches, book.Author); ok {
			data["Found"] = true
			data["MatchTitle"] = best.Title
			data["MatchSeries"] = best.Series
			data["MatchReleaseDate"] = best.ReleaseDate
			if best.SeriesIndex != 0 {
				data["MatchSeriesIndex"] = strconv.FormatFloat(best.SeriesIndex, 'f', -1, 64)
			}
		}
	}
	mergeInto(data, base)
	render(w, "book_hardcover_check.html", data)
}

// BookHardcoverApply writes whichever fields the confirm form still has
// values for — a blank field means the admin cleared it (or it was never
// filled in) to mean "leave this one alone", not "erase it".
func (s *Server) BookHardcoverApply(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		http.NotFound(w, r)
		return
	}
	if err := r.ParseForm(); err != nil {
		http.Error(w, "bad form", http.StatusBadRequest)
		return
	}

	title := r.FormValue("title")
	series := r.FormValue("series")
	publishedDate := r.FormValue("published_date")
	var seriesIndex float64
	if v := r.FormValue("series_index"); v != "" {
		seriesIndex, _ = strconv.ParseFloat(v, 64)
	}

	if err := s.DB.OverrideMetadata(id, title, series, seriesIndex, publishedDate); err != nil {
		http.Error(w, "failed to apply changes", http.StatusInternalServerError)
		return
	}
	if err := s.DB.SetEnrichmentStatus(id, "done"); err != nil {
		log.Printf("hardcover apply: mark done failed for book %d: %v", id, err)
	}
	http.Redirect(w, r, "/", http.StatusSeeOther)
}
