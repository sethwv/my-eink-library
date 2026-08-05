package web

import (
	"embed"
	"html"
	"html/template"
	"net/http"
	"regexp"
	"strings"
	"time"
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
	"formatUnix":      formatUnix,
	"formatPublished": formatPublished,
	"plainText":       plainText,
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

// formatUnix renders a Unix-seconds timestamp as "Jan 2006", or "" if unset.
func formatUnix(sec int64) string {
	if sec == 0 {
		return ""
	}
	return time.Unix(sec, 0).UTC().Format("Jan 2006")
}

// formatPublished renders an EPUB's raw <dc:date> string as "Jan 2006" when
// it parses as a recognizable date, "2006" when it's just a bare year, or
// the original string as a last resort (better to show something unparsed
// than to hide a date the file actually had).
func formatPublished(raw string) string {
	if raw == "" {
		return ""
	}
	for _, layout := range []string{time.RFC3339, "2006-01-02", "2006-01", "2006"} {
		if t, err := time.Parse(layout, raw); err == nil {
			if layout == "2006" {
				return t.Format("2006")
			}
			return t.Format("Jan 2006")
		}
	}
	return raw
}

// render parses layout.html + partials.html + the named page template fresh
// for each call so that each page's "content" block doesn't collide with any
// other page's. partials.html holds shared blocks (e.g. "topbar") reused
// across authenticated pages.
func render(w http.ResponseWriter, page string, data any) {
	tmpl, err := template.New("root").Funcs(templateFuncs).ParseFS(templatesFS, "templates/layout.html", "templates/partials.html", "templates/"+page)
	if err != nil {
		http.Error(w, "template error", http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	// Some older browser engines cache GET responses aggressively, including
	// distinct ?q=/?sort= query variations — force revalidation so paging,
	// searching, and navigating "home" always reflect the current state.
	w.Header().Set("Cache-Control", "no-cache")
	if err := tmpl.ExecuteTemplate(w, "layout", data); err != nil {
		http.Error(w, "render error", http.StatusInternalServerError)
	}
}

// StaticHandler serves the embedded static assets (CSS) under /static/.
func StaticHandler() http.Handler {
	return http.FileServer(http.FS(staticFS))
}
