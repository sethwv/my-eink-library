package epub

import (
	"reflect"
	"testing"
)

func TestSplitAuthorNames(t *testing.T) {
	tests := []struct {
		in   string
		want []string
	}{
		{"P.C. Cast", []string{"P.C. Cast"}},
		{"P.C. Cast & Kristin Cast", []string{"P.C. Cast", "Kristin Cast"}},
		{"P.C. Cast and Kristin Cast", []string{"P.C. Cast", "Kristin Cast"}},
		{"P.C. Cast; Kristin Cast", []string{"P.C. Cast", "Kristin Cast"}},
		{"P.C. Cast / Kristin Cast", []string{"P.C. Cast", "Kristin Cast"}},
		// "and" must not match inside a name like "Anderson".
		{"Kevin J. Anderson", []string{"Kevin J. Anderson"}},
		{"", nil},
	}
	for _, tt := range tests {
		got := splitAuthorNames(tt.in)
		if !reflect.DeepEqual(got, tt.want) {
			t.Errorf("splitAuthorNames(%q) = %v, want %v", tt.in, got, tt.want)
		}
	}
}

func TestNormalizeAuthorName(t *testing.T) {
	tests := []struct{ in, want string }{
		{"Cast, P.C.", "P.C. Cast"},
		{"P.C. Cast", "P.C. Cast"},
		{"  P.C.   Cast  ", "P.C. Cast"},
		{"Le Guin, Ursula K.", "Ursula K. Le Guin"},
		{"Madonna", "Madonna"},
	}
	for _, tt := range tests {
		if got := normalizeAuthorName(tt.in); got != tt.want {
			t.Errorf("normalizeAuthorName(%q) = %q, want %q", tt.in, got, tt.want)
		}
	}
}

func TestCleanAuthorNames(t *testing.T) {
	tests := []struct {
		name     string
		creators []string
		want     []string
	}{
		{
			name:     "straightforward two-author book, separate creator tags",
			creators: []string{"P.C. Cast", "Kristin Cast"},
			want:     []string{"P.C. Cast", "Kristin Cast"},
		},
		{
			name:     "embedded 'and' join plus a redundant duplicate creator tag",
			creators: []string{"P.C. Cast and Kristin Cast", "Kristin Cast"},
			want:     []string{"P.C. Cast", "Kristin Cast"},
		},
		{
			name:     "same author repeated verbatim across two creator tags",
			creators: []string{"Brandon Sanderson", "Brandon Sanderson"},
			want:     []string{"Brandon Sanderson"},
		},
		{
			name:     "case-insensitive dedup",
			creators: []string{"brandon sanderson", "Brandon Sanderson"},
			want:     []string{"brandon sanderson"},
		},
		{
			name:     "single Last, First author normalized",
			creators: []string{"Martin, George R.R."},
			want:     []string{"George R.R. Martin"},
		},
		{
			name:     "blank/whitespace-only creator entries are dropped",
			creators: []string{"  ", "Amy Zed", ""},
			want:     []string{"Amy Zed"},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := cleanAuthorNames(tt.creators)
			if !reflect.DeepEqual(got, tt.want) {
				t.Errorf("cleanAuthorNames(%v) = %v, want %v", tt.creators, got, tt.want)
			}
		})
	}
}
