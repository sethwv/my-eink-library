// Package auth handles the login flow: delegating password checks to
// internal/users, and issuing a stateless HMAC-signed session cookie
// (no server-side session store).
package auth

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/swvn/eink-library/internal/users"
)

const CookieName = "eink_session"

type Authenticator struct {
	users  *users.Store
	secret []byte
	ttl    time.Duration
}

// New creates an Authenticator backed by the given users store.
func New(secret string, ttl time.Duration, store *users.Store) *Authenticator {
	return &Authenticator{
		users:  store,
		secret: []byte(secret),
		ttl:    ttl,
	}
}

// CheckPassword reports whether the given credentials are a valid login.
func (a *Authenticator) CheckPassword(username, password string) bool {
	return a.users.CheckPassword(username, password)
}

// IssueSession sets a signed session cookie for username.
func (a *Authenticator) IssueSession(w http.ResponseWriter, r *http.Request, username string) {
	expires := time.Now().Add(a.ttl)
	payload := username + "|" + strconv.FormatInt(expires.Unix(), 10)
	token := a.sign(payload)

	http.SetCookie(w, &http.Cookie{
		Name:     CookieName,
		Value:    token,
		Path:     "/",
		HttpOnly: true,
		Secure:   r.TLS != nil,
		SameSite: http.SameSiteLaxMode,
		Expires:  expires,
	})
}

// ClearSession removes the session cookie.
func (a *Authenticator) ClearSession(w http.ResponseWriter) {
	http.SetCookie(w, &http.Cookie{
		Name:     CookieName,
		Value:    "",
		Path:     "/",
		HttpOnly: true,
		SameSite: http.SameSiteLaxMode,
		MaxAge:   -1,
	})
}

// VerifySession reports the signed-in username if the request carries a
// valid, unexpired session cookie.
func (a *Authenticator) VerifySession(r *http.Request) (string, bool) {
	c, err := r.Cookie(CookieName)
	if err != nil || c.Value == "" {
		return "", false
	}

	payload, ok := a.verify(c.Value)
	if !ok {
		return "", false
	}

	parts := strings.SplitN(payload, "|", 2)
	if len(parts) != 2 || parts[0] == "" {
		return "", false
	}
	exp, err := strconv.ParseInt(parts[1], 10, 64)
	if err != nil {
		return "", false
	}
	if time.Now().Unix() >= exp {
		return "", false
	}
	return parts[0], true
}

func (a *Authenticator) sign(payload string) string {
	mac := hmac.New(sha256.New, a.secret)
	mac.Write([]byte(payload))
	sig := mac.Sum(nil)
	return base64.RawURLEncoding.EncodeToString([]byte(payload)) + "." + base64.RawURLEncoding.EncodeToString(sig)
}

func (a *Authenticator) verify(token string) (string, bool) {
	parts := strings.SplitN(token, ".", 2)
	if len(parts) != 2 {
		return "", false
	}
	payloadBytes, err := base64.RawURLEncoding.DecodeString(parts[0])
	if err != nil {
		return "", false
	}
	sigBytes, err := base64.RawURLEncoding.DecodeString(parts[1])
	if err != nil {
		return "", false
	}

	mac := hmac.New(sha256.New, a.secret)
	mac.Write(payloadBytes)
	expected := mac.Sum(nil)

	if !hmac.Equal(expected, sigBytes) {
		return "", false
	}
	return string(payloadBytes), true
}
