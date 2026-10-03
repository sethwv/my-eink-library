package users

import (
	"crypto/rand"
	"database/sql"
	"encoding/base64"
	"fmt"
	"time"

	"golang.org/x/crypto/bcrypt"
)

// timedTokenSpec is a closed set of token storage and policy details. Its
// columns are constants defined in this package, never caller-controlled SQL.
type timedTokenSpec struct {
	hashColumn      string
	createdAtColumn string
	ttl             time.Duration
	invalidMessage  string
}

var (
	resetTokenSpec = timedTokenSpec{
		hashColumn: "reset_token_hash", createdAtColumn: "reset_token_created_at",
		ttl: time.Hour, invalidMessage: "invalid or expired reset link",
	}
	inviteTokenSpec = timedTokenSpec{
		hashColumn: "invite_token_hash", createdAtColumn: "invite_token_created_at",
		ttl: 7 * 24 * time.Hour, invalidMessage: "invalid or expired invite link",
	}
)

func (s *Store) GenerateBookmarkToken(username string) (string, error) {
	if !s.Can(username, PermissionBookmarkLink) {
		return "", fmt.Errorf("bookmark links are not permitted")
	}
	token, hash, err := newToken()
	if err != nil {
		return "", fmt.Errorf("generate bookmark token: %w", err)
	}
	_, err = s.sql.Exec(`UPDATE users SET bookmark_token_hash = ?, bookmark_token_created_at = ? WHERE username = ?`, hash, time.Now().Unix(), username)
	return token, err
}

func (s *Store) RevokeBookmarkToken(username string) error {
	_, err := s.sql.Exec(`UPDATE users SET bookmark_token_hash = NULL, bookmark_token_created_at = NULL WHERE username = ?`, username)
	return err
}

func (s *Store) HasBookmarkToken(username string) bool {
	var hash sql.NullString
	err := s.sql.QueryRow(`SELECT bookmark_token_hash FROM users WHERE username = ?`, username).Scan(&hash)
	return err == nil && hash.Valid && hash.String != ""
}

func (s *Store) VerifyBookmarkToken(token string) (string, bool) {
	if token == "" {
		return "", false
	}
	rows, err := s.sql.Query(`SELECT u.username, u.bookmark_token_hash FROM users u LEFT JOIN user_permission_overrides p ON p.user_id = u.id AND p.permission = 'bookmark_link' WHERE u.bookmark_token_hash IS NOT NULL AND u.enabled = 1 AND (p.granted IS NULL OR p.granted = 1)`)
	if err != nil {
		return "", false
	}
	defer rows.Close()
	for rows.Next() {
		var username, hash string
		if err := rows.Scan(&username, &hash); err != nil {
			continue
		}
		if bcrypt.CompareHashAndPassword([]byte(hash), []byte(token)) == nil {
			return username, true
		}
	}
	return "", false
}

func (s *Store) RequestPasswordReset(email string) (token, username string, found bool, err error) {
	if email == "" {
		return "", "", false, nil
	}
	err = s.sql.QueryRow(`SELECT username FROM users WHERE email = ? AND enabled = 1`, email).Scan(&username)
	if err == sql.ErrNoRows {
		return "", "", false, nil
	}
	if err != nil {
		return "", "", false, err
	}
	token, err = s.issueTimedToken(resetTokenSpec, `username = ?`, username)
	if err != nil {
		return "", "", false, fmt.Errorf("generate reset token: %w", err)
	}
	return token, username, true, nil
}

func (s *Store) VerifyResetToken(token string) (string, bool) {
	return s.verifyTimedToken(resetTokenSpec, token)
}

func (s *Store) CompletePasswordReset(token, newPassword string) error {
	return s.consumeTimedToken(resetTokenSpec, token, newPassword)
}

