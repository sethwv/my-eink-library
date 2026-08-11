package hardcover

import (
	"context"
	"fmt"
	"strconv"
	"strings"
)

// Match is the metadata Hardcover has for a book, normalized down to the
// fields this app cares about. Description/Genres/Pages/ISBNs/Rating all
// come back on the same search document as Title/Series/etc — Hardcover's
// Typesense-backed search index returns the full document regardless of the
// `fields` search-weighting param — so no extra API call is needed to get
// them (see internal/hardcover's Detail for the two fields, cover image and
// publisher, that aren't in the search document and do need one).
type Match struct {
	ID          string
	Title       string
	Authors     []string
	Series      string
	SeriesIndex float64
	ReleaseDate string // "YYYY-MM-DD", as Hardcover returns it — parseable by internal/web's formatPublished
	Description string
	Genres      []string
	Pages       int
	ISBNs       []string
	Rating      float64
}

// searchQuery uses a GraphQL variable for the search term rather than
// string-interpolating it into the query text, so titles/authors containing
// quotes or other special characters can't break (or inject into) the query.
const searchQuery = `query Search($q: String!) {
  search(query: $q, query_type: "Book", per_page: 5, page: 1) {
    ids
    results
  }
}`

type searchResponse struct {
	Search struct {
		IDs     []int64 `json:"ids"`
		Results struct {
			Hits []struct {
				Document struct {
					ID             string   `json:"id"`
					Title          string   `json:"title"`
					AuthorNames    []string `json:"author_names"`
					ReleaseDate    string   `json:"release_date"`
					Description    string   `json:"description"`
					Genres         []string `json:"genres"`
					Pages          int      `json:"pages"`
					ISBNs          []string `json:"isbns"`
					Rating         float64  `json:"rating"`
					FeaturedSeries struct {
						Position float64 `json:"position"`
						Series   struct {
							Name string `json:"name"`
						} `json:"series"`
					} `json:"featured_series"`
				} `json:"document"`
			} `json:"hits"`
		} `json:"results"`
	} `json:"search"`
}

// Search returns Hardcover's best candidates for a "title author [identifier]"
// query, in the relevance order Hardcover's own search (Typesense) already
// ranks them. identifier is an optional ISBN/ASIN (from the EPUB's OPF
// metadata); it's only folded into the query when it has a plausible
// ISBN/ASIN shape, since Hardcover's index treats it as free text rather
// than a dedicated lookup field.
func (c *Client) Search(ctx context.Context, title, author, identifier string) ([]Match, error) {
	q := strings.TrimSpace(title + " " + author)
	if looksLikeISBNOrASIN(identifier) {
		q = strings.TrimSpace(q + " " + identifier)
	}
	var resp searchResponse
	if err := c.do(ctx, searchQuery, map[string]any{"q": q}, &resp); err != nil {
		return nil, err
	}

	matches := make([]Match, 0, len(resp.Search.Results.Hits))
	for _, hit := range resp.Search.Results.Hits {
		d := hit.Document
		matches = append(matches, Match{
			ID:          d.ID,
			Title:       d.Title,
			Authors:     d.AuthorNames,
			Series:      d.FeaturedSeries.Series.Name,
			SeriesIndex: d.FeaturedSeries.Position,
			ReleaseDate: d.ReleaseDate,
			Description: d.Description,
			Genres:      d.Genres,
			Pages:       d.Pages,
			ISBNs:       d.ISBNs,
			Rating:      d.Rating,
		})
	}
	return matches, nil
}

// Detail is the fields Hardcover only exposes via the full `books` GraphQL
// type, not the search document (see Match) — publisher name and cover image
// URL. Fetched with exactly one extra request per confidently matched book
// (not one request per field), once BestConfidentMatch has resolved an ID.
type Detail struct {
	Publisher string
	Image     string // cover image URL, empty if the book has none
}

const detailQuery = `query BookDetail($id: Int!) {
  books_by_pk(id: $id) {
    image {
      url
    }
    default_physical_edition {
      publisher {
        name
      }
    }
  }
}`

type detailResponse struct {
	BooksByPK *struct {
		Image *struct {
			URL string `json:"url"`
		} `json:"image"`
		DefaultPhysicalEdition *struct {
			Publisher *struct {
				Name string `json:"name"`
			} `json:"publisher"`
		} `json:"default_physical_edition"`
	} `json:"books_by_pk"`
}

