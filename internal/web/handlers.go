package web

import (
	"fmt"
	"log"
	"net/http"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"time"

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
	DataDir     string
	PageSize    int
	SiteName    string
	StartedAt   time.Time
}

const favoritesSlug = "favourites"
const favoritesName = "Favourites"

// baseData returns template data common to every page that renders the
// shared topbar (Username, IsAdmin, Shelves, SiteName, CurrentURL), ready
// to be merged into a handler's page-specific map via mergeInto. shelves is
// also returned directly for callers (renderBookList) that need to iterate
// it further, e.g. to compute per-book shelf membership.
func (s *Server) baseData(r *http.Request) (data map[string]any, shelves []index.Shelf, err error) {
	username, _ := auth.UsernameFromContext(r.Context())
	isAdmin := false
	if username != "" {
		isAdmin = s.Users.IsAdmin(username)
		if _, err = s.DB.EnsureSystemShelf(username, favoritesSlug, favoritesName); err != nil {
			return nil, nil, err
		}
		if shelves, err = s.DB.ListShelves(username); err != nil {
			return nil, nil, err
		}
	}

	data = map[string]any{
		"Username":   username,
		"IsAdmin":    isAdmin,
		"Shelves":    shelves,
		"SiteName":   s.SiteName,
		"CurrentURL": r.URL.RequestURI(),
	}
	return data, shelves, nil
}

// mergeInto copies every key from src into dst, overwriting on conflict.
func mergeInto(dst, src map[string]any) {
	for k, v := range src {
		dst[k] = v
	}
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
	base, _, err := s.baseData(r)
	if err != nil {
		http.Error(w, "failed to load page", http.StatusInternalServerError)
		return
	}
	data := map[string]any{
		"Title": "Log in",
		"Next":  safeNext(r.URL.Query().Get("next")),
	}
	mergeInto(data, base)
	render(w, "login.html", data)
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
		base, _, _ := s.baseData(r)
		data := map[string]any{
			"Title": "Log in",
			"Error": "Incorrect username or password.",
			"Next":  next,
		}
		mergeInto(data, base)
		render(w, "login.html", data)
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
	action         string       // form action / link base path, e.g. "/" or "/authors"
	name           string       // carried through as a hidden "name" param on filtered views
	filter         index.Filter // Author/Series (if any) pre-set by the caller; Search is filled in from the request
	heading        string
	defaultSort    index.SortKey
	filtered       bool   // show the "back to library" link
	viewingShelfID int64  // set only when viewing a specific shelf (e.g. Favourites); swaps the "+" for a "-"-with-confirm
	shelfName      string // display name for the remove-confirm modal, paired with viewingShelfID
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

	base, shelves, err := s.baseData(r)
	if err != nil {
		http.Error(w, "failed to load shelves", http.StatusInternalServerError)
		return
	}
	memberships := map[int64]map[int64]bool{}
	for _, sh := range shelves {
		ids, err := s.DB.ShelfBookIDs(sh.ID)
		if err != nil {
			http.Error(w, "failed to load shelves", http.StatusInternalServerError)
			return
		}
		memberships[sh.ID] = ids
	}

	data := map[string]any{
		"Title":            p.heading,
		"Heading":          p.heading,
		"Books":            books,
		"Sort":             sortParam,
		"Dir":              dir,
		"ToggleDir":        toggleDir,
		"Page":             page,
		"PrevPage":         page - 1,
		"NextPage":         page + 1,
		"HasNext":          page < totalPages,
		"TotalPages":       totalPages,
		"Query":            search,
		"Action":           p.action,
		"Name":             p.name,
		"Filtered":         p.filtered,
		"ShelfMemberships": memberships,
		"ViewingShelfID":   p.viewingShelfID,
		"ShelfName":        p.shelfName,
	}
	mergeInto(data, base)
	render(w, "library.html", data)
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
		action:         "/favorites",
		filter:         index.Filter{ShelfID: shelfID},
		heading:        favoritesName,
		defaultSort:    index.SortTitle,
		filtered:       true,
		viewingShelfID: shelfID,
		shelfName:      favoritesName,
	})
}

