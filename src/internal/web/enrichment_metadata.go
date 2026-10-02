package web

import (
	"strings"

	"github.com/sethwv/my-sideload-library/internal/chaptarr"
	"github.com/sethwv/my-sideload-library/internal/hardcover"
	"github.com/sethwv/my-sideload-library/internal/index"
)

// hardcoverMetadataPatch adapts a Hardcover match and detail document to the
// index persistence boundary.
func hardcoverMetadataPatch(match hardcover.Match, detail hardcover.Detail) index.MetadataPatch {
	return index.MetadataPatch{
		Title:         match.Title,
		Series:        match.Series,
		SeriesIndex:   match.SeriesIndex,
		PublishedDate: match.ReleaseDate,
		Description:   match.Description,
		Genres:        match.Genres,
		Publisher:     detail.Publisher,
		Pages:         match.Pages,
		ISBN:          firstISBN(match.ISBNs),
		Rating:        match.Rating,
	}
}

// mergeChaptarrFields retains Chaptarr precedence and uses the optional
// Hardcover lookup only to fill fields unavailable from Chaptarr.
func mergeChaptarrFields(match chaptarr.Book, hc hardcover.Match, hcDetail hardcover.Detail) index.MetadataPatch {
	f := index.MetadataPatch{
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
	f.PublishedDate = hc.ReleaseDate
	f.Description = hc.Description
	f.Publisher = hcDetail.Publisher
	f.Pages = hc.Pages
	f.ISBN = firstISBN(hc.ISBNs)
	return f
}

func firstISBN(isbns []string) string {
	for _, isbn := range isbns {
		if len(strings.ReplaceAll(isbn, "-", "")) == 13 {
			return isbn
		}
	}
	if len(isbns) > 0 {
		return isbns[0]
	}
	return ""
}
