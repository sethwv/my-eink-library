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
	"github.com/swvn/eink-library/internal/chaptarr"
	"github.com/swvn/eink-library/internal/hardcover"
	"github.com/swvn/eink-library/internal/index"
	"github.com/swvn/eink-library/internal/kepub"
	"github.com/swvn/eink-library/internal/mail"
	"github.com/swvn/eink-library/internal/thumbnail"
	"github.com/swvn/eink-library/internal/users"
)

type Server struct {
	Auth        *auth.Authenticator
	DB          *index.DB
	Covers      *thumbnail.Store
	Users       *users.Store
	Hardcover   *hardcover.Client // nil-safe: Enabled() is false with no token, callers check before use
	Chaptarr    *chaptarr.Client  // nil-safe: Enabled() is false with no URL/key, callers check before use
	LibraryPath string
	DataDir     string
	PageSize    int
	SiteName    string
	PublicURL   string // trusted base URL for emailed links; see config.Config.PublicURL
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
		"ShelfMemberships": memberships,
		"ViewingShelfID":   p.viewingShelfID,
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

// AdminUsersInvite creates a pending account and emails the invitee a link
// to set their own password.
func (s *Server) AdminUsersInvite(w http.ResponseWriter, r *http.Request) {
	if err := r.ParseForm(); err != nil {
		http.Error(w, "bad form", http.StatusBadRequest)
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

	list, err := s.Users.List()
	if err != nil {
		http.Error(w, "failed to load users", http.StatusInternalServerError)
		return
	}
	var email string
	for _, u := range list {
		if u.ID == id {
			email = u.Email
		}
	}
	if email == "" {
		s.renderAdminUsersError(w, r, "user has no email on file")
		return
	}

	token, err := s.Users.ResendInvite(id)
	if err != nil {
		s.renderAdminUsersError(w, r, err.Error())
		return
	}
	if err := s.sendInviteEmail(r, email, token); err != nil {
		log.Printf("resend invite email to %s: %v", email, err)
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
	authors, err := s.DB.ListAuthors()
	if err != nil {
		return nil, err
	}
	series, err := s.DB.ListSeries()
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

	smtp, err := s.Users.GetSMTPSettings()
	if err != nil {
		return nil, err
	}

	return map[string]any{
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
		"SMTPConfigured":     smtp.Enabled(),
		"SMTPHost":           smtp.Host,
		"SMTPPort":           smtp.Port,
		"SMTPEncryption":     smtp.Encryption,
		"SMTPUsername":       smtp.Username,
		"SMTPFromName":       smtp.FromName,
		"SMTPFromAddress":    smtp.FromAddress,
	}, nil
}

// serverIntegrationsData builds the template data for the admin
// Integrations page (Hardcover + Chaptarr): current DB-backed settings,
// live Enabled() state from the running clients, and Hardcover's
// enrichment progress stats (moved here from admin_server.html).
func (s *Server) serverIntegrationsData() (map[string]any, error) {
	settings, err := s.Users.GetIntegrationSettings()
	if err != nil {
		return nil, err
	}
	enrichmentStats, err := s.DB.GetEnrichmentStats()
	if err != nil {
		return nil, err
	}

	return map[string]any{
		"Title":               "Integrations",
		"HardcoverEnabled":    settings.HardcoverEnabled,
		"HardcoverActive":     s.Hardcover.Enabled(),
		"HardcoverConfigured": settings.HardcoverToken != "",
		"ChaptarrEnabled":     settings.ChaptarrEnabled,
		"ChaptarrActive":      s.Chaptarr.Enabled(),
		"ChaptarrURL":         settings.ChaptarrURL,
		"ChaptarrConfigured":  settings.ChaptarrAPIKey != "",
		"EnrichmentPending":   enrichmentStats.Pending,
		"EnrichmentDone":      enrichmentStats.Done,
		"EnrichmentNoMatch":   enrichmentStats.NoMatch,
		"EnrichmentErrored":   enrichmentStats.Errored,
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

func (s *Server) renderAdminServerError(w http.ResponseWriter, r *http.Request, errMsg, statusMsg string) {
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
	if errMsg != "" {
		data["Error"] = errMsg
	}
	if statusMsg != "" {
		data["Status"] = statusMsg
	}
	mergeInto(data, base)
	render(w, "admin_server.html", data)
}

// ServerSMTPSave saves the admin-configured SMTP settings. An empty
// submitted password means "keep the existing password" rather than
// clearing it, since the form never echoes the real password back.
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
		s.renderAdminServerError(w, r, "failed to save SMTP settings: "+err.Error(), "")
		return
	}

	http.Redirect(w, r, "/admin/server", http.StatusSeeOther)
}

// ServerSMTPTest sends a test email to an admin-supplied address using the
// currently saved SMTP settings.
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
		s.renderAdminServerError(w, r, "SMTP is not configured yet. Save settings first.", "")
		return
	}

	body := fmt.Sprintf("This is a test email from %s, confirming your SMTP settings work.", s.SiteName)
	if err := mail.Send(settings, to, "Test email from "+s.SiteName, body); err != nil {
		s.renderAdminServerError(w, r, "test email failed: "+err.Error(), "")
		return
	}

	s.renderAdminServerError(w, r, "", "Test email sent to "+to+".")
}

// ServerRescan triggers a synchronous full library rescan, then returns to the server info page.
func (s *Server) ServerRescan(w http.ResponseWriter, r *http.Request) {
	if err := s.DB.Scan(s.LibraryPath, s.Covers); err != nil {
		http.Error(w, "rescan failed: "+err.Error(), http.StatusInternalServerError)
		return
	}
	http.Redirect(w, r, "/admin/server", http.StatusSeeOther)
}

// ServerReimport triggers a synchronous full library reimport, re-parsing
// every EPUB regardless of whether its file has changed (unlike a normal
// rescan, which skips unchanged files) — used to pick up EPUB metadata
// parsing fixes on books that are already indexed.
func (s *Server) ServerReimport(w http.ResponseWriter, r *http.Request) {
	if err := s.DB.Reimport(s.LibraryPath, s.Covers); err != nil {
		http.Error(w, "reimport failed: "+err.Error(), http.StatusInternalServerError)
		return
	}
	http.Redirect(w, r, "/admin/server", http.StatusSeeOther)
}

// ServerEnrichmentReset clears all Hardcover/Chaptarr-derived enrichment
// data so the background queues re-process every book from scratch.
func (s *Server) ServerEnrichmentReset(w http.ResponseWriter, r *http.Request) {
	if err := s.DB.ResetEnrichment(); err != nil {
		http.Error(w, "enrichment reset failed: "+err.Error(), http.StatusInternalServerError)
		return
	}
	http.Redirect(w, r, "/admin/server/integrations", http.StatusSeeOther)
}

// ServerIntegrations shows the admin Integrations page: Hardcover and
// Chaptarr toggle/credential settings plus Hardcover's enrichment progress.
func (s *Server) ServerIntegrations(w http.ResponseWriter, r *http.Request) {
	base, _, err := s.baseData(r)
	if err != nil {
		http.Error(w, "failed to load page", http.StatusInternalServerError)
		return
	}
	data, err := s.serverIntegrationsData()
	if err != nil {
		http.Error(w, "failed to load settings", http.StatusInternalServerError)
		return
	}
	mergeInto(data, base)
	render(w, "admin_integrations.html", data)
}

func (s *Server) renderAdminIntegrationsError(w http.ResponseWriter, r *http.Request, errMsg, statusMsg string) {
	base, _, err := s.baseData(r)
	if err != nil {
		http.Error(w, "failed to load page", http.StatusInternalServerError)
		return
	}
	data, err := s.serverIntegrationsData()
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

// ServerIntegrationsHardcoverSave saves the Hardcover enable toggle and API
// token, then applies the change to the running client immediately (see
// hardcover.Client.SetConfig) so RunEnrichmentQueue picks it up on its next
// loop iteration without a restart. An empty submitted token means "keep
// the existing token" — the form never echoes the real token back.
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

	if err := s.Users.SaveIntegrationSettings(current); err != nil {
		s.renderAdminIntegrationsError(w, r, "failed to save Hardcover settings: "+err.Error(), "")
		return
	}
	s.Hardcover.SetConfig(current.HardcoverEnabled, current.HardcoverToken)

	http.Redirect(w, r, "/admin/server/integrations", http.StatusSeeOther)
}

// ServerIntegrationsChaptarrSave saves the Chaptarr enable toggle, base
// URL, and API key, then applies the change to the running client
// immediately (see chaptarr.Client.SetConfig) so RunChaptarrQueue picks it
// up on its next loop iteration without a restart. An empty submitted key
// means "keep the existing key" — the form never echoes the real key back.
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

	if err := s.Users.SaveIntegrationSettings(current); err != nil {
		s.renderAdminIntegrationsError(w, r, "failed to save Chaptarr settings: "+err.Error(), "")
		return
	}
	s.Chaptarr.SetConfig(current.ChaptarrEnabled, current.ChaptarrURL, current.ChaptarrAPIKey)

	http.Redirect(w, r, "/admin/server/integrations", http.StatusSeeOther)
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
	data := map[string]any{"Title": "Change Password", "DigestSubscribed": s.Users.IsDigestSubscribed(username)}
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
		data := map[string]any{"Title": "Change Password", "Error": msg, "DigestSubscribed": s.Users.IsDigestSubscribed(username)}
		mergeInto(data, base)
		render(w, "account_password.html", data)
	}

	if !s.Auth.CheckPassword(username, current) {
		renderErr("Current password is incorrect.")
		return
	}

	list, err := s.Users.List()
	if err != nil {
		http.Error(w, "failed to load account", http.StatusInternalServerError)
		return
	}
	var id int64
	for _, u := range list {
		if u.Username == username {
			id = u.ID
		}
	}
	if err := s.Users.ResetPassword(id, newPassword); err != nil {
		renderErr(err.Error())
		return
	}

	base, _, err := s.baseData(r)
	if err != nil {
		http.Error(w, "failed to load page", http.StatusInternalServerError)
		return
	}
	data := map[string]any{"Title": "Change Password", "Status": "Password updated.", "DigestSubscribed": s.Users.IsDigestSubscribed(username)}
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
	data := map[string]any{"Title": "Forgot Password"}
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
		"Title":  "Forgot Password",
		"Status": "If that address is on file, a reset link has been sent.",
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
