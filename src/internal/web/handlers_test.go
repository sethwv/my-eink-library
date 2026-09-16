package web

import (
	"net/http"
	"net/http/httptest"
	"net/url"
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
