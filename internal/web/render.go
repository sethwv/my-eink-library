package web

import (
	"bytes"
	"embed"
	"fmt"
	"html"
	"html/template"
	"net/http"
	"net/url"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/swvn/eink-library/internal/epub"
)

//go:embed templates/*.html
var templatesFS embed.FS

//go:embed static/*
var staticFS embed.FS

// templateFuncs are available to every page template. Kept lenient: dates in
// this app come either from a Unix timestamp we set ourselves (always valid)
// or a raw string pulled straight out of whatever an EPUB's <dc:date>
// happened to contain (often just a year, sometimes a full date, sometimes
// garbage) — these never error, they just fall back to showing whatever they
// were given rather than blanking out or panicking a template render.
var templateFuncs = template.FuncMap{
	"formatUnix":          formatUnix,
	"formatPublished":     formatPublished,
	"formatYear":          formatYear,
	"formatPublishedYear": formatPublishedYear,
	"plainText":           plainText,
	"authorNames":         authorNames,
	"withQueryParam":      withQueryParam,
	"locationLabel":       locationLabel,
	"pageURL":             pageURL,
}

// locationLabel renders a book location as "<library root name>/<parent
// folder>" (e.g. "Fiction/Jane Austen") for the modal's path pills — root is
// an absolute configured LIBRARY_PATH entry, so only its base name is shown.
func locationLabel(root, path string) string {
	return filepath.Base(root) + "/" + filepath.Base(filepath.Dir(path))
}

// authorNames splits a book's stored author byline into individual,
// normalized names for display — one <a> per author on a card/modal,
// rather than one link showing the whole raw multi-author string. Always
// re-normalizes ("Last, First" → "First Last", de-duplicated) at render
// time via epub.CleanAuthorNames, the same helper EPUB parsing itself uses,
// so display is consistent even for a book indexed before this normalization
// existed — not dependent on that book having been reimported since.
func authorNames(author string) []string {
	return epub.CleanAuthorNames([]string{author})
}

// withQueryParam returns rawURL (typically baseData's CurrentURL, a
// path+query string from r.URL.RequestURI()) with key=value added or
// replaced in its query string — used to build a direct/shareable link to
// the current list view with a specific book's modal pre-opened (see
// library.html's cover-link href and modal.js's onload handler, which
// reads this same param back out), without every list-page template having
// to hand-reconstruct its own query string. Falls back to rawURL unchanged
// if it doesn't parse (shouldn't happen for anything derived from an
// actual request, but a template helper must never panic mid-render).
func withQueryParam(rawURL, key string, value any) string {
	u, err := url.Parse(rawURL)
	if err != nil {
		return rawURL
	}
	q := u.Query()
	q.Set(key, fmt.Sprint(value))
	u.RawQuery = q.Encode()
	return u.String()
}

// pageURL builds a book-list pagination/sort link with all query params
// properly escaped via url.Values.Encode() — used by library.html's
// prev/next links and mirrored by infinite-scroll.js's own query-string
// construction for XHR requests, so both paths agree on the same param set.
func pageURL(action, sort, dir string, page int, q, name string) string {
	v := url.Values{}
	v.Set("sort", sort)
	v.Set("dir", dir)
	v.Set("page", strconv.Itoa(page))
	if q != "" {
		v.Set("q", q)
	}
	if name != "" {
		v.Set("name", name)
	}
	return action + "?" + v.Encode()
}

var htmlTagPattern = regexp.MustCompile(`<[^>]*>`)

// plainText strips markup out of an EPUB's <dc:description> — Calibre and
// many other tools store this as an HTML fragment (headings, bold, nested
// divs), which html/template would otherwise auto-escape into literal
// visible "<p>", "<b>" etc. rather than rendering it. Stripping tags instead
// of rendering the HTML avoids trusting arbitrary markup from a book file;
// this app has no HTML sanitizer dependency and doesn't need one just for a
// synopsis. Collapses the whitespace left behind by removed block tags and
// unescapes entities (e.g. "&amp;") so the result reads as plain prose.
func plainText(s string) string {
	s = htmlTagPattern.ReplaceAllString(s, " ")
	s = html.UnescapeString(s)
	return strings.Join(strings.Fields(s), " ")
}

// formatUnix renders a Unix-seconds timestamp as "MM/DD/YYYY", or "" if unset.
func formatUnix(sec int64) string {
	if sec == 0 {
		return ""
	}
	return time.Unix(sec, 0).UTC().Format("01/02/2006")
}

