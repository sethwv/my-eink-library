// Package auth handles the single-user login flow: password check via
// bcrypt and a stateless HMAC-signed session cookie (no server-side session store).
package auth

import (
	"crypto/hmac"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"

	"golang.org/x/crypto/bcrypt"
)

const CookieName = "eink_session"

type Authenticator struct {
	username     string
	passwordHash []byte
	secret       []byte
	ttl          time.Duration
}

// New hashes the configured password once at startup so it's never compared in plaintext.
func New(username, password, secret string, ttl time.Duration) (*Authenticator, error) {
	hash, err := bcrypt.GenerateFromPassword([]byte(password), bcrypt.DefaultCost)
	if err != nil {
		return nil, fmt.Errorf("hash password: %w", err)
	}
	return &Authenticator{
		username:     username,
		passwordHash: hash,
		secret:       []byte(secret),
		ttl:          ttl,
	}, nil
}

// CheckPassword reports whether the given credentials match the configured user/pass.
func (a *Authenticator) CheckPassword(username, password string) bool {
	if subtle.ConstantTimeCompare([]byte(username), []byte(a.username)) != 1 {
		// Still run bcrypt so failed-username timing doesn't leak whether the
		// username was right, at the cost of one extra hash comparison.
		bcrypt.CompareHashAndPassword(a.passwordHash, []byte(password))
		return false
	}
	return bcrypt.CompareHashAndPassword(a.passwordHash, []byte(password)) == nil
}

// IssueSession sets a signed session cookie for username.
func (a *Authenticator) IssueSession(w http.ResponseWriter, r *http.Request) {
	expires := time.Now().Add(a.ttl)
	payload := a.username + "|" + strconv.FormatInt(expires.Unix(), 10)
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

// VerifySession reports whether the request carries a valid, unexpired session cookie.
func (a *Authenticator) VerifySession(r *http.Request) bool {
	c, err := r.Cookie(CookieName)
	if err != nil || c.Value == "" {
		return false
	}

	payload, ok := a.verify(c.Value)
	if !ok {
		return false
	}

	parts := strings.SplitN(payload, "|", 2)
	if len(parts) != 2 {
		return false
	}
	if subtle.ConstantTimeCompare([]byte(parts[0]), []byte(a.username)) != 1 {
		return false
	}
	exp, err := strconv.ParseInt(parts[1], 10, 64)
	if err != nil {
		return false
	}
	return time.Now().Unix() < exp
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
