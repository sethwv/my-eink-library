package hardcover

import "testing"

func TestBestConfidentMatch_AcceptsMatchingAuthor(t *testing.T) {
	matches := []Match{{Title: "Mistborn", Authors: []string{"Brandon Sanderson"}}}
	got, ok := BestConfidentMatch(matches, "Brandon Sanderson")
	if !ok {
		t.Fatal("expected a confident match")
	}
	if got.Title != "Mistborn" {
		t.Errorf("Title = %q, want %q", got.Title, "Mistborn")
	}
}

func TestBestConfidentMatch_AcceptsCaseInsensitiveSubstring(t *testing.T) {
	matches := []Match{{Title: "Mistborn", Authors: []string{"brandon sanderson"}}}
	if _, ok := BestConfidentMatch(matches, "Brandon Sanderson"); !ok {
		t.Error("expected case-insensitive author match to be confident")
	}
}

func TestBestConfidentMatch_RejectsMismatchedAuthor(t *testing.T) {
	matches := []Match{{Title: "Mistborn", Authors: []string{"Someone Else"}}}
	if _, ok := BestConfidentMatch(matches, "Brandon Sanderson"); ok {
		t.Error("expected a mismatched author to not be confident")
	}
}

func TestBestConfidentMatch_NoResults(t *testing.T) {
	if _, ok := BestConfidentMatch(nil, "Brandon Sanderson"); ok {
		t.Error("expected no results to not be confident")
	}
}

func TestBestConfidentMatch_AcceptsTopResultWhenAuthorUnknown(t *testing.T) {
	matches := []Match{{Title: "Mistborn", Authors: []string{"Anyone"}}}
	if _, ok := BestConfidentMatch(matches, ""); !ok {
		t.Error("expected top result to be accepted when we don't know the author ourselves")
	}
}

func TestBestConfidentMatch_SkipsBundleInFavorOfStandaloneEdition(t *testing.T) {
	matches := []Match{
		{Title: "The Mistborn Trilogy Boxed Set", Authors: []string{"Brandon Sanderson"}},
		{Title: "Mistborn", Authors: []string{"Brandon Sanderson"}},
	}
	got, ok := BestConfidentMatch(matches, "Brandon Sanderson")
	if !ok {
		t.Fatal("expected a confident match")
	}
	if got.Title != "Mistborn" {
		t.Errorf("Title = %q, want the standalone edition to be preferred over the box set", got.Title)
	}
}

func TestBestConfidentMatch_FallsBackToBundleIfNothingElseMatches(t *testing.T) {
	matches := []Match{
		{Title: "Someone Else's Book", Authors: []string{"Someone Else"}},
		{Title: "The Mistborn Omnibus", Authors: []string{"Brandon Sanderson"}},
	}
	got, ok := BestConfidentMatch(matches, "Brandon Sanderson")
	if !ok {
		t.Fatal("expected a confident match")
	}
	if got.Title != "The Mistborn Omnibus" {
		t.Errorf("Title = %q, want the only author-plausible candidate even though it's a bundle", got.Title)
	}
}

func TestBestConfidentMatch_FuzzyAuthorTokenOverlap(t *testing.T) {
	matches := []Match{{Title: "Mistborn", Authors: []string{"Sanderson, Brandon"}}}
	if _, ok := BestConfidentMatch(matches, "Brandon Sanderson"); !ok {
		t.Error("expected token-overlap fallback to match a reordered author name")
	}
}