// formatYear renders a Unix-seconds timestamp as "YYYY", or "" if unset.
// Used on the card, where both the added and published dates need to fit
// alongside title/author/series without pushing the card taller.
func formatYear(sec int64) string {
	if sec == 0 {
		return ""
	}
	return time.Unix(sec, 0).UTC().Format("2006")
}

// formatPublished renders an EPUB's raw <dc:date> string as "MM/DD/YYYY" when
// it parses as a full date, "MM/YYYY" when only year+month is present,
// "YYYY" when it's just a bare year, or the original string as a last
// resort (better to show something unparsed than to hide a date the file
// actually had).
func formatPublished(raw string) string {
	if raw == "" {
		return ""
	}
	for _, layout := range []string{time.RFC3339, "2006-01-02", "2006-01", "2006"} {
		if t, err := time.Parse(layout, raw); err == nil {
			switch layout {
			case "2006":
				return t.Format("2006")
			case "2006-01":
				return t.Format("01/2006")
			default:
				return t.Format("01/02/2006")
			}
		}
	}
	return raw
}

// formatPublishedYear renders just the "YYYY" portion of an EPUB's raw
// <dc:date> string, or the original string as a last resort when it doesn't
// parse as any recognized layout — same fallback behavior as
// formatPublished, for the same reason (better to show something unparsed
// than to hide a date the file actually had).
func formatPublishedYear(raw string) string {
	if raw == "" {
		return ""
	}
	for _, layout := range []string{time.RFC3339, "2006-01-02", "2006-01", "2006"} {
		if t, err := time.Parse(layout, raw); err == nil {
			return t.Format("2006")
		}
	}
	return raw
}

// render parses layout.html + partials.html + the named page template fresh
// for each call so that each page's "content" block doesn't collide with any
// other page's. partials.html holds shared blocks (e.g. "topbar") reused
// across authenticated pages.
//
// Executes into a buffer rather than writing to w directly: html/template
// can fail partway through ExecuteTemplate (e.g. a runtime error in a later
// block) after already emitting some output, and writing anything to w
// without an explicit WriteHeader implicitly sends a 200, so the subsequent
// http.Error(w, ..., 500) then hits "superfluous response.WriteHeader call"
// and can't actually change the status the client already received.
// Buffering means a mid-render failure still gets a clean 500 with no
// partial HTML sent.
func render(w http.ResponseWriter, page string, data any) {
	tmpl, err := template.New("root").Funcs(templateFuncs).ParseFS(templatesFS, "templates/layout.html", "templates/partials.html", "templates/"+page)
	if err != nil {
		http.Error(w, "template error", http.StatusInternalServerError)
		return
	}

	var buf bytes.Buffer
	if err := tmpl.ExecuteTemplate(&buf, "layout", data); err != nil {
		http.Error(w, "render error", http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	// Some older browser engines cache GET responses aggressively, including
	// distinct ?q=/?sort= query variations — force revalidation so paging,
	// searching, and navigating "home" always reflect the current state.
	w.Header().Set("Cache-Control", "no-cache")
	buf.WriteTo(w)
}

// renderPartials renders one or more named blocks from partials.html only
// (skipping layout.html and the page template), for XHR responses that need
// just a fragment of a page — e.g. infinite-scroll.js re-fetching book_cards
// without the surrounding topbar/toolbar/HTML shell. Same buffer-then-write
// rationale as render.
func renderPartials(w http.ResponseWriter, data any, names ...string) {
	tmpl, err := template.New("root").Funcs(templateFuncs).ParseFS(templatesFS, "templates/partials.html")
	if err != nil {
		http.Error(w, "template error", http.StatusInternalServerError)
		return
	}

	var buf bytes.Buffer
	for _, name := range names {
		if err := tmpl.ExecuteTemplate(&buf, name, data); err != nil {
			http.Error(w, "render error", http.StatusInternalServerError)
			return
		}
	}

	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Header().Set("Cache-Control", "no-cache")
	buf.WriteTo(w)
}

// StaticHandler serves the embedded static assets (CSS) under /static/.
// Same no-cache reasoning as render(): old browser engines can cache GET
// responses aggressively, and http.FileServer's default Last-Modified/ETag
// validators aren't reliable cache-busters here since go:embed doesn't
// preserve meaningful file mtimes across builds. Without this, a deployed
// CSS/JS change can silently keep serving a stale cached copy on a device
// that already loaded the app once.
func StaticHandler() http.Handler {
	fileServer := http.FileServer(http.FS(staticFS))
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Cache-Control", "no-cache")
		fileServer.ServeHTTP(w, r)
	})
}
