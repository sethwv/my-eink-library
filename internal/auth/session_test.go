package auth

import (
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func testAuthenticator(t *testing.T) *Authenticator {
	t.Helper()
	a, err := New("reader", "s3cret", "test-signing-secret", time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	return a
}

func TestCheckPassword(t *testing.T) {
	a := testAuthenticator(t)

	if !a.CheckPassword("reader", "s3cret") {
		t.Error("expected correct credentials to pass")
	}
	if a.CheckPassword("reader", "wrong") {
		t.Error("expected wrong password to fail")
	}
	if a.CheckPassword("nope", "s3cret") {
		t.Error("expected wrong username to fail")
	}
}

func TestSessionRoundTrip(t *testing.T) {
	a := testAuthenticator(t)

	w := httptest.NewRecorder()
	req := httptest.NewRequest("POST", "/login", nil)
	a.IssueSession(w, req)

	cookies := w.Result().Cookies()
	if len(cookies) != 1 {
		t.Fatalf("expected 1 cookie, got %d", len(cookies))
	}

	verifyReq := httptest.NewRequest("GET", "/", nil)
	verifyReq.AddCookie(cookies[0])
	if !a.VerifySession(verifyReq) {
		t.Error("expected valid session to verify")
	}
}

func TestSessionExpired(t *testing.T) {
	a, err := New("reader", "s3cret", "test-signing-secret", -time.Hour)
	if err != nil {
		t.Fatal(err)
	}

	w := httptest.NewRecorder()
	req := httptest.NewRequest("POST", "/login", nil)
	a.IssueSession(w, req)

	verifyReq := httptest.NewRequest("GET", "/", nil)
	verifyReq.AddCookie(w.Result().Cookies()[0])
	if a.VerifySession(verifyReq) {
		t.Error("expected expired session to fail verification")
	}
}

func TestSessionTamperedRejected(t *testing.T) {
	a := testAuthenticator(t)

	w := httptest.NewRecorder()
	req := httptest.NewRequest("POST", "/login", nil)
	a.IssueSession(w, req)
	cookie := w.Result().Cookies()[0]
	cookie.Value = cookie.Value + "x"

	verifyReq := httptest.NewRequest("GET", "/", nil)
	verifyReq.AddCookie(cookie)
	if a.VerifySession(verifyReq) {
		t.Error("expected tampered cookie to fail verification")
	}
}

func TestSessionWrongSecretRejected(t *testing.T) {
	a1, _ := New("reader", "s3cret", "secret-one", time.Hour)
	a2, _ := New("reader", "s3cret", "secret-two", time.Hour)

	w := httptest.NewRecorder()
	req := httptest.NewRequest("POST", "/login", nil)
	a1.IssueSession(w, req)

	verifyReq := httptest.NewRequest("GET", "/", nil)
	verifyReq.AddCookie(w.Result().Cookies()[0])
	if a2.VerifySession(verifyReq) {
		t.Error("expected session signed with different secret to fail verification")
	}
}

func TestRequireAuth_RedirectsUnauthenticated(t *testing.T) {
	a := testAuthenticator(t)
	handler := a.RequireAuth(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	req := httptest.NewRequest("GET", "/", nil)
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	if w.Code != http.StatusSeeOther {
		t.Errorf("status = %d, want %d", w.Code, http.StatusSeeOther)
	}
	loc := w.Header().Get("Location")
	if loc == "" || loc[:6] != "/login" {
		t.Errorf("Location = %q, want redirect to /login", loc)
	}
}

func TestRequireAuth_AllowsAuthenticated(t *testing.T) {
	a := testAuthenticator(t)
	handler := a.RequireAuth(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	issueW := httptest.NewRecorder()
	a.IssueSession(issueW, httptest.NewRequest("POST", "/login", nil))

	req := httptest.NewRequest("GET", "/", nil)
	req.AddCookie(issueW.Result().Cookies()[0])
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("status = %d, want %d", w.Code, http.StatusOK)
	}
}