func (s *Store) InviteUser(username, email, role string, canBookmark bool) (token string, err error) {
	if username == "" || email == "" {
		return "", fmt.Errorf("username and email are required")
	}
	if !validRole(role) {
		return "", fmt.Errorf("invalid role %q", role)
	}
	if err := s.checkEmailAvailable(email, 0); err != nil {
		return "", err
	}
	randomPassword := make([]byte, 32)
	if _, err := rand.Read(randomPassword); err != nil {
		return "", fmt.Errorf("generate placeholder password: %w", err)
	}
	passwordHash, err := bcrypt.GenerateFromPassword(randomPassword, bcrypt.DefaultCost)
	if err != nil {
		return "", fmt.Errorf("hash placeholder password: %w", err)
	}
	token, hash, err := newToken()
	if err != nil {
		return "", fmt.Errorf("generate invite token: %w", err)
	}
	result, err := s.sql.Exec(`INSERT INTO users (username, password_hash, is_admin, role, can_bookmark, email, invite_token_hash, invite_token_created_at, created_at) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)`, username, string(passwordHash), boolToInt(role == RoleAdmin), role, boolToInt(canBookmark), email, hash, time.Now().Unix(), time.Now().Unix())
	if err != nil {
		return "", err
	}
	if canBookmark {
		return token, nil
	}
	id, err := result.LastInsertId()
	if err != nil {
		return "", err
	}
	denied := false
	if err := s.SetPermissionOverride(id, PermissionBookmarkLink, &denied); err != nil {
		return "", err
	}
	return token, nil
}

func (s *Store) ResendInvite(id int64) (string, error) {
	var pending sql.NullString
	if err := s.sql.QueryRow(`SELECT invite_token_hash FROM users WHERE id = ? AND enabled = 1`, id).Scan(&pending); err != nil {
		return "", err
	}
	if !pending.Valid || pending.String == "" {
		return "", fmt.Errorf("user has no pending invite")
	}
	token, err := s.issueTimedToken(inviteTokenSpec, `id = ?`, id)
	if err != nil {
		return "", fmt.Errorf("generate invite token: %w", err)
	}
	return token, nil
}

func (s *Store) VerifyInviteToken(token string) (string, bool) {
	return s.verifyTimedToken(inviteTokenSpec, token)
}

func (s *Store) AcceptInvite(token, password string) error {
	return s.consumeTimedToken(inviteTokenSpec, token, password)
}

func (s *Store) issueTimedToken(spec timedTokenSpec, where string, arg any) (string, error) {
	token, hash, err := newToken()
	if err != nil {
		return "", err
	}
	query := `UPDATE users SET ` + spec.hashColumn + ` = ?, ` + spec.createdAtColumn + ` = ? WHERE ` + where
	_, err = s.sql.Exec(query, hash, time.Now().Unix(), arg)
	return token, err
}

func (s *Store) consumeTimedToken(spec timedTokenSpec, token, password string) error {
	tx, err := s.sql.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	username, ok := verifyTimedToken(tx, spec, token)
	if !ok {
		return fmt.Errorf("%s", spec.invalidMessage)
	}
	hash, err := passwordHash(password)
	if err != nil {
		return err
	}
	_, err = tx.Exec(`UPDATE users SET password_hash = ?, `+spec.hashColumn+` = NULL, `+spec.createdAtColumn+` = NULL WHERE username = ?`, hash, username)
	if err != nil {
		return err
	}
	return tx.Commit()
}

type tokenQueryer interface {
	Query(string, ...any) (*sql.Rows, error)
}

func (s *Store) verifyTimedToken(spec timedTokenSpec, token string) (string, bool) {
	return verifyTimedToken(s.sql, spec, token)
}

func verifyTimedToken(db tokenQueryer, spec timedTokenSpec, token string) (string, bool) {
	if token == "" {
		return "", false
	}
	query := `SELECT username, ` + spec.hashColumn + `, ` + spec.createdAtColumn + ` FROM users WHERE enabled = 1 AND ` + spec.hashColumn + ` IS NOT NULL`
	rows, err := db.Query(query)
	if err != nil {
		return "", false
	}
	defer rows.Close()
	cutoff := time.Now().Add(-spec.ttl).Unix()
	for rows.Next() {
		var username, hash string
		var createdAt int64
		if err := rows.Scan(&username, &hash, &createdAt); err != nil || createdAt < cutoff {
			continue
		}
		if bcrypt.CompareHashAndPassword([]byte(hash), []byte(token)) == nil {
			return username, true
		}
	}
	return "", false
}

func newToken() (raw, hash string, err error) {
	buf := make([]byte, 32)
	if _, err := rand.Read(buf); err != nil {
		return "", "", err
	}
	raw = base64.RawURLEncoding.EncodeToString(buf)
	hashed, err := bcrypt.GenerateFromPassword([]byte(raw), bcrypt.DefaultCost)
	if err != nil {
		return "", "", err
	}
	return raw, string(hashed), nil
}
