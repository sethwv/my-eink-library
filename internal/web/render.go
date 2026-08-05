package web

import (
	"embed"
	"html/template"
	"net/http"
)

//go:embed templates/*.html
var templatesFS embed.FS

//go:embed static/*
var staticFS embed.FS

// render parses layout.html + partials.html + the named page template fresh
// for each call so that each page's "content" block doesn't collide with any
// other page's. partials.html holds shared blocks (e.g. "topbar") reused
// across authenticated pages.
func render(w http.ResponseWriter, page string, data any) {
	tmpl, err := template.ParseFS(templatesFS, "templates/layout.html", "templates/partials.html", "templates/"+page)
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
