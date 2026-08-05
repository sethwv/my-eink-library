package auth

import (
	"context"
	"net/http"
	"net/url"
)

type contextKey int

const (
	usernameContextKey contextKey = iota
	restrictedContextKey
)

// RequireAuth redirects to /login (preserving the original path as ?next=)
// unless the request carries a valid session cookie or a valid ?token=
// bookmark token — the latter checked on every request, not just once to
// establish a cookie, since a bookmarking e-reader's cookie handling isn't
// fully trusted (see internal/users.Store.VerifyBookmarkToken). When a
// request is authenticated via token, a restricted session cookie is also
// issued opportunistically so any cookie-capable browsing within the
// session doesn't need the token on every link — but the token alone must
// keep working even if that cookie never sticks. The verified username and
// restricted flag are attached to the request context for downstream
// handlers/RequireFull/RequireAdmin.
func (a *Authenticator) RequireAuth(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		username, restricted, ok := a.VerifySession(r)
		if !ok {
			if token := r.URL.Query().Get("token"); token != "" {
				if u, valid := a.users.VerifyBookmarkToken(token); valid {
					username, ok = u, true
					restricted = true
					a.IssueRestrictedSession(w, r, username)
				}
			}
		}
		if !ok {
			nextPath := url.QueryEscape(r.URL.RequestURI())
			http.Redirect(w, r, "/login?next="+nextPath, http.StatusSeeOther)
			return
		}
		ctx := context.WithValue(r.Context(), usernameContextKey, username)
		ctx = context.WithValue(ctx, restrictedContextKey, restricted)
		next.ServeHTTP(w, r.WithContext(ctx))
	})
}

// RequireFull wraps RequireAuth and additionally sends a restricted
// (bookmark-token-issued) session back through /login as a password
// step-up, redirecting to the original path on success — a full password
// login always issues a full session (IssueSession), so this naturally
// upgrades the account's access in place rather than just blocking it.
// Used for admin routes and any other action a magic-link-only session
// shouldn't be able to reach.
func (a *Authenticator) RequireFull(next http.Handler) http.Handler {
	return a.RequireAuth(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if IsRestricted(r.Context()) {
			nextPath := url.QueryEscape(r.URL.RequestURI())
			http.Redirect(w, r, "/login?next="+nextPath, http.StatusSeeOther)
			return
		}
		next.ServeHTTP(w, r)
	}))
}

// RequireAdmin wraps RequireFull (so a restricted session step-up-prompts
// rather than 403ing) and additionally 403s any non-admin user.
func (a *Authenticator) RequireAdmin(next http.Handler) http.Handler {
	return a.RequireFull(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
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

// IsRestricted reports whether the current request's session was
// authenticated via a bookmark token (view/shelves only) rather than a full
// password login. Only meaningful after RequireAuth has run.
func IsRestricted(ctx context.Context) bool {
	restricted, _ := ctx.Value(restrictedContextKey).(bool)
	return restricted
}
