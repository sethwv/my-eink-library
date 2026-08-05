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
	"github.com/swvn/eink-library/internal/users"
)

type Server struct {
	Auth        *auth.Authenticator
	DB          *index.DB
	Covers      *thumbnail.Store
	Users       *users.Store
	LibraryPath string
	PageSize    int
}

const favoritesSlug = "favourites"
const favoritesName = "Favourites"

// bookView pairs a book with whether the current viewer has favorited it,
// so library.html can render the star without a second per-book query.
type bookView struct {
	index.Book
	IsFavorite bool
}

// viewerInfo returns the signed-in username and whether they're an admin,
// for use in template data across authenticated pages.
func (s *Server) viewerInfo(r *http.Request) (username string, isAdmin bool) {
	username, _ = auth.UsernameFromContext(r.Context())
	if username != "" {
		isAdmin = s.Users.IsAdmin(username)
	}
	return username, isAdmin
}

// safeNext restricts post-login redirect targets to same-site relative paths,
// rejecting absolute and protocol-relative ("//host/...") URLs to prevent open redirects.
func safeNext(next string) string {
	if next == "" || !strings.HasPrefix(next, "/") || strings.HasPrefix(next, "//") {
		return "/"
	}
	return next
}

func (s *Server) LoginPage(w http.ResponseWriter, r *http.Request) {
	render(w, "login.html", map[string]any{
		"Title": "Log in",
		"Next":  safeNext(r.URL.Query().Get("next")),
	})
}

func (s *Server) LoginSubmit(w http.ResponseWriter, r *http.Request) {
	if err := r.ParseForm(); err != nil {
		http.Error(w, "bad form", http.StatusBadRequest)
		return
	}

	username := r.FormValue("username")
	password := r.FormValue("password")
	next := safeNext(r.FormValue("next"))

	if !s.Auth.CheckPassword(username, password) {
		render(w, "login.html", map[string]any{
			"Title": "Log in",
			"Error": "Incorrect username or password.",
			"Next":  next,
		})
		return
	}

	s.Auth.IssueSession(w, r, username)

	http.Redirect(w, r, next, http.StatusSeeOther)
}

func (s *Server) Logout(w http.ResponseWriter, r *http.Request) {
	s.Auth.ClearSession(w)
	http.Redirect(w, r, "/login", http.StatusSeeOther)
}

func (s *Server) LibraryGrid(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	s.renderBookList(w, r, bookListParams{
		action:      "/",
		filter:      index.Filter{Search: strings.TrimSpace(q.Get("q"))},
		heading:     "Library",
		defaultSort: index.SortTitle,
		filtered:    false,
	})
}

var validSortParams = map[string]bool{"title": true, "author": true, "series": true, "added": true, "released": true}

// bookListParams configures one call to renderBookList: the shared
// sort/page/search machinery behind the library grid and every filtered
// view (author, series, and future ones like shelves/favorites).
type bookListParams struct {
	action      string       // form action / link base path, e.g. "/" or "/authors"
	name        string       // carried through as a hidden "name" param on filtered views
	filter      index.Filter // Author/Series (if any) pre-set by the caller; Search is filled in from the request
	heading     string
	defaultSort index.SortKey
	filtered    bool // show the "back to library" link
}

func (s *Server) renderBookList(w http.ResponseWriter, r *http.Request, p bookListParams) {
	q := r.URL.Query()

	sortParam := q.Get("sort")
	if !validSortParams[sortParam] {
		sortParam = string(p.defaultSort)
	}
	sort := index.SortKey(sortParam)

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

	search := strings.TrimSpace(q.Get("q"))
	filter := p.filter
	filter.Search = search

	books, err := s.DB.List(sort, descending, page, pageSize, filter)
	if err != nil {
		http.Error(w, "failed to load library", http.StatusInternalServerError)
		return
	}
	total, err := s.DB.Count(filter)
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

	username, isAdmin := s.viewerInfo(r)

	views := make([]bookView, len(books))
	if username != "" {
		shelfID, err := s.DB.EnsureSystemShelf(username, favoritesSlug, favoritesName)
		if err != nil {
			http.Error(w, "failed to load favourites", http.StatusInternalServerError)
			return
		}
		favIDs, err := s.DB.ShelfBookIDs(shelfID)
		if err != nil {
			http.Error(w, "failed to load favourites", http.StatusInternalServerError)
			return
		}
		for i, b := range books {
			views[i] = bookView{Book: b, IsFavorite: favIDs[b.ID]}
		}
	} else {
		for i, b := range books {
			views[i] = bookView{Book: b}
		}
	}

	render(w, "library.html", map[string]any{
		"Title":      p.heading,
		"Heading":    p.heading,
		"Books":      views,
		"Sort":       sortParam,
		"Dir":        dir,
		"ToggleDir":  toggleDir,
		"Page":       page,
		"PrevPage":   page - 1,
		"NextPage":   page + 1,
		"HasNext":    page < totalPages,
		"TotalPages": totalPages,
		"Username":   username,
		"IsAdmin":    isAdmin,
		"Query":      search,
		"Action":     p.action,
		"Name":       p.name,
		"Filtered":   p.filtered,
		"CurrentURL": r.URL.RequestURI(),
	})
}

// AuthorsHandler is dual-mode: with no ?name=, it lists every author (browse
// index); with ?name=, it shows that author's books via renderBookList.
func (s *Server) AuthorsHandler(w http.ResponseWriter, r *http.Request) {
	name := r.URL.Query().Get("name")
	if name == "" {
		s.renderNameIndex(w, r, "Authors", "/authors", s.DB.ListAuthors)
		return
	}
	s.renderBookList(w, r, bookListParams{
		action:      "/authors",
		name:        name,
		filter:      index.Filter{Author: name},
		heading:     "Books by " + name,
		defaultSort: index.SortTitle,
		filtered:    true,
	})
}

