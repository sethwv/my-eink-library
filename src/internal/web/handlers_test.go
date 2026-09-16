package web

import (
	"archive/zip"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/sethwv/my-eink-library/internal/auth"
	"github.com/sethwv/my-eink-library/internal/index"
	"github.com/sethwv/my-eink-library/internal/users"
)

func newTestServer(t *testing.T) *Server {
	t.Helper()

	store, err := users.Open(filepath.Join(t.TempDir(), "users.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { store.Close() })

	return &Server{
		Auth:     auth.New("test-session-secret", time.Hour, store),
		Users:    store,
		SiteName: "Test Library",
	}
}

func addTestBook(t *testing.T, db *index.DB) int64 {
	t.Helper()

	library := t.TempDir()
	path := filepath.Join(library, "book.epub")
	f, err := os.Create(path)
	if err != nil {
		t.Fatal(err)
	}
	zw := zip.NewWriter(f)
	for name, content := range map[string]string{
		"META-INF/container.xml": `<?xml version="1.0"?><container><rootfiles><rootfile full-path="content.opf"/></rootfiles></container>`,
		"content.opf":            `<?xml version="1.0"?><package><metadata><title>Test Book</title><creator>Test Author</creator></metadata></package>`,
	} {
		w, err := zw.Create(name)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := w.Write([]byte(content)); err != nil {
			t.Fatal(err)
		}
	}
	if err := zw.Close(); err != nil {
		t.Fatal(err)
	}
	if err := f.Close(); err != nil {
		t.Fatal(err)
	}
	if err := db.Scan([]string{library}, nil); err != nil {
		t.Fatal(err)
	}
	books, err := db.List(index.SortTitle, false, 1, 1, index.Filter{})
	if err != nil {
		t.Fatal(err)
	}
	if len(books) != 1 {
		t.Fatalf("indexed books = %d, want 1", len(books))
	}
	return books[0].ID
}

func TestLoginSubmit(t *testing.T) {
	server := newTestServer(t)
	if err := server.Users.Create("reader", "correct-password", users.RoleMember, true, ""); err != nil {
		t.Fatal(err)
	}

	tests := []struct {
		name         string
		form         url.Values
		wantStatus   int
		wantLocation string
		wantBody     string
		wantSession  bool
	}{
		{
			name: "valid credentials redirect to same-site next",
			form: url.Values{
				"username": {"reader"},
				"password": {"correct-password"},
				"next":     {"/authors?name=Octavia+Butler"},
			},
			wantStatus:   http.StatusSeeOther,
			wantLocation: "/authors?name=Octavia+Butler",
			wantSession:  true,
		},
		{
			name: "valid credentials reject off-site next",
			form: url.Values{
				"username": {"reader"},
				"password": {"correct-password"},
				"next":     {"https://attacker.example"},
			},
			wantStatus:   http.StatusSeeOther,
			wantLocation: "/",
			wantSession:  true,
		},
		{
			name: "invalid credentials render generic error without session",
			form: url.Values{
				"username": {"reader"},
				"password": {"wrong-password"},
				"next":     {"/authors"},
			},
			wantStatus: http.StatusOK,
			wantBody:   "Incorrect username or password.",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodPost, "/login", strings.NewReader(tt.form.Encode()))
			req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
			recorder := httptest.NewRecorder()

			server.LoginSubmit(recorder, req)

			if recorder.Code != tt.wantStatus {
				t.Fatalf("status = %d, want %d: %s", recorder.Code, tt.wantStatus, recorder.Body.String())
			}
			if got := recorder.Header().Get("Location"); got != tt.wantLocation {
				t.Errorf("Location = %q, want %q", got, tt.wantLocation)
			}
			if tt.wantBody != "" && !strings.Contains(recorder.Body.String(), tt.wantBody) {
				t.Errorf("response missing %q: %s", tt.wantBody, recorder.Body.String())
			}

			cookies := recorder.Result().Cookies()
			hasSession := false
			for _, cookie := range cookies {
				if cookie.Name == auth.CookieName {
					hasSession = true
				}
			}
			if hasSession != tt.wantSession {
				t.Errorf("session cookie = %t, want %t", hasSession, tt.wantSession)
			}
		})
	}
}

