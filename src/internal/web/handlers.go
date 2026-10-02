package web

import (
	"fmt"
	"log"
	"net/http"
	"net/url"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"time"

	"github.com/sethwv/my-sideload-library/internal/auth"
	"github.com/sethwv/my-sideload-library/internal/chaptarr"
	"github.com/sethwv/my-sideload-library/internal/hardcover"
	"github.com/sethwv/my-sideload-library/internal/index"
	"github.com/sethwv/my-sideload-library/internal/kepub"
	"github.com/sethwv/my-sideload-library/internal/mail"
	"github.com/sethwv/my-sideload-library/internal/tasks"
	"github.com/sethwv/my-sideload-library/internal/thumbnail"
	"github.com/sethwv/my-sideload-library/internal/users"
)

type Server struct {
	Auth         *auth.Authenticator
	DB           *index.DB
	Covers       *thumbnail.Store
	Users        *users.Store
	Hardcover    *hardcover.Client // nil-safe: Enabled() is false with no token, callers check before use
	Chaptarr     *chaptarr.Client  // nil-safe: Enabled() is false with no URL/key, callers check before use
	LibraryPaths []string
	DataDir      string
	PageSize     int
	SiteName     string
	PublicURL    string // trusted base URL for emailed links; see config.Config.PublicURL
	StartedAt    time.Time
	BuildVersion string
	BuildDate    string
	Tasks        *tasks.Manager
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
	canManageUsers := false
	canManageServer := false
	canBookmark := false
	if username != "" {
		// A restricted (bookmark-token) session shouldn't be offered admin
		// links even if the account has them, since reaching /admin/* still
		// redirects to a password step-up regardless, but hiding the links
		// keeps the UI honest about the "view & shelves only" state.
		full := !auth.IsRestricted(r.Context())
		isAdmin = s.Users.IsAdmin(username) && full
		canManageUsers = s.Users.CanManageUsers(username) && full
		canManageServer = s.Users.CanManageServer(username) && full
		canBookmark = s.Users.CanUseBookmark(username)
		if _, err = s.DB.EnsureSystemShelf(username, favoritesSlug, favoritesName); err != nil {
			return nil, nil, err
		}
		if shelves, err = s.DB.ListShelves(username); err != nil {
			return nil, nil, err
		}
	}
	kepubSettings := users.KepubSettings{Enabled: true}
	if s.Users != nil {
		kepubSettings, err = s.Users.GetKepubSettings()
		if err != nil {
			return nil, nil, err
		}
	}

	data = map[string]any{
		"Username":        username,
		"IsAdmin":         isAdmin,
		"CanManageUsers":  canManageUsers,
		"CanManageServer": canManageServer,
		"CanBookmark":     canBookmark,
		"Restricted":      auth.IsRestricted(r.Context()),
		"Shelves":         shelves,
		"SiteName":        s.SiteName,
		"CurrentURL":      r.URL.RequestURI(),
		"BuildVersion":    s.BuildVersion,
		"BuildDate":       s.BuildDate,
		"KepubEnabled":    kepubSettings.Enabled,
	}
	return data, shelves, nil
}

// mergeInto copies every key from src into dst, overwriting on conflict.
func mergeInto(dst, src map[string]any) {
	for k, v := range src {
		dst[k] = v
	}
}

// safeNext restricts post-login redirect targets to same-site relative paths.
func safeNext(next string) string {
	// Browsers treat backslashes as path separators in URLs, so normalize them
	// before checking for protocol-relative URLs such as /\example.com.
	next = strings.ReplaceAll(next, "\\", "/")
	u, err := url.Parse(next)
	if err != nil || u.Scheme != "" || u.Host != "" || u.User != nil || !strings.HasPrefix(u.Path, "/") || strings.HasPrefix(u.Path, "//") {
		return "/"
	}
	return u.String()
}

func (s *Server) LoginPage(w http.ResponseWriter, r *http.Request) {
	hasUsers, err := s.Users.HasUsers()
	if err != nil {
		http.Error(w, "failed to load users", http.StatusInternalServerError)
		return
	}
	if !hasUsers {
		http.Redirect(w, r, "/setup", http.StatusSeeOther)
		return
	}
	base, _, err := s.baseData(r)
	if err != nil {
		http.Error(w, "failed to load page", http.StatusInternalServerError)
		return
	}
	data := map[string]any{
		"Title":          "Log in",
		"Next":           safeNext(r.URL.Query().Get("next")),
		"EmailAvailable": s.emailAvailable(),
	}
	mergeInto(data, base)
	render(w, "login.html", data)
}

func (s *Server) LoginSubmit(w http.ResponseWriter, r *http.Request) {
	hasUsers, err := s.Users.HasUsers()
	if err != nil {
		http.Error(w, "failed to load users", http.StatusInternalServerError)
		return
	}
	if !hasUsers {
		http.Redirect(w, r, "/setup", http.StatusSeeOther)
		return
	}
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
			"Title":          "Log in",
			"Error":          "Incorrect username or password.",
			"Next":           next,
			"EmailAvailable": s.emailAvailable(),
		}
		mergeInto(data, base)
		render(w, "login.html", data)
		return
	}

	s.Auth.IssueSession(w, r, username)

	target, err := url.Parse(next)
	if err != nil || target.Scheme != "" || target.Hostname() != "" || target.User != nil || !strings.HasPrefix(target.Path, "/") || strings.HasPrefix(target.Path, "//") {
		target = &url.URL{Path: "/"}
	}
	http.Redirect(w, r, target.String(), http.StatusSeeOther)
}

func (s *Server) emailAvailable() bool {
	settings, err := s.Users.GetSMTPSettings()
	return err == nil && settings.Enabled() && s.PublicURL != ""
}

func (s *Server) smtpAvailable() bool {
	settings, err := s.Users.GetSMTPSettings()
	return err == nil && settings.Enabled()
}

func emailUnavailableMessage() string {
	return "Email is unavailable. Configure SMTP and the Public URL in Server settings."
}

func (s *Server) SetupPage(w http.ResponseWriter, r *http.Request) {
	hasUsers, err := s.Users.HasUsers()
	if err != nil {
		http.Error(w, "failed to load users", http.StatusInternalServerError)
		return
	}
	if hasUsers {
		http.Redirect(w, r, "/login", http.StatusSeeOther)
		return
	}
	base, _, err := s.baseData(r)
	if err != nil {
		http.Error(w, "failed to load page", http.StatusInternalServerError)
		return
	}
	data := map[string]any{"Title": "Set up administrator"}
	mergeInto(data, base)
	render(w, "setup.html", data)
}

