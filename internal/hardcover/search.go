package hardcover

import (
	"context"
	"strings"
)

// Match is the metadata Hardcover has for a book, normalized down to the
// fields this app cares about.
type Match struct {
	ID          string
	Title       string
	Authors     []string
	Series      string
	SeriesIndex float64
	ReleaseDate string // "YYYY-MM-DD", as Hardcover returns it — parseable by internal/web's formatPublished
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

// Search returns Hardcover's best candidates for a "title author" query, in
// the relevance order Hardcover's own search (Typesense) already ranks them.
func (c *Client) Search(ctx context.Context, title, author string) ([]Match, error) {
	q := strings.TrimSpace(title + " " + author)
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
		})
	}
	return matches, nil
}

// BestConfidentMatch returns the top search result only if its author list
// plausibly matches the given author — a simple case-insensitive substring
// check in either direction, since Hardcover's own text-match ranking
// already handles title similarity. If knownAuthor is blank, the top result
// is accepted without a check. Returns ok=false if there's no result or the
// top result's author doesn't plausibly match, meaning auto-fill should
// leave the book alone rather than risk attaching the wrong book's data.
func BestConfidentMatch(matches []Match, knownAuthor string) (Match, bool) {
	if len(matches) == 0 {
		return Match{}, false
	}
	top := matches[0]
	if knownAuthor == "" {
		return top, true
	}
	known := strings.ToLower(strings.TrimSpace(knownAuthor))
	for _, a := range top.Authors {
		a = strings.ToLower(strings.TrimSpace(a))
		if a == "" {
			continue
		}
		if strings.Contains(a, known) || strings.Contains(known, a) {
			return top, true
		}
	}
	return Match{}, false
}
