package chaptarr

import "testing"

func TestMatchByPath_MatchesOnTrailingSegmentsDespiteDifferentRoots(t *testing.T) {
	books := []Book{
		{Title: "Mistborn", Paths: []string{"/mnt/chaptarr-library/Brandon Sanderson/Mistborn/Mistborn.epub"}},
	}
	got, ok := MatchByPath(books, "Brandon Sanderson/Mistborn/Mistborn.epub")
	if !ok {
		t.Fatal("expected a match despite differing library roots")
	}
	if got.Title != "Mistborn" {
		t.Errorf("Title = %q, want %q", got.Title, "Mistborn")
	}
}

func TestMatchByPath_CaseAndSlashInsensitive(t *testing.T) {
	books := []Book{
		{Title: "Mistborn", Paths: []string{`C:\library\Brandon Sanderson\MISTBORN\Mistborn.epub`}},
	}
	if _, ok := MatchByPath(books, "brandon sanderson/mistborn/mistborn.epub"); !ok {
		t.Error("expected a case/slash-insensitive match")
	}
}

func TestMatchByPath_NoMatchOnDifferentFilename(t *testing.T) {
	books := []Book{
		{Title: "Warbreaker", Paths: []string{"/library/Brandon Sanderson/Warbreaker/Warbreaker.epub"}},
	}
	if _, ok := MatchByPath(books, "Brandon Sanderson/Mistborn/Mistborn.epub"); ok {
		t.Error("expected no match when even the filename differs")
	}
}

func TestMatchByPath_PrefersLongerSuffixOverlap(t *testing.T) {
	books := []Book{
		{Title: "Wrong Author Same Filename", Paths: []string{"/library/Someone Else/Mistborn/Mistborn.epub"}},
		{Title: "Correct Match", Paths: []string{"/library/Brandon Sanderson/Mistborn/Mistborn.epub"}},
	}
	got, ok := MatchByPath(books, "Brandon Sanderson/Mistborn/Mistborn.epub")
	if !ok {
		t.Fatal("expected a match")
	}
	if got.Title != "Correct Match" {
		t.Errorf("Title = %q, want the candidate with more matching trailing segments", got.Title)
	}
}

func TestMatchByPath_NoBooks(t *testing.T) {
	if _, ok := MatchByPath(nil, "Brandon Sanderson/Mistborn/Mistborn.epub"); ok {
		t.Error("expected no match with an empty book list")
	}
}

func TestMatchByPath_EmptyLocalPath(t *testing.T) {
	books := []Book{{Title: "Mistborn", Paths: []string{"/library/Mistborn.epub"}}}
	if _, ok := MatchByPath(books, ""); ok {
		t.Error("expected no match for an empty local path")
	}
}
