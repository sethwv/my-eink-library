package auth

import (
	"context"
	"net/http"
	"net/url"
)

type contextKey int

const usernameContextKey contextKey = iota

// RequireAuth redirects to /login (preserving the original path as ?next=)
// unless the request carries a valid session cookie. The verified username
// is attached to the request context for downstream handlers/RequireAdmin.
func (a *Authenticator) RequireAuth(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		username, ok := a.VerifySession(r)
		if !ok {
			nextPath := url.QueryEscape(r.URL.RequestURI())
			http.Redirect(w, r, "/login?next="+nextPath, http.StatusSeeOther)
			return
		}
		ctx := context.WithValue(r.Context(), usernameContextKey, username)
		next.ServeHTTP(w, r.WithContext(ctx))
	})
}

// RequireAdmin wraps RequireAuth and additionally 403s any non-admin user.
// Must be applied after (outside) RequireAuth so the username is in context.
func (a *Authenticator) RequireAdmin(next http.Handler) http.Handler {
	return a.RequireAuth(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		username, _ := UsernameFromContext(r.Context())
		if !a.users.IsAdmin(username) {
			http.Error(w, "forbidden", http.StatusForbidden)
			return
		}
		next.ServeHTTP(w, r)
	}))
}

// UsernameFromContext returns the signed-in username set by RequireAuth, if any.
func UsernameFromContext(ctx context.Context) (string, bool) {
	username, ok := ctx.Value(usernameContextKey).(string)
	return username, ok
}