func TestResetPasswordHandlers(t *testing.T) {
	server := newTestServer(t)
	if err := server.Users.Create("reader", "old-password", users.RoleMember, true, "reader@example.com"); err != nil {
		t.Fatal(err)
	}
	token, _, found, err := server.Users.RequestPasswordReset("reader@example.com")
	if err != nil || !found {
		t.Fatalf("RequestPasswordReset() = (%q, found=%t, err=%v)", token, found, err)
	}

	t.Run("page accepts a valid token", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/reset-password?token="+url.QueryEscape(token), nil)
		recorder := httptest.NewRecorder()

		server.ResetPasswordPage(recorder, req)

		if recorder.Code != http.StatusOK {
			t.Fatalf("status = %d, want %d", recorder.Code, http.StatusOK)
		}
		if !strings.Contains(recorder.Body.String(), `action="/reset-password"`) {
			t.Errorf("valid token page did not render the reset form: %s", recorder.Body.String())
		}
	})

	t.Run("page rejects an invalid token", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/reset-password?token=invalid", nil)
		recorder := httptest.NewRecorder()

		server.ResetPasswordPage(recorder, req)

		if recorder.Code != http.StatusOK {
			t.Fatalf("status = %d, want %d", recorder.Code, http.StatusOK)
		}
		if !strings.Contains(recorder.Body.String(), "This reset link is invalid or has expired.") {
			t.Errorf("invalid token page missing error: %s", recorder.Body.String())
		}
		if strings.Contains(recorder.Body.String(), `action="/reset-password"`) {
			t.Errorf("invalid token page rendered the reset form: %s", recorder.Body.String())
		}
	})

	t.Run("submit consumes token and redirects to login", func(t *testing.T) {
		form := url.Values{"token": {token}, "password": {"new-password"}}
		req := httptest.NewRequest(http.MethodPost, "/reset-password", strings.NewReader(form.Encode()))
		req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		recorder := httptest.NewRecorder()

		server.ResetPasswordSubmit(recorder, req)

		if recorder.Code != http.StatusSeeOther || recorder.Header().Get("Location") != "/login" {
			t.Errorf("response = (%d, %q), want (%d, %q)", recorder.Code, recorder.Header().Get("Location"), http.StatusSeeOther, "/login")
		}
		if !server.Auth.CheckPassword("reader", "new-password") {
			t.Error("reset password was not applied")
		}
		if _, ok := server.Users.VerifyResetToken(token); ok {
			t.Error("reset token remains valid after submission")
		}
	})
}

