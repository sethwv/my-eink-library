package auth

import (
	"net/http"
	"net/url"
)

// RequireAuth redirects to /login (preserving the original path as ?next=)
// unless the request carries a valid session cookie.
func (a *Authenticator) RequireAuth(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !a.VerifySession(r) {
			nextPath := url.QueryEscape(r.URL.RequestURI())
			http.Redirect(w, r, "/login?next="+nextPath, http.StatusSeeOther)
			return
		}
		next.ServeHTTP(w, r)
	})
}