// ShelfToggle adds or removes a book from one of the current user's shelves,
// then redirects back to wherever the request came from. Ownership of the
// shelf is checked — a shelf id alone doesn't prove it belongs to this user.
func (s *Server) ShelfToggle(w http.ResponseWriter, r *http.Request) {
	bookID, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		http.NotFound(w, r)
		return
	}
	shelfID, err := strconv.ParseInt(r.PathValue("shelfID"), 10, 64)
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
	shelf, err := s.DB.GetShelf(shelfID)
	if err != nil {
		http.Error(w, "failed to update shelf", http.StatusInternalServerError)
		return
	}
	if shelf == nil || shelf.Username != username {
		http.NotFound(w, r)
		return
	}

	onShelf, err := s.DB.IsBookOnShelf(shelfID, bookID)
	if err != nil {
		http.Error(w, "failed to update shelf", http.StatusInternalServerError)
		return
	}
	if onShelf {
		err = s.DB.RemoveBookFromShelf(shelfID, bookID)
	} else {
		err = s.DB.AddBookToShelf(shelfID, bookID)
	}
	if err != nil {
		http.Error(w, "failed to update shelf", http.StatusInternalServerError)
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

	base, _, err := s.baseData(r)
	if err != nil {
		http.Error(w, "failed to load page", http.StatusInternalServerError)
		return
	}

	data := map[string]any{
		"Title":    heading,
		"Heading":  heading,
		"Items":    items,
		"LinkBase": linkBase,
	}
	mergeInto(data, base)
	render(w, "name_index.html", data)
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

	base, _, err := s.baseData(r)
	if err != nil {
		http.Error(w, "failed to load page", http.StatusInternalServerError)
		return
	}

	data := map[string]any{
		"Title": "Manage Users",
		"Users": list,
	}
	mergeInto(data, base)
	render(w, "admin_users.html", data)
}

func (s *Server) renderAdminUsersError(w http.ResponseWriter, r *http.Request, errMsg string) {
	list, _ := s.Users.List()
	base, _, _ := s.baseData(r)
	data := map[string]any{
		"Title": "Manage Users",
		"Users": list,
		"Error": errMsg,
	}
	mergeInto(data, base)
	render(w, "admin_users.html", data)
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

// ServerInfo shows admin-only read-only server/library stats.
func (s *Server) ServerInfo(w http.ResponseWriter, r *http.Request) {
	base, _, err := s.baseData(r)
	if err != nil {
		http.Error(w, "failed to load page", http.StatusInternalServerError)
		return
	}

	bookCount, err := s.DB.Count(index.Filter{})
	if err != nil {
		http.Error(w, "failed to load stats", http.StatusInternalServerError)
		return
	}
	authors, err := s.DB.ListAuthors()
	if err != nil {
		http.Error(w, "failed to load stats", http.StatusInternalServerError)
		return
	}
	series, err := s.DB.ListSeries()
	if err != nil {
		http.Error(w, "failed to load stats", http.StatusInternalServerError)
		return
	}
	userList, err := s.Users.List()
	if err != nil {
		http.Error(w, "failed to load stats", http.StatusInternalServerError)
		return
	}
	adminCount := 0
	for _, u := range userList {
		if u.IsAdmin {
			adminCount++
		}
	}

	var lastScanAt string
	if v, ok, _ := s.DB.GetMeta("last_scan_at"); ok {
		if unix, err := strconv.ParseInt(v, 10, 64); err == nil {
			lastScanAt = time.Unix(unix, 0).Format("2006-01-02 15:04:05 MST")
		}
	}
	var lastScanDurationMs string
	if v, ok, _ := s.DB.GetMeta("last_scan_duration_ms"); ok {
		lastScanDurationMs = v
	}

	data := map[string]any{
		"Title":              "Manage Server",
		"GoVersion":          runtime.Version(),
		"Uptime":             time.Since(s.StartedAt).Round(time.Second).String(),
		"LibraryPath":        s.LibraryPath,
		"DataDir":            s.DataDir,
		"BookCount":          bookCount,
		"AuthorCount":        len(authors),
		"SeriesCount":        len(series),
		"UserCount":          len(userList),
		"AdminCount":         adminCount,
		"LastScanAt":         lastScanAt,
		"LastScanDurationMs": lastScanDurationMs,
	}
	mergeInto(data, base)
	render(w, "admin_server.html", data)
}

// ServerRescan triggers a synchronous full library rescan, then returns to the server info page.
func (s *Server) ServerRescan(w http.ResponseWriter, r *http.Request) {
	if err := s.DB.Scan(s.LibraryPath, s.Covers); err != nil {
		http.Error(w, "rescan failed: "+err.Error(), http.StatusInternalServerError)
		return
	}
	http.Redirect(w, r, "/admin/server", http.StatusSeeOther)
}