func TestInviteAcceptHandlers(t *testing.T) {
	server := newTestServer(t)
	token, err := server.Users.InviteUser("invitee", "invitee@example.com", users.RoleMember, true)
	if err != nil {
		t.Fatal(err)
	}

	t.Run("page accepts a valid token", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/invite/accept?token="+url.QueryEscape(token), nil)
		recorder := httptest.NewRecorder()

		server.InviteAcceptPage(recorder, req)

		if recorder.Code != http.StatusOK {
			t.Fatalf("status = %d, want %d", recorder.Code, http.StatusOK)
		}
		if !strings.Contains(recorder.Body.String(), `action="/invite/accept"`) {
			t.Errorf("valid token page did not render the invite form: %s", recorder.Body.String())
		}
	})

	t.Run("page rejects an invalid token", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/invite/accept?token=invalid", nil)
		recorder := httptest.NewRecorder()

		server.InviteAcceptPage(recorder, req)

		if recorder.Code != http.StatusOK {
			t.Fatalf("status = %d, want %d", recorder.Code, http.StatusOK)
		}
		if !strings.Contains(recorder.Body.String(), "This invite link is invalid or has expired.") {
			t.Errorf("invalid token page missing error: %s", recorder.Body.String())
		}
		if strings.Contains(recorder.Body.String(), `action="/invite/accept"`) {
			t.Errorf("invalid token page rendered the invite form: %s", recorder.Body.String())
		}
	})

	t.Run("submit consumes token and redirects to login", func(t *testing.T) {
		form := url.Values{"token": {token}, "password": {"chosen-password"}}
		req := httptest.NewRequest(http.MethodPost, "/invite/accept", strings.NewReader(form.Encode()))
		req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		recorder := httptest.NewRecorder()

		server.InviteAcceptSubmit(recorder, req)

		if recorder.Code != http.StatusSeeOther || recorder.Header().Get("Location") != "/login" {
			t.Errorf("response = (%d, %q), want (%d, %q)", recorder.Code, recorder.Header().Get("Location"), http.StatusSeeOther, "/login")
		}
		if !server.Auth.CheckPassword("invitee", "chosen-password") {
			t.Error("invite password was not applied")
		}
		if _, ok := server.Users.VerifyInviteToken(token); ok {
			t.Error("invite token remains valid after submission")
		}
	})
}