// SeriesHandler is dual-mode: with no ?name=, it lists every series (browse
// index); with ?name=, it shows that series' books via renderBookList.
func (s *Server) SeriesHandler(w http.ResponseWriter, r *http.Request) {
	name := r.URL.Query().Get("name")
	if name == "" {
		s.renderNameIndex(w, r, "Series", "/series", s.DB.ListSeries)
		return
	}
	s.renderBookList(w, r, bookListParams{
		action:      "/series",
		name:        name,
		filter:      index.Filter{Series: name},
		heading:     "Series: " + name,
		defaultSort: index.SortSeries,
		filtered:    true,
	})
}

// FavoritesHandler shows the current user's favourites shelf.
func (s *Server) FavoritesHandler(w http.ResponseWriter, r *http.Request) {
	username, _ := auth.UsernameFromContext(r.Context())
	shelfID, err := s.DB.EnsureSystemShelf(username, favoritesSlug, favoritesName)
	if err != nil {
		http.Error(w, "failed to load favourites", http.StatusInternalServerError)
		return
	}
	s.renderBookList(w, r, bookListParams{
		action:      "/favorites",
		filter:      index.Filter{ShelfID: shelfID},
		heading:     favoritesName,
		defaultSort: index.SortTitle,
		filtered:    true,
	})
}

// FavoriteToggle adds or removes a book from the current user's favourites
// shelf, then redirects back to wherever the request came from.
func (s *Server) FavoriteToggle(w http.ResponseWriter, r *http.Request) {
	bookID, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		http.NotFound(w, r)
		return
	}
	if err := r.ParseForm(); err != nil {
		http.Error(w, "bad form", http.StatusBadRequest)
		return
	}
	next := safeNext(r.FormValue("next"))

	username, _ := auth.UsernameFromContext(r.Context())
	shelfID, err := s.DB.EnsureSystemShelf(username, favoritesSlug, favoritesName)
	if err != nil {
		http.Error(w, "failed to update favourites", http.StatusInternalServerError)
		return
	}

	onShelf, err := s.DB.IsBookOnShelf(shelfID, bookID)
	if err != nil {
		http.Error(w, "failed to update favourites", http.StatusInternalServerError)
		return
	}
	if onShelf {
		err = s.DB.RemoveBookFromShelf(shelfID, bookID)
	} else {
		err = s.DB.AddBookToShelf(shelfID, bookID)
	}
	if err != nil {
		http.Error(w, "failed to update favourites", http.StatusInternalServerError)
		return
	}

	http.Redirect(w, r, next, http.StatusSeeOther)
}

func (s *Server) renderNameIndex(w http.ResponseWriter, r *http.Request, heading, linkBase string, list func() ([]index.NameCount, error)) {
	items, err := list()
	if err != nil {
		http.Error(w, "failed to load list", http.StatusInternalServerError)
		return
	}

	username, isAdmin := s.viewerInfo(r)

	render(w, "name_index.html", map[string]any{
		"Title":    heading,
		"Heading":  heading,
		"Items":    items,
		"LinkBase": linkBase,
		"Username": username,
		"IsAdmin":  isAdmin,
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

func (s *Server) AdminUsers(w http.ResponseWriter, r *http.Request) {
	list, err := s.Users.List()
	if err != nil {
		http.Error(w, "failed to load users", http.StatusInternalServerError)
		return
	}

	username, isAdmin := s.viewerInfo(r)

	render(w, "admin_users.html", map[string]any{
		"Title":    "Manage Users",
		"Users":    list,
		"Username": username,
		"IsAdmin":  isAdmin,
	})
}

func (s *Server) renderAdminUsersError(w http.ResponseWriter, r *http.Request, errMsg string) {
	list, _ := s.Users.List()
	username, isAdmin := s.viewerInfo(r)
	render(w, "admin_users.html", map[string]any{
		"Title":    "Manage Users",
		"Users":    list,
		"Error":    errMsg,
		"Username": username,
		"IsAdmin":  isAdmin,
	})
}

func (s *Server) AdminUsersCreate(w http.ResponseWriter, r *http.Request) {
	if err := r.ParseForm(); err != nil {
		http.Error(w, "bad form", http.StatusBadRequest)
		return
	}

	username := r.FormValue("username")
	password := r.FormValue("password")
	isAdmin := r.FormValue("is_admin") == "on"

	if err := s.Users.Create(username, password, isAdmin); err != nil {
		s.renderAdminUsersError(w, r, err.Error())
		return
	}

	http.Redirect(w, r, "/admin/users", http.StatusSeeOther)
}

func (s *Server) AdminUsersDelete(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		http.NotFound(w, r)
		return
	}

	if err := s.Users.Delete(id); err != nil {
		s.renderAdminUsersError(w, r, err.Error())
		return
	}

	http.Redirect(w, r, "/admin/users", http.StatusSeeOther)
}

func (s *Server) AdminUsersResetPassword(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		http.NotFound(w, r)
		return
	}
	if err := r.ParseForm(); err != nil {
		http.Error(w, "bad form", http.StatusBadRequest)
		return
	}

	if err := s.Users.ResetPassword(id, r.FormValue("password")); err != nil {
		s.renderAdminUsersError(w, r, err.Error())
		return
	}

	http.Redirect(w, r, "/admin/users", http.StatusSeeOther)
}
