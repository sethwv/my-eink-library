package web

import (
	"testing"

	"github.com/swvn/eink-library/internal/chaptarr"
	"github.com/swvn/eink-library/internal/hardcover"
)

func TestMergeChaptarrFields_ChaptarrWinsWhenBothHaveAField(t *testing.T) {
	match := chaptarr.Book{
		Title:       "Onyx Storm",
		Series:      "The Empyrean",
		SeriesIndex: 3,
		Genres:      []string{"Fantasy"},
		Rating:      4.5,
	}
	hc := hardcover.Match{
		Title:       "Onyx Storm (Hardcover title)",
		Series:      "Empyrean (Hardcover series)",
		SeriesIndex: 99,
		Genres:      []string{"Romance"},
		Rating:      1.0,
	}
	f := mergeChaptarrFields(match, hc, hardcover.Detail{})
	if f.Title != "Onyx Storm" || f.Series != "The Empyrean" || f.SeriesIndex != 3 || f.Rating != 4.5 {
		t.Errorf("f = %+v, want Chaptarr's values to win where it has one", f)
	}
	if len(f.Genres) != 1 || f.Genres[0] != "Fantasy" {
		t.Errorf("Genres = %v, want Chaptarr's [Fantasy] to win", f.Genres)
	}
}

func TestMergeChaptarrFields_HardcoverFillsWhatChaptarrLacks(t *testing.T) {
	match := chaptarr.Book{Title: "Onyx Storm"}
	hc := hardcover.Match{
		Title:       "Onyx Storm",
		Description: "A dragon rider saga.",
		ReleaseDate: "2026-01-20",
		Pages:       512,
		ISBNs:       []string{"9781234567897", "1234567891"},
		Rating:      4.5,
	}
	hcDetail := hardcover.Detail{Publisher: "Entangled: Red Tower Books"}
	f := mergeChaptarrFields(match, hc, hcDetail)
	if f.Description != "A dragon rider saga." {
		t.Errorf("Description = %q, want Hardcover's (Chaptarr never has one)", f.Description)
	}
	if f.Publisher != "Entangled: Red Tower Books" {
		t.Errorf("Publisher = %q, want Hardcover's", f.Publisher)
	}
	if f.PublishedDate != "2026-01-20" {
		t.Errorf("PublishedDate = %q, want Hardcover's", f.PublishedDate)
	}
	if f.Pages != 512 {
		t.Errorf("Pages = %d, want 512 from Hardcover", f.Pages)
	}
	if f.ISBN != "9781234567897" {
		t.Errorf("ISBN = %q, want the ISBN-13 preferred by firstISBN", f.ISBN)
	}
	if f.Rating != 4.5 {
		t.Errorf("Rating = %v, want Hardcover's since Chaptarr had none", f.Rating)
	}
}

func TestMergeChaptarrFields_ChaptarrOnlyWhenHardcoverDisabledOrNoMatch(t *testing.T) {
	match := chaptarr.Book{
		Title:       "Onyx Storm",
		Series:      "The Empyrean",
		SeriesIndex: 3,
		Genres:      []string{"Fantasy"},
		Rating:      4.5,
	}
	// Zero-value hc/hcDetail — the shape callers pass when Hardcover is
	// disabled, the Chaptarr book has no HardcoverID, or the lookup failed.
	f := mergeChaptarrFields(match, hardcover.Match{}, hardcover.Detail{})
	if f.Title != "Onyx Storm" || f.Series != "The Empyrean" || f.SeriesIndex != 3 || f.Rating != 4.5 {
		t.Errorf("f = %+v, want Chaptarr's own fields preserved", f)
	}
	if f.Description != "" || f.Publisher != "" || f.Pages != 0 || f.ISBN != "" {
		t.Errorf("f = %+v, want every Hardcover-only field left blank with no daisy-chained lookup", f)
	}
}