func TestShelfToggleRejectsAnotherUsersShelf(t *testing.T) {
	server := newTestServer(t)
	if err := server.Users.Create("reader", "reader-password", users.RoleMember, true, ""); err != nil {
		t.Fatal(err)
	}

	db, err := index.Open(filepath.Join(t.TempDir(), "index.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	server.DB = db

	otherShelfID, err := db.EnsureSystemShelf("other-reader", "favourites", "Favourites")
	if err != nil {
		t.Fatal(err)
	}

	sessionRecorder := httptest.NewRecorder()
	sessionRequest := httptest.NewRequest(http.MethodPost, "/login", nil)
	server.Auth.IssueSession(sessionRecorder, sessionRequest, "reader")
	cookies := sessionRecorder.Result().Cookies()
	if len(cookies) != 1 {
		t.Fatalf("issued cookies = %d, want 1", len(cookies))
	}

	req := httptest.NewRequest(http.MethodPost, "/books/99/shelves/1", strings.NewReader("next=%2Fauthors"))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.AddCookie(cookies[0])
	req.SetPathValue("id", "99")
	req.SetPathValue("shelfID", strconv.FormatInt(otherShelfID, 10))
	recorder := httptest.NewRecorder()

	server.Auth.RequireAuth(http.HandlerFunc(server.ShelfToggle)).ServeHTTP(recorder, req)

	if recorder.Code != http.StatusNotFound {
		t.Errorf("status = %d, want %d", recorder.Code, http.StatusNotFound)
	}
}

func TestShelfToggleAddsBookAndRedirects(t *testing.T) {
	server := newTestServer(t)
	if err := server.Users.Create("reader", "reader-password", users.RoleMember, true, ""); err != nil {
		t.Fatal(err)
	}

	db, err := index.Open(filepath.Join(t.TempDir(), "index.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	server.DB = db
	bookID := addTestBook(t, db)
	shelfID, err := db.EnsureSystemShelf("reader", "favourites", "Favourites")
	if err != nil {
		t.Fatal(err)
	}

	sessionRecorder := httptest.NewRecorder()
	server.Auth.IssueSession(sessionRecorder, httptest.NewRequest(http.MethodPost, "/login", nil), "reader")
	cookies := sessionRecorder.Result().Cookies()
	if len(cookies) != 1 {
		t.Fatalf("issued cookies = %d, want 1", len(cookies))
	}

	req := httptest.NewRequest(http.MethodPost, "/books/1/shelves/1", strings.NewReader("next=%2Fauthors"))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.AddCookie(cookies[0])
	req.SetPathValue("id", strconv.FormatInt(bookID, 10))
	req.SetPathValue("shelfID", strconv.FormatInt(shelfID, 10))
	recorder := httptest.NewRecorder()

	server.Auth.RequireAuth(http.HandlerFunc(server.ShelfToggle)).ServeHTTP(recorder, req)

	if recorder.Code != http.StatusSeeOther || recorder.Header().Get("Location") != "/authors" {
		t.Errorf("response = (%d, %q), want (%d, %q)", recorder.Code, recorder.Header().Get("Location"), http.StatusSeeOther, "/authors")
	}
	onShelf, err := db.IsBookOnShelf(shelfID, bookID)
	if err != nil {
		t.Fatal(err)
	}
	if !onShelf {
		t.Error("book was not added to the requested shelf")
	}
}

func TestBookEditMetadataSavePersistsSubmittedFields(t *testing.T) {
	server := newTestServer(t)
	db, err := index.Open(filepath.Join(t.TempDir(), "index.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	server.DB = db
	bookID := addTestBook(t, db)

	form := url.Values{
		"title":          {"Edited Book"},
		"series":         {"Test Series"},
		"series_index":   {"2.5"},
		"published_date": {"2024-01-02"},
		"description":    {"A complete test description."},
		"genres":         {"Fantasy, Science Fiction"},
		"publisher":      {"Test Publisher"},
		"pages":          {"321"},
		"isbn":           {"9781234567897"},
		"rating":         {"4.5"},
	}
	req := httptest.NewRequest(http.MethodPost, "/books/1/edit-metadata", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.SetPathValue("id", strconv.FormatInt(bookID, 10))
	recorder := httptest.NewRecorder()

	server.BookEditMetadataSave(recorder, req)

	wantLocation := "/books/" + strconv.FormatInt(bookID, 10) + "/edit-metadata"
	if recorder.Code != http.StatusSeeOther || recorder.Header().Get("Location") != wantLocation {
		t.Errorf("response = (%d, %q), want (%d, %q)", recorder.Code, recorder.Header().Get("Location"), http.StatusSeeOther, wantLocation)
	}
	book, err := db.Get(bookID)
	if err != nil {
		t.Fatal(err)
	}
	if book == nil {
		t.Fatal("saved book was not found")
	}
	if book.Title != "Edited Book" || book.Series != "Test Series" || book.SeriesIndex != 2.5 {
		t.Errorf("saved title/series = (%q, %q, %v)", book.Title, book.Series, book.SeriesIndex)
	}
	if book.PublishedAt != "2024-01-02" || book.Description != "A complete test description." || book.Publisher != "Test Publisher" {
		t.Errorf("saved metadata = (%q, %q, %q)", book.PublishedAt, book.Description, book.Publisher)
	}
	if got, want := strings.Join(book.Genres, ","), "Fantasy,Science Fiction"; got != want {
		t.Errorf("genres = %q, want %q", got, want)
	}
	if book.Pages != 321 || book.ISBN != "9781234567897" || book.Rating != 4.5 {
		t.Errorf("saved numeric metadata = (%d, %q, %v)", book.Pages, book.ISBN, book.Rating)
	}
}

func TestAdminUsersCreateRendersValidationError(t *testing.T) {
	server := newTestServer(t)
	form := url.Values{
		"username": {"reader"},
		"password": {"reader-password"},
		"role":     {"not-a-role"},
	}
	req := httptest.NewRequest(http.MethodPost, "/admin/users", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	recorder := httptest.NewRecorder()

	server.AdminUsersCreate(recorder, req)

	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d: %s", recorder.Code, http.StatusOK, recorder.Body.String())
	}
	if !strings.Contains(recorder.Body.String(), `invalid role &#34;not-a-role&#34;`) {
		t.Errorf("response missing validation error: %s", recorder.Body.String())
	}
	if server.Auth.CheckPassword("reader", "reader-password") {
		t.Error("invalid role submission created a user")
	}
}
