package users

import (
	"crypto/rand"
	"database/sql"
	"encoding/base64"
	"fmt"
)

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

func randomValue(bytes int) (string, error) {
	b := make([]byte, bytes)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(b), nil
}