func (s *Server) SetupSubmit(w http.ResponseWriter, r *http.Request) {
	if err := r.ParseForm(); err != nil {
		http.Error(w, "bad form", http.StatusBadRequest)
		return
	}
	username := r.FormValue("username")
	password := r.FormValue("password")
	created, err := s.Users.CreateFirstAdmin(username, password)
	if err != nil {
		base, _, _ := s.baseData(r)
		data := map[string]any{"Title": "Set up administrator", "Error": err.Error(), "Username": username}
		mergeInto(data, base)
		render(w, "setup.html", data)
		return
	}
	if !created {
		http.Redirect(w, r, "/login", http.StatusSeeOther)
		return
	}
	s.Auth.IssueSession(w, r, username)
	http.Redirect(w, r, "/", http.StatusSeeOther)
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
		defaultSort: index.SortAdded,
	})
}

var validSortParams = map[string]bool{"title": true, "author": true, "series": true, "added": true, "released": true}

// defaultDirFor picks the direction a sort key opens in when the request
// doesn't specify one: date-based sorts read newest-first by default,
// everything else reads A-Z.
func defaultDirFor(sort index.SortKey) string {
	if sort == index.SortAdded || sort == index.SortReleased {
		return "desc"
	}
	return "asc"
}

// bookListParams configures one call to renderBookList: the shared
// sort/page/search machinery behind the library grid and every filtered
// view (author, series, and future ones like shelves/favorites).
type bookListParams struct {
	action         string       // form action / link base path, e.g. "/" or "/authors"
	name           string       // carried through as a hidden "name" param on filtered views
	filter         index.Filter // Author/Series (if any) pre-set by the caller; Search is filled in from the request
	heading        string
	defaultSort    index.SortKey
	viewingShelfID int64 // set only when viewing a specific shelf (e.g. Favourites); its shelf-toggle button removes the book from the page instead of just flipping the checkmark
}

// hideMatchFilter returns a Filter carrying just the current admin-configured
// hide-no-match settings, read fresh on every call (not cached on Server) so
// a save on the Integrations page takes effect on the very next request.
func (s *Server) hideMatchFilter() index.Filter {
	settings, err := s.Users.GetIntegrationSettings()
	if err != nil {
		return index.Filter{}
	}
	return index.Filter{
		HideNoChaptarrMatch:  settings.HideNoChaptarrMatch,
		HideNoHardcoverMatch: settings.HideNoHardcoverMatch,
	}
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
		dir = defaultDirFor(sort)
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
	hide := s.hideMatchFilter()
	filter.HideNoChaptarrMatch = hide.HideNoChaptarrMatch
	filter.HideNoHardcoverMatch = hide.HideNoHardcoverMatch

	total, err := s.DB.Count(filter)
	if err != nil {
		http.Error(w, "failed to load library", http.StatusInternalServerError)
		return
	}

	totalPages := (total + pageSize - 1) / pageSize
	if totalPages < 1 {
		totalPages = 1
	}
	if page > totalPages {
		page = totalPages
	}
	pages := make([]int, totalPages)
	for i := range pages {
		pages[i] = i + 1
	}

	books, err := s.DB.List(sort, descending, page, pageSize, filter)
	if err != nil {
		http.Error(w, "failed to load library", http.StatusInternalServerError)
		return
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

	ids := make([]int64, len(books))
	for i, b := range books {
		ids[i] = b.ID
	}
	locations, err := s.DB.LocationsForBooks(ids)
	if err != nil {
		http.Error(w, "failed to load library", http.StatusInternalServerError)
		return
	}
	for _, b := range books {
		locations[b.ID] = append([]index.Location{{LibraryRoot: b.LibraryRoot, FilePath: b.FilePath}}, locations[b.ID]...)
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
		"Pages":            pages,
		"Query":            search,
		"Action":           p.action,
		"Name":             p.name,
		"ShelfMemberships": memberships,
		"ViewingShelfID":   p.viewingShelfID,
		"Locations":        locations,
	}
	mergeInto(data, base)
	render(w, "library.html", data)
}

// AuthorsHandler is dual-mode: with no ?name=, it lists every author (browse
// index); with ?name=, it shows that author's books via renderBookList.
func (s *Server) AuthorsHandler(w http.ResponseWriter, r *http.Request) {
	name := r.URL.Query().Get("name")
	if name == "" {
		s.renderNameIndex(w, r, "Authors", "/authors", func() ([]index.NameCount, error) {
			return s.DB.ListAuthors(s.hideMatchFilter())
		})
		return
	}
	s.renderBookList(w, r, bookListParams{
		action:      "/authors",
		name:        name,
		filter:      index.Filter{Author: name},
		heading:     "Books by " + name,
		defaultSort: index.SortTitle,
	})
}

// SeriesHandler is dual-mode: with no ?name=, it lists every series (browse
// index); with ?name=, it shows that series' books via renderBookList.
func (s *Server) SeriesHandler(w http.ResponseWriter, r *http.Request) {
	name := r.URL.Query().Get("name")
	if name == "" {
		s.renderNameIndex(w, r, "Series", "/series", func() ([]index.NameCount, error) {
			return s.DB.ListSeries(s.hideMatchFilter())
		})
		return
	}
	s.renderBookList(w, r, bookListParams{
		action:      "/series",
		name:        name,
		filter:      index.Filter{Series: name},
		heading:     "Series: " + name,
		defaultSort: index.SortSeries,
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
		viewingShelfID: shelfID,
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

	target, err := url.Parse(next)
	if err != nil || target.Hostname() != "" {
		target = &url.URL{Path: "/"}
	}
	http.Redirect(w, r, target.String(), http.StatusSeeOther)
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

	fullPath := filepath.Join(book.LibraryRoot, book.FilePath)
	filename := filenameFor(book.Title, book.Author, "epub")

	w.Header().Set("Content-Disposition", fmt.Sprintf(`attachment; filename="%s"`, filename))
	http.ServeFile(w, r, fullPath)
}

// DownloadKepub converts the EPUB to kepub on the fly and streams it. Not cached to disk.
func (s *Server) DownloadKepub(w http.ResponseWriter, r *http.Request) {
	settings, err := s.Users.GetKepubSettings()
	if err != nil {
		http.Error(w, "failed to load KEPUB settings", http.StatusInternalServerError)
		return
	}
	if !settings.Enabled {
		http.NotFound(w, r)
		return
	}

	book, ok := s.lookupBook(w, r)
	if !ok {
		return
	}

	fullPath := filepath.Join(book.LibraryRoot, book.FilePath)
	filename := filenameFor(book.Title, book.Author, "kepub.epub")

	w.Header().Set("Content-Type", "application/epub+zip")
	w.Header().Set("Content-Disposition", fmt.Sprintf(`attachment; filename="%s"`, filename))

	var metadata *kepub.Metadata
	if settings.WriteCalibreMetadata {
		metadata = &kepub.Metadata{Series: book.Series, SeriesIndex: book.SeriesIndex}
	}
	if err := kepub.ConvertFile(r.Context(), w, fullPath, metadata); err != nil {
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
		"Title":          "Manage Users",
		"AdminTab":       "users",
		"Users":          list,
		"EmailAvailable": s.emailAvailable(),
		"EmailMessage":   emailUnavailableMessage(),
	}
	mergeInto(data, base)
	render(w, "admin_users.html", data)
}

func (s *Server) renderAdminUsersError(w http.ResponseWriter, r *http.Request, errMsg string) {
	list, _ := s.Users.List()
	base, _, _ := s.baseData(r)
	data := map[string]any{
		"Title":          "Manage Users",
		"AdminTab":       "users",
		"Users":          list,
		"Error":          errMsg,
		"EmailAvailable": s.emailAvailable(),
		"EmailMessage":   emailUnavailableMessage(),
	}
	mergeInto(data, base)
	render(w, "admin_users.html", data)
}

func (s *Server) AdminUsersSetEnabled(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		http.NotFound(w, r)
		return
	}
	if err := r.ParseForm(); err != nil {
		http.Error(w, "bad form", http.StatusBadRequest)
		return
	}
	if err := s.Users.SetEnabled(id, r.FormValue("enabled") == "true"); err != nil {
		s.renderAdminUsersError(w, r, err.Error())
		return
	}
	http.Redirect(w, r, "/admin/users", http.StatusSeeOther)
}

func (s *Server) AdminUsersCreate(w http.ResponseWriter, r *http.Request) {
	if err := r.ParseForm(); err != nil {
		http.Error(w, "bad form", http.StatusBadRequest)
		return
	}

	username := r.FormValue("username")
	password := r.FormValue("password")
	role := r.FormValue("role")
	canBookmark := r.FormValue("can_bookmark") == "on"
	email := r.FormValue("email")

	if err := s.Users.Create(username, password, role, canBookmark, email); err != nil {
		s.renderAdminUsersError(w, r, err.Error())
		return
	}

	http.Redirect(w, r, "/admin/users", http.StatusSeeOther)
}

// AdminUsersSetRole updates an existing user's role and bookmark-link
// permission in place, without deleting/recreating the account.
func (s *Server) AdminUsersSetRole(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		http.NotFound(w, r)
		return
	}
	if err := r.ParseForm(); err != nil {
		http.Error(w, "bad form", http.StatusBadRequest)
		return
	}

	role := r.FormValue("role")
	canBookmark := r.FormValue("can_bookmark") == "on"

	if err := s.Users.SetRole(id, role, canBookmark); err != nil {
		s.renderAdminUsersError(w, r, err.Error())
		return
	}

	http.Redirect(w, r, "/admin/users", http.StatusSeeOther)
}

// AdminUsersSetEmail updates an existing user's email address in place.
func (s *Server) AdminUsersSetEmail(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		http.NotFound(w, r)
		return
	}
	if err := r.ParseForm(); err != nil {
		http.Error(w, "bad form", http.StatusBadRequest)
		return
	}

	if err := s.Users.SetEmail(id, r.FormValue("email")); err != nil {
		s.renderAdminUsersError(w, r, err.Error())
		return
	}

	http.Redirect(w, r, "/admin/users", http.StatusSeeOther)
}

