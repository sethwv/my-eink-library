package web

import (
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
)

func TestRenderIncludesBuildVersion(t *testing.T) {
	recorder := httptest.NewRecorder()
	render(recorder, "login.html", map[string]any{
		"Title":        "Log in",
		"SiteName":     "eink-library",
		"BuildVersion": "v1.2.3",
		"BuildDate":    "2026-08-15",
	})

	if recorder.Code != 200 {
		t.Fatalf("status = %d, want 200", recorder.Code)
	}
	if !strings.Contains(recorder.Body.String(), "v1.2.3 08-15-2026") {
		t.Errorf("response does not contain the build metadata: %s", recorder.Body.String())
	}
}

func TestBaseDataIncludesBuildVersion(t *testing.T) {
	server := &Server{BuildVersion: "v1.2.3", BuildDate: "2026-08-15"}
	data, _, err := server.baseData(httptest.NewRequest("GET", "/login", nil))
	if err != nil {
		t.Fatalf("baseData() error = %v", err)
	}
	if got := data["BuildVersion"]; got != "v1.2.3" {
		t.Errorf("BuildVersion = %v, want v1.2.3", got)
	}
	if got := data["BuildDate"]; got != "2026-08-15" {
		t.Errorf("BuildDate = %v, want 2026-08-15", got)
	}
}

func TestFormatBuildDate(t *testing.T) {
	if got := formatBuildDate("2026-08-15"); got != "08-15-2026" {
		t.Errorf("formatBuildDate() = %q, want 08-15-2026", got)
	}
}

func TestWithQueryParam(t *testing.T) {
	tests := []struct {
		name     string
		rawURL   string
		key      string
		value    any
		wantPath string
		wantVals map[string]string
	}{
		{
			name:     "adds param to a bare path",
			rawURL:   "/",
			key:      "book",
			value:    42,
			wantPath: "/",
			wantVals: map[string]string{"book": "42"},
		},
		{
			name:     "adds param alongside existing query string",
			rawURL:   "/authors?name=P.C.+Cast&sort=title",
			key:      "book",
			value:    7,
			wantPath: "/authors",
			wantVals: map[string]string{"book": "7", "name": "P.C. Cast", "sort": "title"},
		},
		{
			name:     "replaces an existing value for the same key",
			rawURL:   "/?book=1",
			key:      "book",
			value:    2,
			wantPath: "/",
			wantVals: map[string]string{"book": "2"},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := withQueryParam(tt.rawURL, tt.key, tt.value)
			parsed, err := url.Parse(got)
			if err != nil {
				t.Fatalf("withQueryParam(%q, %q, %v) = %q, not a valid URL: %v", tt.rawURL, tt.key, tt.value, got, err)
			}
			if parsed.Path != tt.wantPath {
				t.Errorf("path = %q, want %q (from %q)", parsed.Path, tt.wantPath, got)
			}
			for k, want := range tt.wantVals {
				if v := parsed.Query().Get(k); v != want {
					t.Errorf("query[%q] = %q, want %q (from %q)", k, v, want, got)
				}
			}
		})
	}
}

func TestWithQueryParam_InvalidURLReturnsUnchanged(t *testing.T) {
	// A control character makes url.Parse fail — withQueryParam must not
	// panic, just hand back the input unchanged.
	bad := "/\x7f"
	if got := withQueryParam(bad, "book", 1); got != bad {
		t.Errorf("withQueryParam(%q, ...) = %q, want the input unchanged on a parse failure", bad, got)
	}
}
