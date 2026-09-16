package web

import (
	"net/http"
	"net/http/httptest"
	"net/url"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/sethwv/my-eink-library/internal/auth"
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