// AdminUsersInvite creates a pending account and emails the invitee a link
// to set their own password.
func (s *Server) AdminUsersInvite(w http.ResponseWriter, r *http.Request) {
	if err := r.ParseForm(); err != nil {
		http.Error(w, "bad form", http.StatusBadRequest)
		return
	}
	if !s.emailAvailable() {
		s.renderAdminUsersError(w, r, emailUnavailableMessage())
		return
	}

	username := r.FormValue("username")
	email := r.FormValue("email")
	role := r.FormValue("role")

	token, err := s.Users.InviteUser(username, email, role, true)
	if err != nil {
		s.renderAdminUsersError(w, r, err.Error())
		return
	}

	if err := s.sendInviteEmail(r, email, token); err != nil {
		log.Printf("send invite email to %s: %v", email, err)
		s.renderAdminUsersError(w, r, "user created, but the invite email failed to send: "+err.Error())
		return
	}

	http.Redirect(w, r, "/admin/users", http.StatusSeeOther)
}

// AdminUsersResendInvite reissues a fresh invite token/email for a user
// whose invite is still pending.
func (s *Server) AdminUsersResendInvite(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		http.NotFound(w, r)
		return
	}
	if !s.emailAvailable() {
		s.renderAdminUsersError(w, r, emailUnavailableMessage())
		return
	}

	user, err := s.Users.UserByID(id)
	if err != nil {
		http.Error(w, "failed to load users", http.StatusInternalServerError)
		return
	}
	if user == nil || user.Email == "" {
		s.renderAdminUsersError(w, r, "user has no email on file")
		return
	}

	token, err := s.Users.ResendInvite(id)
	if err != nil {
		s.renderAdminUsersError(w, r, err.Error())
		return
	}
	if err := s.sendInviteEmail(r, user.Email, token); err != nil {
		log.Printf("resend invite email to %s: %v", user.Email, err)
		s.renderAdminUsersError(w, r, "invite reissued, but the email failed to send: "+err.Error())
		return
	}

	http.Redirect(w, r, "/admin/users", http.StatusSeeOther)
}

func (s *Server) sendInviteEmail(r *http.Request, to, token string) error {
	settings, err := s.Users.GetSMTPSettings()
	if err != nil {
		return err
	}
	if !settings.Enabled() {
		return fmt.Errorf("SMTP is not configured")
	}
	origin, err := s.emailOrigin()
	if err != nil {
		return err
	}
	link := origin + "/invite/accept?token=" + token
	body := fmt.Sprintf(
		"You've been invited to %s.\n\nSet your password to finish creating your account:\n%s\n\nThis link expires in 7 days.",
		s.SiteName, link,
	)
	return mail.Send(settings, to, "You're invited to "+s.SiteName, body)
}

