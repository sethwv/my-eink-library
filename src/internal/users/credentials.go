package users

import (
	"crypto/rand"
	"database/sql"
	"encoding/base64"
	"fmt"
)

const primaryAdminUsername = "admin"

// SessionSecret returns the persisted session secret, creating it when absent.
func (s *Store) SessionSecret() (secret string, created bool, err error) {
	err = s.sql.QueryRow(`SELECT session_secret FROM bootstrap_settings WHERE id = 1`).Scan(&secret)
	if err == nil && secret != "" {
		return secret, false, nil
	}
	if err != nil && err != sql.ErrNoRows {
		return "", false, err
	}

	secret, err = randomValue(32)
	if err != nil {
		return "", false, fmt.Errorf("generate session secret: %w", err)
	}
	_, err = s.sql.Exec(`INSERT INTO bootstrap_settings (id, session_secret) VALUES (1, ?) ON CONFLICT(id) DO UPDATE SET session_secret = excluded.session_secret WHERE bootstrap_settings.session_secret = ''`, secret)
	if err != nil {
		return "", false, err
	}
	return secret, true, nil
}

// BootstrapPrimaryAdmin creates the fixed first administrator only when no
// users exist. The returned password is available only to the caller that
// created it and is never stored in plaintext.
func (s *Store) BootstrapPrimaryAdmin() (username, password string, created bool, err error) {
	var n int
	if err := s.sql.QueryRow(`SELECT COUNT(*) FROM users`).Scan(&n); err != nil {
		return "", "", false, err
	}
	if n > 0 {
		return "", "", false, nil
	}
	password, err = randomValue(24)
	if err != nil {
		return "", "", false, fmt.Errorf("generate administrator password: %w", err)
	}
	if err := s.Create(primaryAdminUsername, password, RoleAdmin, true, ""); err != nil {
		return "", "", false, err
	}
	return primaryAdminUsername, password, true, nil
}

// ResetUserPassword generates and sets a new password for username.
func (s *Store) ResetUserPassword(username string) (string, error) {
	u, err := s.UserByUsername(username)
	if err != nil {
		return "", err
	}
	if u == nil {
		return "", fmt.Errorf("user %q does not exist", username)
	}
	password, err := randomValue(24)
	if err != nil {
		return "", fmt.Errorf("generate password: %w", err)
	}
	if err := s.ResetPassword(u.ID, password); err != nil {
		return "", err
	}
	return password, nil
}

func randomValue(bytes int) (string, error) {
	b := make([]byte, bytes)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(b), nil
}