// Detail fetches the publisher and cover image URL for a Hardcover book ID
// (as returned in Match.ID). Returns a zero Detail, no error, if the book
// has neither.
func (c *Client) Detail(ctx context.Context, id string) (Detail, error) {
	bookID, err := strconv.Atoi(id)
	if err != nil {
		return Detail{}, fmt.Errorf("hardcover: invalid book id %q: %w", id, err)
	}

	var resp detailResponse
	if err := c.do(ctx, detailQuery, map[string]any{"id": bookID}, &resp); err != nil {
		return Detail{}, err
	}
	if resp.BooksByPK == nil {
		return Detail{}, nil
	}

	var d Detail
	if ed := resp.BooksByPK.DefaultPhysicalEdition; ed != nil && ed.Publisher != nil {
		d.Publisher = ed.Publisher.Name
	}
	if img := resp.BooksByPK.Image; img != nil {
		d.Image = img.URL
	}
	return d, nil
}

// looksLikeISBNOrASIN is a loose shape check (digits/X for ISBN-10, all
// digits for ISBN-13, 10-char alphanumeric for ASIN) — good enough to avoid
// folding an unrelated identifier (e.g. a Calibre UUID) into the search text.
func looksLikeISBNOrASIN(s string) bool {
	s = strings.ToUpper(strings.ReplaceAll(strings.TrimSpace(s), "-", ""))
	if len(s) != 10 && len(s) != 13 {
		return false
	}
	for i, r := range s {
		if r >= '0' && r <= '9' {
			continue
		}
		// A trailing check digit 'X' is only valid for 10-digit ISBNs; any
		// other letter is only plausible as part of an ASIN.
		if len(s) == 10 && (r == 'X' && i == len(s)-1) {
			continue
		}
		if len(s) == 10 && r >= 'A' && r <= 'Z' {
			continue
		}
		return false
	}
	return true
}

// bundleKeywords flag titles that are almost certainly a box set, omnibus,
// or other multi-book bundle rather than the single book being enriched —
// these get deprioritized in favor of a standalone edition when one is
// available among the candidates.
var bundleKeywords = []string{
	"box set", "boxed set", "boxset", "bundle", "omnibus",
	"collection", "trilogy", "complete series", "books 1-",
}

func looksLikeBundle(title string) bool {
	t := strings.ToLower(title)
	for _, kw := range bundleKeywords {
		if strings.Contains(t, kw) {
			return true
		}
	}
	return false
}

// spinoffKeywords flag titles that are almost certainly a tie-in product
// riding on the real book's name/characters/author — a calendar, quote
// collection, study guide, coloring book, etc. — rather than the book
// itself. These show up in Hardcover's search results because they
// legitimately share the author and reference the title in their own name
// (e.g. "Quotes from George R. R. Martin's A Game of Thrones Book Series
// 2016 Day-to-Day Calendar"), which is exactly the case authorPlausiblyMatches
// and looksLikeBundle don't catch: same author, not a bundle, just not the
// book being searched for.
var spinoffKeywords = []string{
	"calendar", "day-to-day", "day to day", "quotes from", "companion",
	"study guide", "coloring book", "colouring book", "sticker book",
	"journal", "planner", "cookbook", "trivia", "workbook", "guide to",
}

func looksLikeSpinoff(title string) bool {
	t := strings.ToLower(title)
	for _, kw := range spinoffKeywords {
		if strings.Contains(t, kw) {
			return true
		}
	}
	return false
}

// titleTokens lowercases and splits title into its non-trivial (>2 char)
// alphanumeric words, the same tokenization shape splitNameTokens uses for
// author names — good enough for a coarse overlap check without pulling in
// a real string-distance library.
func titleTokens(title string) []string {
	fields := strings.FieldsFunc(strings.ToLower(title), func(r rune) bool {
		return !(r >= 'a' && r <= 'z' || r >= '0' && r <= '9')
	})
	tokens := make([]string, 0, len(fields))
	for _, f := range fields {
		if len(f) > 2 {
			tokens = append(tokens, f)
		}
	}
	return tokens
}

// minTitleOverlapRatio is how much of knownTitle's tokens a candidate must
// share to be considered plausibly the same book, as a fraction of
// knownTitle's own (usually shorter) token count — checking against the
// known title's length rather than the candidate's means a long spin-off
// title that happens to contain every word of a short real title still gets
// scored on how much of the real title it covers relative to itself, not
// diluted by the spin-off's own bulk. Tuned against the Game-of-Thrones
// calendar case: "quotes from george r r martins a game of thrones book
// series 2016 day to day calendar" shares 3 of "a game of thrones"'s 3
// non-trivial tokens... which is exactly why the keyword blocklist above
// exists as a first-line defense — the token check alone can't catch a
// title that's a superset of the real one. It's here to catch the opposite
// case: a candidate that shares the author but has an unrelated or
// substantially different title.
const minTitleOverlapRatio = 0.5