// emailOrigin returns the base URL to use when building a link that leaves
// the server via email (password reset, invite). Unlike siteOrigin, this
// never falls back to the request's Host header: Host is client-controlled,
// and a spoofed Host on an unauthenticated request like /forgot-password
// would otherwise let an attacker put their own domain into a victim's
// password-reset email. Refuses to send rather than guess.
func (s *Server) emailOrigin() (string, error) {
	if s.PublicURL == "" {
		return "", fmt.Errorf("PUBLIC_URL is not configured, refusing to send an email with a login link")
	}
	return s.PublicURL, nil
}

// siteOrigin returns the base URL to use for a link handed straight back to
// the same browser that requested it (e.g. a bookmark-link redirect) rather
// than emailed elsewhere. Falling back to the request's Host header is safe
// here since the result only ever reaches the request's own client, never a
// third party's inbox; still prefers the admin-configured PublicURL when set.
func (s *Server) siteOrigin(r *http.Request) string {
	if s.PublicURL != "" {
		return s.PublicURL
	}
	scheme := "http"
	if r.TLS != nil {
		scheme = "https"
	}
	return scheme + "://" + r.Host
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
func (s *Server) serverInfoData() (map[string]any, error) {
	bookCount, err := s.DB.Count(index.Filter{})
	if err != nil {
		return nil, err
	}
	authors, err := s.DB.ListAuthors(index.Filter{})
	if err != nil {
		return nil, err
	}
	series, err := s.DB.ListSeries(index.Filter{})
	if err != nil {
		return nil, err
	}
	userList, err := s.Users.List()
	if err != nil {
		return nil, err
	}
	adminCount := 0
	for _, u := range userList {
		if u.IsAdmin {
			adminCount++
		}
	}

	var memory runtime.MemStats
	runtime.ReadMemStats(&memory)
	memoryPercent := 0
	if memory.Sys > 0 {
		memoryPercent = int(memory.HeapAlloc * 100 / memory.Sys)
	}

	return map[string]any{
		"Title":           "Manage Server",
		"AdminTab":        "server",
		"Uptime":          time.Since(s.StartedAt).Round(time.Second).String(),
		"Platform":        runtime.GOOS + "/" + runtime.GOARCH,
		"BuildVersion":    s.BuildVersion,
		"BuildDate":       s.BuildDate,
		"GoVersion":       runtime.Version(),
		"Goroutines":      runtime.NumGoroutine(),
		"MemoryAllocated": formatBytes(memory.HeapAlloc),
		"MemoryReserved":  formatBytes(memory.Sys),
		"MemoryPercent":   memoryPercent,
		"LibraryPath":     strings.Join(s.LibraryPaths, ", "),
		"DataDir":         s.DataDir,
		"BookCount":       bookCount,
		"AuthorCount":     len(authors),
		"SeriesCount":     len(series),
		"UserCount":       len(userList),
		"AdminCount":      adminCount,
	}, nil
}

func formatBytes(value uint64) string {
	if value < 1024 {
		return strconv.FormatUint(value, 10) + " B"
	}

	units := []string{"KiB", "MiB", "GiB", "TiB"}
	size := float64(value)
	unit := -1
	for size >= 1024 && unit < len(units)-1 {
		size /= 1024
		unit++
	}
	return fmt.Sprintf("%.1f %s", size, units[unit])
}

func (s *Server) adminSettingsData() (map[string]any, error) {
	general, err := s.Users.GetGeneralSettings()
	if err != nil {
		return nil, err
	}
	kepubSettings, err := s.Users.GetKepubSettings()
	if err != nil {
		return nil, err
	}

	return map[string]any{
		"Title":                     "Setup",
		"AdminTab":                  "settings",
		"SiteName":                  general.SiteName,
		"PublicURL":                 general.PublicURL,
		"CoverWidth":                general.CoverWidth,
		"PageSize":                  general.PageSize,
		"SessionTTL":                general.SessionTTL.String(),
		"KepubEnabled":              kepubSettings.Enabled,
		"KepubWriteCalibreMetadata": kepubSettings.WriteCalibreMetadata,
	}, nil
}

func (s *Server) adminSMTPData() (map[string]any, error) {
	smtp, err := s.Users.GetSMTPSettings()
	if err != nil {
		return nil, err
	}

	return map[string]any{
		"Title":           "SMTP",
		"AdminTab":        "smtp",
		"SMTPHost":        smtp.Host,
		"SMTPPort":        smtp.Port,
		"SMTPEncryption":  smtp.Encryption,
		"SMTPUsername":    smtp.Username,
		"SMTPFromName":    smtp.FromName,
		"SMTPFromAddress": smtp.FromAddress,
		"SMTPConfigured":  smtp.Enabled(),
	}, nil
}

func (s *Server) serverIntegrationsData(provider string) (map[string]any, error) {
	settings, err := s.Users.GetIntegrationSettings()
	if err != nil {
		return nil, err
	}
	enrichmentStats, err := s.DB.GetEnrichmentStats()
	if err != nil {
		return nil, err
	}

	return map[string]any{
		"Title":                   "Enhancement",
		"AdminTab":                "integrations",
		"EnrichmentTab":           provider,
		"HardcoverEnabled":        settings.HardcoverEnabled,
		"HardcoverActive":         s.Hardcover.Enabled(),
		"HardcoverConfigured":     settings.HardcoverToken != "",
		"HideNoHardcoverMatch":    settings.HideNoHardcoverMatch,
		"HardcoverOverwriteCover": settings.HardcoverOverwriteCover,
		"ChaptarrEnabled":         settings.ChaptarrEnabled,
		"ChaptarrActive":          s.Chaptarr.Enabled(),
		"ChaptarrURL":             settings.ChaptarrURL,
		"ChaptarrConfigured":      settings.ChaptarrAPIKey != "",
		"HideNoChaptarrMatch":     settings.HideNoChaptarrMatch,
		"EnrichmentPending":       enrichmentStats.Pending,
		"EnrichmentDone":          enrichmentStats.Done,
		"EnrichmentNoMatch":       enrichmentStats.NoMatch,
		"EnrichmentErrored":       enrichmentStats.Errored,
	}, nil
}

func (s *Server) ServerInfo(w http.ResponseWriter, r *http.Request) {
	base, _, err := s.baseData(r)
	if err != nil {
		http.Error(w, "failed to load page", http.StatusInternalServerError)
		return
	}
	data, err := s.serverInfoData()
	if err != nil {
		http.Error(w, "failed to load stats", http.StatusInternalServerError)
		return
	}
	mergeInto(data, base)
	render(w, "admin_server.html", data)
}

func (s *Server) AdminSettings(w http.ResponseWriter, r *http.Request) {
	base, _, err := s.baseData(r)
	if err != nil {
		http.Error(w, "failed to load page", http.StatusInternalServerError)
		return
	}
	data, err := s.adminSettingsData()
	if err != nil {
		http.Error(w, "failed to load settings", http.StatusInternalServerError)
		return
	}
	mergeInto(data, base)
	render(w, "admin_settings.html", data)
}

func (s *Server) AdminSMTP(w http.ResponseWriter, r *http.Request) {
	base, _, err := s.baseData(r)
	if err != nil {
		http.Error(w, "failed to load page", http.StatusInternalServerError)
		return
	}
	data, err := s.adminSMTPData()
	if err != nil {
		http.Error(w, "failed to load settings", http.StatusInternalServerError)
		return
	}
	mergeInto(data, base)
	render(w, "admin_smtp.html", data)
}

func (s *Server) renderAdminSettingsError(w http.ResponseWriter, r *http.Request, errMsg, statusMsg string) {
	base, _, err := s.baseData(r)
	if err != nil {
		http.Error(w, "failed to load page", http.StatusInternalServerError)
		return
	}
	data, err := s.adminSettingsData()
	if err != nil {
		http.Error(w, "failed to load settings", http.StatusInternalServerError)
		return
	}
	if errMsg != "" {
		data["Error"] = errMsg
	}
	if statusMsg != "" {
		data["Status"] = statusMsg
	}
	mergeInto(data, base)
	render(w, "admin_settings.html", data)
}

func (s *Server) renderAdminSMTPError(w http.ResponseWriter, r *http.Request, errMsg, statusMsg string) {
	base, _, err := s.baseData(r)
	if err != nil {
		http.Error(w, "failed to load page", http.StatusInternalServerError)
		return
	}
	data, err := s.adminSMTPData()
	if err != nil {
		http.Error(w, "failed to load settings", http.StatusInternalServerError)
		return
	}
	if errMsg != "" {
		data["Error"] = errMsg
	}
	if statusMsg != "" {
		data["Status"] = statusMsg
	}
	mergeInto(data, base)
	render(w, "admin_smtp.html", data)
}

func (s *Server) AdminSettingsGeneralSave(w http.ResponseWriter, r *http.Request) {
	if err := r.ParseForm(); err != nil {
		http.Error(w, "bad form", http.StatusBadRequest)
		return
	}

	coverWidth, err := strconv.Atoi(r.FormValue("cover_width"))
	if err != nil || coverWidth < 1 {
		s.renderAdminSettingsError(w, r, "cover width must be a positive number", "")
		return
	}
	pageSize, err := strconv.Atoi(r.FormValue("page_size"))
	if err != nil || pageSize < 1 {
		s.renderAdminSettingsError(w, r, "page size must be a positive number", "")
		return
	}
	sessionTTL, err := time.ParseDuration(r.FormValue("session_ttl"))
	if err != nil || sessionTTL <= 0 {
		s.renderAdminSettingsError(w, r, "session TTL must be a valid duration like 720h", "")
		return
	}

	settings := users.GeneralSettings{
		SiteName:   r.FormValue("site_name"),
		PublicURL:  strings.TrimRight(r.FormValue("public_url"), "/"),
		CoverWidth: coverWidth,
		PageSize:   pageSize,
		SessionTTL: sessionTTL,
	}
	if err := s.Users.SaveGeneralSettings(settings); err != nil {
		s.renderAdminSettingsError(w, r, "failed to save settings: "+err.Error(), "")
		return
	}

	s.SiteName = settings.SiteName
	s.PublicURL = settings.PublicURL
	s.PageSize = settings.PageSize
	s.Covers.SetWidth(settings.CoverWidth)
	s.Auth.SetTTL(settings.SessionTTL)

	http.Redirect(w, r, "/admin/settings", http.StatusSeeOther)
}

func (s *Server) AdminSettingsKepubSave(w http.ResponseWriter, r *http.Request) {
	if err := r.ParseForm(); err != nil {
		http.Error(w, "bad form", http.StatusBadRequest)
		return
	}
	if err := s.Users.SaveKepubSettings(users.KepubSettings{
		Enabled:              r.FormValue("enabled") == "on",
		WriteCalibreMetadata: r.FormValue("write_calibre_metadata") == "on",
	}); err != nil {
		s.renderAdminSettingsError(w, r, "failed to save KEPUB settings: "+err.Error(), "")
		return
	}
	http.Redirect(w, r, "/admin/settings", http.StatusSeeOther)
}

func (s *Server) ServerSMTPSave(w http.ResponseWriter, r *http.Request) {
	if err := r.ParseForm(); err != nil {
		http.Error(w, "bad form", http.StatusBadRequest)
		return
	}

	current, err := s.Users.GetSMTPSettings()
	if err != nil {
		http.Error(w, "failed to load settings", http.StatusInternalServerError)
		return
	}

	port, _ := strconv.Atoi(r.FormValue("port"))
	password := r.FormValue("password")
	if password == "" {
		password = current.Password
	}

	settings := mail.Settings{
		Host:        r.FormValue("host"),
		Port:        port,
		Encryption:  r.FormValue("encryption"),
		Username:    r.FormValue("username"),
		Password:    password,
		FromName:    r.FormValue("from_name"),
		FromAddress: r.FormValue("from_addr"),
	}

	if err := s.Users.SaveSMTPSettings(settings); err != nil {
		s.renderAdminSMTPError(w, r, "failed to save SMTP settings: "+err.Error(), "")
		return
	}

	http.Redirect(w, r, "/admin/smtp", http.StatusSeeOther)
}

func (s *Server) ServerSMTPTest(w http.ResponseWriter, r *http.Request) {
	if err := r.ParseForm(); err != nil {
		http.Error(w, "bad form", http.StatusBadRequest)
		return
	}
	to := r.FormValue("test_email")

	settings, err := s.Users.GetSMTPSettings()
	if err != nil {
		http.Error(w, "failed to load settings", http.StatusInternalServerError)
		return
	}
	if !settings.Enabled() {
		s.renderAdminSMTPError(w, r, "SMTP is not configured yet. Save settings first.", "")
		return
	}

	body := fmt.Sprintf("This is a test email from %s, confirming your SMTP settings work.", s.SiteName)
	if err := mail.Send(settings, to, "Test email from "+s.SiteName, body); err != nil {
		s.renderAdminSMTPError(w, r, "test email failed: "+err.Error(), "")
		return
	}

	s.renderAdminSMTPError(w, r, "", "Test email sent to "+to+".")
}

func (s *Server) ServerRescan(w http.ResponseWriter, r *http.Request) {
	s.enqueueTask(w, r, "rescan")
}

func (s *Server) ServerReimport(w http.ResponseWriter, r *http.Request) {
	s.ServerLibraryClear(w, r)
}

// ServerLibraryClear clears derived library data, then queues a fresh scan.
func (s *Server) ServerLibraryClear(w http.ResponseWriter, r *http.Request) {
	if s.Tasks == nil {
		http.Error(w, "task manager is unavailable", http.StatusServiceUnavailable)
		return
	}
	if err := s.DB.ClearLibrary(); err != nil {
		http.Error(w, "clear library failed: "+err.Error(), http.StatusInternalServerError)
		return
	}
	if _, _, err := s.Tasks.Enqueue("rescan"); err != nil {
		http.Error(w, "scan could not be queued", http.StatusInternalServerError)
		return
	}
	http.Redirect(w, r, "/admin/server", http.StatusSeeOther)
}

func (s *Server) ServerEnrichmentReset(w http.ResponseWriter, r *http.Request) {
	if err := s.DB.ResetEnrichment(); err != nil {
		http.Error(w, "enrichment reset failed: "+err.Error(), http.StatusInternalServerError)
		return
	}
	http.Redirect(w, r, "/admin/integrations", http.StatusSeeOther)
}

func (s *Server) enqueueTask(w http.ResponseWriter, r *http.Request, key string) {
	if s.Tasks == nil {
		http.Error(w, "task manager is unavailable", http.StatusServiceUnavailable)
		return
	}
	_, _, err := s.Tasks.Enqueue(key)
	if err != nil {
		http.Error(w, "task could not be queued", http.StatusBadRequest)
		return
	}
	http.Redirect(w, r, "/admin/tasks", http.StatusSeeOther)
}

func (s *Server) AdminTaskRun(w http.ResponseWriter, r *http.Request) {
	s.enqueueTask(w, r, r.PathValue("task"))
}

func (s *Server) AdminTasks(w http.ResponseWriter, r *http.Request) {
	if s.Tasks == nil {
		http.Error(w, "task manager is unavailable", http.StatusServiceUnavailable)
		return
	}
	base, _, err := s.baseData(r)
	if err != nil {
		http.Error(w, "failed to load page", http.StatusInternalServerError)
		return
	}
	summaries, err := s.Tasks.Summaries()
	if err != nil {
		http.Error(w, "failed to load tasks", http.StatusInternalServerError)
		return
	}
	var jobs []tasks.Summary
	for _, summary := range summaries {
		if summary.Kind == tasks.KindService {
			continue
		}
		s.populateTaskNextRun(&summary)
		jobs = append(jobs, summary)
	}
	history, err := s.Tasks.History(10)
	if err != nil {
		http.Error(w, "failed to load tasks", http.StatusInternalServerError)
		return
	}
	data := map[string]any{"Title": "Tasks", "AdminTab": "tasks", "TaskPage": true, "Tasks": jobs, "History": history}
	mergeInto(data, base)
	render(w, "admin_tasks.html", data)
}

func (s *Server) populateTaskNextRun(summary *tasks.Summary) {
	switch summary.Key {
	case "chaptarr-refresh":
		if !s.Chaptarr.Enabled() {
			summary.NextRun = "Disabled"
			return
		}
		summary.NextRunAt = s.Chaptarr.NextRefreshAt()
		if summary.NextRunAt.IsZero() {
			summary.NextRun = "Pending initial refresh"
		}
		return
	case "email-digest":
	default:
		return
	}
	settings, err := s.Users.GetSMTPSettings()
	if err != nil || !settings.Enabled() {
		summary.NextRun = "Disabled"
		return
	}
	value, ok, err := s.DB.GetMeta(lastDigestMetaKey)
	if err != nil || !ok {
		return
	}
	lastSent, err := strconv.ParseInt(value, 10, 64)
	if err == nil {
		summary.NextRunAt = time.Unix(lastSent, 0).Add(digestInterval)
	}
}

func (s *Server) ServerIntegrations(w http.ResponseWriter, r *http.Request) {
	base, _, err := s.baseData(r)
	if err != nil {
		http.Error(w, "failed to load page", http.StatusInternalServerError)
		return
	}
	provider := r.URL.Query().Get("provider")
	if provider != "hardcover" {
		provider = "chaptarr"
	}
	data, err := s.serverIntegrationsData(provider)
	if err != nil {
		http.Error(w, "failed to load settings", http.StatusInternalServerError)
		return
	}
	mergeInto(data, base)
	render(w, "admin_integrations.html", data)
}

func (s *Server) renderAdminIntegrationsError(w http.ResponseWriter, r *http.Request, provider, errMsg, statusMsg string) {
	base, _, err := s.baseData(r)
	if err != nil {
		http.Error(w, "failed to load page", http.StatusInternalServerError)
		return
	}
	data, err := s.serverIntegrationsData(provider)
	if err != nil {
		http.Error(w, "failed to load settings", http.StatusInternalServerError)
		return
	}
	if errMsg != "" {
		data["Error"] = errMsg
	}
	if statusMsg != "" {
		data["Status"] = statusMsg
	}
	mergeInto(data, base)
	render(w, "admin_integrations.html", data)
}

func (s *Server) ServerIntegrationsHardcoverSave(w http.ResponseWriter, r *http.Request) {
	if err := r.ParseForm(); err != nil {
		http.Error(w, "bad form", http.StatusBadRequest)
		return
	}

	current, err := s.Users.GetIntegrationSettings()
	if err != nil {
		http.Error(w, "failed to load settings", http.StatusInternalServerError)
		return
	}

	token := r.FormValue("hardcover_token")
	if token == "" {
		token = current.HardcoverToken
	}
	current.HardcoverEnabled = r.FormValue("hardcover_enabled") == "on"
	current.HardcoverToken = token
	current.HideNoHardcoverMatch = r.FormValue("hide_no_hardcover_match") == "on"
	current.HardcoverOverwriteCover = r.FormValue("hardcover_overwrite_cover") == "on"

	if err := s.Users.SaveIntegrationSettings(current); err != nil {
		s.renderAdminIntegrationsError(w, r, "hardcover", "failed to save Hardcover settings: "+err.Error(), "")
		return
	}
	s.Hardcover.SetConfig(current.HardcoverEnabled, current.HardcoverToken)

	http.Redirect(w, r, "/admin/integrations?provider=hardcover", http.StatusSeeOther)
}

func (s *Server) ServerIntegrationsChaptarrSave(w http.ResponseWriter, r *http.Request) {
	if err := r.ParseForm(); err != nil {
		http.Error(w, "bad form", http.StatusBadRequest)
		return
	}

	current, err := s.Users.GetIntegrationSettings()
	if err != nil {
		http.Error(w, "failed to load settings", http.StatusInternalServerError)
		return
	}

	apiKey := r.FormValue("chaptarr_api_key")
	if apiKey == "" {
		apiKey = current.ChaptarrAPIKey
	}
	current.ChaptarrEnabled = r.FormValue("chaptarr_enabled") == "on"
	current.ChaptarrURL = r.FormValue("chaptarr_url")
	current.ChaptarrAPIKey = apiKey
	current.HideNoChaptarrMatch = r.FormValue("hide_no_chaptarr_match") == "on"

	if err := s.Users.SaveIntegrationSettings(current); err != nil {
		s.renderAdminIntegrationsError(w, r, "chaptarr", "failed to save Chaptarr settings: "+err.Error(), "")
		return
	}
	s.Chaptarr.SetConfig(current.ChaptarrEnabled, current.ChaptarrURL, current.ChaptarrAPIKey)

	http.Redirect(w, r, "/admin/integrations?provider=chaptarr", http.StatusSeeOther)
}

// AccountBookmark shows the current bookmark-token status and a button to
// create/regenerate one. 403s if the account's bookmark-link permission has
// been revoked by an admin.
func (s *Server) AccountBookmark(w http.ResponseWriter, r *http.Request) {
	base, _, err := s.baseData(r)
	if err != nil {
		http.Error(w, "failed to load page", http.StatusInternalServerError)
		return
	}
	username, _ := auth.UsernameFromContext(r.Context())
	if !s.Users.CanUseBookmark(username) {
		http.Error(w, "forbidden", http.StatusForbidden)
		return
	}

	data := map[string]any{
		"Title":    "Bookmark Link",
		"HasToken": s.Users.HasBookmarkToken(username),
	}
	mergeInto(data, base)
	render(w, "account_bookmark.html", data)
}

// AccountBookmarkRegenerate revokes any existing bookmark token, issues a
// new one, and redirects the browser straight to the tokened library URL
// (not back to an informational page) — copy/paste is unreliable on e-reader
// browsers, so the address bar itself needs to show the bookmarkable URL,
// ready for the browser's own "bookmark this page" action.
func (s *Server) AccountBookmarkRegenerate(w http.ResponseWriter, r *http.Request) {
	username, _ := auth.UsernameFromContext(r.Context())
	if !s.Users.CanUseBookmark(username) {
		http.Error(w, "forbidden", http.StatusForbidden)
		return
	}
	token, err := s.Users.GenerateBookmarkToken(username)
	if err != nil {
		http.Error(w, "failed to generate bookmark link", http.StatusInternalServerError)
		return
	}
	http.Redirect(w, r, s.bookmarkURL(r, token), http.StatusSeeOther)
}

// AccountPassword shows the self-service change-password form for a
// logged-in user.
func (s *Server) AccountPassword(w http.ResponseWriter, r *http.Request) {
	base, _, err := s.baseData(r)
	if err != nil {
		http.Error(w, "failed to load page", http.StatusInternalServerError)
		return
	}
	username, _ := auth.UsernameFromContext(r.Context())
	data := map[string]any{"Title": "Change Password", "DigestSubscribed": s.Users.IsDigestSubscribed(username), "EmailAvailable": s.smtpAvailable(), "EmailMessage": "Email is unavailable. Configure SMTP in Server settings."}
	mergeInto(data, base)
	render(w, "account_password.html", data)
}

// AccountPasswordSubmit changes the logged-in user's password after
// verifying their current one.
func (s *Server) AccountPasswordSubmit(w http.ResponseWriter, r *http.Request) {
	if err := r.ParseForm(); err != nil {
		http.Error(w, "bad form", http.StatusBadRequest)
		return
	}
	username, _ := auth.UsernameFromContext(r.Context())
	current := r.FormValue("current_password")
	newPassword := r.FormValue("new_password")

	renderErr := func(msg string) {
		base, _, err := s.baseData(r)
		if err != nil {
			http.Error(w, "failed to load page", http.StatusInternalServerError)
			return
		}
		data := map[string]any{"Title": "Change Password", "Error": msg, "DigestSubscribed": s.Users.IsDigestSubscribed(username), "EmailAvailable": s.smtpAvailable(), "EmailMessage": "Email is unavailable. Configure SMTP in Server settings."}
		mergeInto(data, base)
		render(w, "account_password.html", data)
	}

	if !s.Auth.CheckPassword(username, current) {
		renderErr("Current password is incorrect.")
		return
	}

	user, err := s.Users.UserByUsername(username)
	if err != nil {
		http.Error(w, "failed to load account", http.StatusInternalServerError)
		return
	}
	if user == nil {
		http.Error(w, "failed to load account", http.StatusInternalServerError)
		return
	}
	if err := s.Users.ResetPassword(user.ID, newPassword); err != nil {
		renderErr(err.Error())
		return
	}

	base, _, err := s.baseData(r)
	if err != nil {
		http.Error(w, "failed to load page", http.StatusInternalServerError)
		return
	}
	data := map[string]any{"Title": "Change Password", "Status": "Password updated.", "DigestSubscribed": s.Users.IsDigestSubscribed(username), "EmailAvailable": s.smtpAvailable(), "EmailMessage": "Email is unavailable. Configure SMTP in Server settings."}
	mergeInto(data, base)
	render(w, "account_password.html", data)
}

// AccountDigestToggle flips the logged-in user's opt-in to the weekly
// new-book digest email.
func (s *Server) AccountDigestToggle(w http.ResponseWriter, r *http.Request) {
	if err := r.ParseForm(); err != nil {
		http.Error(w, "bad form", http.StatusBadRequest)
		return
	}
	if !s.smtpAvailable() {
		http.Redirect(w, r, "/account/password", http.StatusSeeOther)
		return
	}
	username, _ := auth.UsernameFromContext(r.Context())
	if err := s.Users.SetDigestSubscribed(username, r.FormValue("subscribed") == "on"); err != nil {
		http.Error(w, "failed to update preference", http.StatusInternalServerError)
		return
	}
	http.Redirect(w, r, "/account/password", http.StatusSeeOther)
}

// ForgotPasswordPage shows the "request a reset link" form.
func (s *Server) ForgotPasswordPage(w http.ResponseWriter, r *http.Request) {
	base, _, err := s.baseData(r)
	if err != nil {
		http.Error(w, "failed to load page", http.StatusInternalServerError)
		return
	}
	data := map[string]any{"Title": "Forgot Password", "EmailAvailable": s.emailAvailable(), "EmailMessage": emailUnavailableMessage()}
	mergeInto(data, base)
	render(w, "forgot_password.html", data)
}

// ForgotPasswordSubmit emails a reset link if the address is on file, but
// always shows the same generic confirmation either way so the response
// can't be used to enumerate registered email addresses.
func (s *Server) ForgotPasswordSubmit(w http.ResponseWriter, r *http.Request) {
	if err := r.ParseForm(); err != nil {
		http.Error(w, "bad form", http.StatusBadRequest)
		return
	}
	email := r.FormValue("email")
	if !s.emailAvailable() {
		base, _, err := s.baseData(r)
		if err != nil {
			http.Error(w, "failed to load page", http.StatusInternalServerError)
			return
		}
		data := map[string]any{"Title": "Forgot Password", "Error": emailUnavailableMessage(), "EmailAvailable": false, "EmailMessage": emailUnavailableMessage()}
		mergeInto(data, base)
		render(w, "forgot_password.html", data)
		return
	}

	token, _, found, err := s.Users.RequestPasswordReset(email)
	if err != nil {
		http.Error(w, "failed to process request", http.StatusInternalServerError)
		return
	}
	if found {
		settings, err := s.Users.GetSMTPSettings()
		if err != nil {
			log.Printf("forgot password: load smtp settings: %v", err)
		} else if !settings.Enabled() {
			log.Printf("forgot password: SMTP not configured, cannot email reset link to %s", email)
		} else if origin, err := s.emailOrigin(); err != nil {
			log.Printf("forgot password: %v", err)
		} else {
			link := origin + "/reset-password?token=" + token
			body := fmt.Sprintf("Someone requested a password reset for your %s account.\n\nReset your password:\n%s\n\nThis link expires in 1 hour. If you didn't request this, you can ignore this email.", s.SiteName, link)
			if err := mail.Send(settings, email, "Reset your "+s.SiteName+" password", body); err != nil {
				log.Printf("forgot password: send email: %v", err)
			}
		}
	}

	base, _, err := s.baseData(r)
	if err != nil {
		http.Error(w, "failed to load page", http.StatusInternalServerError)
		return
	}
	data := map[string]any{
		"Title":          "Forgot Password",
		"Status":         "If that address is on file, a reset link has been sent.",
		"EmailAvailable": true,
		"EmailMessage":   emailUnavailableMessage(),
	}
	mergeInto(data, base)
	render(w, "forgot_password.html", data)
}

// ResetPasswordPage shows the "set a new password" form for a token from a
// forgot-password email.
func (s *Server) ResetPasswordPage(w http.ResponseWriter, r *http.Request) {
	base, _, err := s.baseData(r)
	if err != nil {
		http.Error(w, "failed to load page", http.StatusInternalServerError)
		return
	}
	token := r.URL.Query().Get("token")
	data := map[string]any{"Title": "Reset Password", "Token": token}
	if _, ok := s.Users.VerifyResetToken(token); !ok {
		data["Error"] = "This reset link is invalid or has expired."
		data["Invalid"] = true
	}
	mergeInto(data, base)
	render(w, "reset_password.html", data)
}

// ResetPasswordSubmit completes a password reset from a forgot-password
// email link.
func (s *Server) ResetPasswordSubmit(w http.ResponseWriter, r *http.Request) {
	if err := r.ParseForm(); err != nil {
		http.Error(w, "bad form", http.StatusBadRequest)
		return
	}
	token := r.FormValue("token")
	password := r.FormValue("password")

	if err := s.Users.CompletePasswordReset(token, password); err != nil {
		base, _, berr := s.baseData(r)
		if berr != nil {
			http.Error(w, "failed to load page", http.StatusInternalServerError)
			return
		}
		data := map[string]any{"Title": "Reset Password", "Token": token, "Error": err.Error()}
		mergeInto(data, base)
		render(w, "reset_password.html", data)
		return
	}

	http.Redirect(w, r, "/login", http.StatusSeeOther)
}

// InviteAcceptPage shows the "set your password" form for a new-account
// invite link.
func (s *Server) InviteAcceptPage(w http.ResponseWriter, r *http.Request) {
	base, _, err := s.baseData(r)
	if err != nil {
		http.Error(w, "failed to load page", http.StatusInternalServerError)
		return
	}
	token := r.URL.Query().Get("token")
	data := map[string]any{"Title": "Accept Invite", "Token": token}
	if _, ok := s.Users.VerifyInviteToken(token); !ok {
		data["Error"] = "This invite link is invalid or has expired."
		data["Invalid"] = true
	}
	mergeInto(data, base)
	render(w, "invite_accept.html", data)
}

// InviteAcceptSubmit sets the invited account's password, activating it.
func (s *Server) InviteAcceptSubmit(w http.ResponseWriter, r *http.Request) {
	if err := r.ParseForm(); err != nil {
		http.Error(w, "bad form", http.StatusBadRequest)
		return
	}
	token := r.FormValue("token")
	password := r.FormValue("password")

	if err := s.Users.AcceptInvite(token, password); err != nil {
		base, _, berr := s.baseData(r)
		if berr != nil {
			http.Error(w, "failed to load page", http.StatusInternalServerError)
			return
		}
		data := map[string]any{"Title": "Accept Invite", "Token": token, "Error": err.Error()}
		mergeInto(data, base)
		render(w, "invite_accept.html", data)
		return
	}

	http.Redirect(w, r, "/login", http.StatusSeeOther)
}

func (s *Server) bookmarkURL(r *http.Request, token string) string {
	return s.siteOrigin(r) + "/?token=" + token
}
