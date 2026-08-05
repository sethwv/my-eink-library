package web

import (
	"fmt"
	"log"
	"net/http"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/swvn/eink-library/internal/auth"
	"github.com/swvn/eink-library/internal/index"
	"github.com/swvn/eink-library/internal/kepub"
	"github.com/swvn/eink-library/internal/thumbnail"
)

type Server struct {
	Auth        *auth.Authenticator
	DB          *index.DB
	Covers      *thumbnail.Store
	LibraryPath string
	PageSize    int
}

func (s *Server) LoginPage(w http.ResponseWriter, r *http.Request) {
	render(w, "login.html", map[string]any{
		"Title": "Log in",
		"Next":  r.URL.Query().Get("next"),
	})
}

func (s *Server) LoginSubmit(w http.ResponseWriter, r *http.Request) {
	if err := r.ParseForm(); err != nil {
		http.Error(w, "bad form", http.StatusBadRequest)
		return
	}

	username := r.FormValue("username")
	password := r.FormValue("password")
	next := r.FormValue("next")

	if !s.Auth.CheckPassword(username, password) {
		render(w, "login.html", map[string]any{
			"Title": "Log in",
			"Error": "Incorrect username or password.",
			"Next":  next,
		})
		return
	}

	s.Auth.IssueSession(w, r)

	if next == "" {
		next = "/"
	}
	http.Redirect(w, r, next, http.StatusSeeOther)
}

func (s *Server) Logout(w http.ResponseWriter, r *http.Request) {
	s.Auth.ClearSession(w)
	http.Redirect(w, r, "/login", http.StatusSeeOther)
}

func (s *Server) LibraryGrid(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()

	sortParam := q.Get("sort")
	sort := index.SortTitle
	switch sortParam {
	case "author":
		sort = index.SortAuthor
	case "series":
		sort = index.SortSeries
	case "added":
		sort = index.SortAdded
	default:
		sortParam = "title"
	}

	dir := q.Get("dir")
	if dir != "asc" && dir != "desc" {
		dir = "asc"
	}
	descending := dir == "desc"

	page, _ := strconv.Atoi(q.Get("page"))
	if page < 1 {
		page = 1
	}

	pageSize := s.PageSize
	if pageSize < 1 {
		pageSize = 48
	}

	books, err := s.DB.List(sort, descending, page, pageSize)
	if err != nil {
		http.Error(w, "failed to load library", http.StatusInternalServerError)
		return
	}
	total, err := s.DB.Count()
	if err != nil {
		http.Error(w, "failed to load library", http.StatusInternalServerError)
		return
	}

	totalPages := (total + pageSize - 1) / pageSize
	if totalPages < 1 {
		totalPages = 1
	}

	toggleDir := "desc"
	if descending {
		toggleDir = "asc"
	}

	render(w, "library.html", map[string]any{
		"Title":      "Library",
		"Books":      books,
		"Sort":       sortParam,
		"Dir":        dir,
		"ToggleDir":  toggleDir,
		"Page":       page,
		"PrevPage":   page - 1,
		"NextPage":   page + 1,
		"HasNext":    page < totalPages,
		"TotalPages": totalPages,
	})
}

func (s *Server) Cover(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		http.NotFound(w, r)
		return
	}

	book, err := s.DB.Get(id)
	if err != nil || book == nil || !book.HasCover || book.CoverPath == "" {
		http.Redirect(w, r, "/static/placeholder-cover.svg", http.StatusFound)
		return
	}

	http.ServeFile(w, r, s.Covers.Path(book.CoverPath))
}

// DownloadEPUB streams the original, unmodified EPUB file.
func (s *Server) DownloadEPUB(w http.ResponseWriter, r *http.Request) {
	book, ok := s.lookupBook(w, r)
	if !ok {
		return
	}

	fullPath := filepath.Join(s.LibraryPath, book.FilePath)
	filename := filenameFor(book.Title, book.Author, "epub")

	w.Header().Set("Content-Disposition", fmt.Sprintf(`attachment; filename="%s"`, filename))
	http.ServeFile(w, r, fullPath)
}

// DownloadKepub converts the EPUB to kepub on the fly and streams it. Not cached to disk.
func (s *Server) DownloadKepub(w http.ResponseWriter, r *http.Request) {
	book, ok := s.lookupBook(w, r)
	if !ok {
		return
	}

	fullPath := filepath.Join(s.LibraryPath, book.FilePath)
	filename := filenameFor(book.Title, book.Author, "kepub.epub")

	w.Header().Set("Content-Type", "application/epub+zip")
	w.Header().Set("Content-Disposition", fmt.Sprintf(`attachment; filename="%s"`, filename))

	if err := kepub.ConvertFile(r.Context(), w, fullPath); err != nil {
		log.Printf("kepub conversion failed for %s: %v", book.FilePath, err)
	}
}

func (s *Server) lookupBook(w http.ResponseWriter, r *http.Request) (*index.Book, bool) {
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		http.NotFound(w, r)
		return nil, false
	}
	book, err := s.DB.Get(id)
	if err != nil || book == nil {
		http.NotFound(w, r)
		return nil, false
	}
	return book, true
}

func filenameFor(title, author, ext string) string {
	name := title
	if author != "" {
		name = author + " - " + title
	}
	return sanitizeFilename(name) + "." + ext
}

var filenameReplacer = strings.NewReplacer(
	"/", "-", "\\", "-", ":", "-", "\"", "'", "<", "(", ">", ")", "|", "-", "?", "", "*", "",
)

func sanitizeFilename(name string) string {
	return filenameReplacer.Replace(name)
}