// titleImplausible reports whether candidateTitle is too dissimilar from
// knownTitle to plausibly be the same book. A blank knownTitle (nothing to
// compare against) never rejects.
func titleImplausible(candidateTitle, knownTitle string) bool {
	knownTitle = strings.TrimSpace(knownTitle)
	if knownTitle == "" {
		return false
	}
	known := titleTokens(knownTitle)
	if len(known) == 0 {
		return false
	}
	candidateSet := make(map[string]bool)
	for _, t := range titleTokens(candidateTitle) {
		candidateSet[t] = true
	}
	shared := 0
	for _, t := range known {
		if candidateSet[t] {
			shared++
		}
	}
	ratio := float64(shared) / float64(len(known))
	return ratio < minTitleOverlapRatio
}

// authorPlausiblyMatches checks knownAuthor against a candidate's author
// list: first a case-insensitive substring check in either direction, then
// (for a fuzzier fallback) a token-overlap check — any whitespace/comma-
// separated token of length > 2 shared between the two — to handle name
// variants like "Rowling, J.K." vs "J.K. Rowling".
func authorPlausiblyMatches(candidateAuthors []string, knownAuthor string) bool {
	known := strings.ToLower(strings.TrimSpace(knownAuthor))
	if known == "" {
		return true
	}
	knownTokens := splitNameTokens(known)

	for _, a := range candidateAuthors {
		a = strings.ToLower(strings.TrimSpace(a))
		if a == "" {
			continue
		}
		if strings.Contains(a, known) || strings.Contains(known, a) {
			return true
		}
		for _, t := range splitNameTokens(a) {
			for _, kt := range knownTokens {
				if t == kt {
					return true
				}
			}
		}
	}
	return false
}

func splitNameTokens(name string) []string {
	fields := strings.FieldsFunc(name, func(r rune) bool {
		return r == ' ' || r == ',' || r == '.'
	})
	tokens := make([]string, 0, len(fields))
	for _, f := range fields {
		if len(f) > 2 {
			tokens = append(tokens, f)
		}
	}
	return tokens
}

// BestConfidentMatch scans the search results (up to Search's per_page) for
// the best plausible match for (knownTitle, knownAuthor). A candidate is
// rejected outright (never returned, no fallback) if its author doesn't
// plausibly match knownAuthor, if it looks like a spin-off product
// (calendar, quote collection, study guide, etc. — same author, wrong
// product), or if its title shares too few words with knownTitle to
// plausibly be the same book — see titleImplausible's doc comment for why
// the spin-off keyword check has to exist alongside the title-overlap
// check rather than either alone catching cases like a Game of Thrones
// quote-a-day calendar. Among the remaining candidates, the first one that
// doesn't look like a box set/omnibus/bundle is preferred, since Hardcover's
// own text-match ranking already handles title similarity ordering but has
// no notion of "this is a bundle, not the single book we're after"; if
// every remaining candidate looks like a bundle, the first one is still
// returned (better than nothing — unlike the spin-off/title checks, being a
// bundle doesn't mean it's the wrong book). If knownAuthor is blank, the
// top result is accepted without any check. Returns ok=false if there's no
// result or no candidate survives filtering, meaning auto-fill should leave
// the book alone rather than risk attaching the wrong book's data.
func BestConfidentMatch(matches []Match, knownTitle, knownAuthor string) (Match, bool) {
	if len(matches) == 0 {
		return Match{}, false
	}
	if knownAuthor == "" {
		return matches[0], true
	}

	var firstPlausible *Match
	for i := range matches {
		m := &matches[i]
		if !authorPlausiblyMatches(m.Authors, knownAuthor) {
			continue
		}
		if looksLikeSpinoff(m.Title) {
			continue
		}
		if titleImplausible(m.Title, knownTitle) {
			continue
		}
		if firstPlausible == nil {
			firstPlausible = m
		}
		if !looksLikeBundle(m.Title) {
			return *m, true
		}
	}
	if firstPlausible != nil {
		return *firstPlausible, true
	}
	return Match{}, false
}
