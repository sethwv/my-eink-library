// Package users manages login accounts in a small SQLite-backed store,
// separate from the book index so the two concerns stay decoupled.
package users

import (
	"crypto/rand"
	"database/sql"
	"encoding/base64"
	"fmt"
	"time"

	"golang.org/x/crypto/bcrypt"
	_ "modernc.org/sqlite"
)

const schema = `
CREATE TABLE IF NOT EXISTS users (
    id                         INTEGER PRIMARY KEY AUTOINCREMENT,
    username                   TEXT NOT NULL UNIQUE,
    password_hash              TEXT NOT NULL,
    is_admin                   INTEGER NOT NULL DEFAULT 0,
    created_at                 INTEGER NOT NULL,
    bookmark_token_hash        TEXT,
    bookmark_token_created_at  INTEGER
);
`

// dummyHash is compared against on username-not-found so failed logins take
// roughly the same time whether or not the username exists.
var dummyHash, _ = bcrypt.GenerateFromPassword([]byte("dummy-password-for-timing"), bcrypt.DefaultCost)

type User struct {
	ID       int64
	Username string
	IsAdmin  bool
}

type Store struct {
	sql *sql.DB
}

// Open opens (creating if necessary) the users database at dbPath.
func Open(dbPath string) (*Store, error) {
	sdb, err := sql.Open("sqlite", dbPath+"?_pragma=busy_timeout(5000)&_pragma=journal_mode(WAL)")
	if err != nil {
		return nil, fmt.Errorf("open users db: %w", err)
	}
	sdb.SetMaxOpenConns(1)

	if _, err := sdb.Exec(schema); err != nil {
		sdb.Close()
		return nil, fmt.Errorf("apply users schema: %w", err)
	}

	store := &Store{sql: sdb}
	if err := store.migrateBookmarkTokenColumns(); err != nil {
		sdb.Close()
		return nil, fmt.Errorf("migrate users schema: %w", err)
	}

	return store, nil
}

// migrateBookmarkTokenColumns adds the bookmark_token_hash/created_at
// columns to a users table that predates them. CREATE TABLE IF NOT EXISTS
// in schema only applies to brand-new databases, so an existing users.db
// needs its columns added in place — there's no migration framework here,
// just an additive, idempotent ALTER TABLE guarded by checking what
// columns already exist.
func (s *Store) migrateBookmarkTokenColumns() error {
	rows, err := s.sql.Query(`PRAGMA table_info(users)`)
	if err != nil {
		return err
	}
	existing := map[string]bool{}
	for rows.Next() {
		var cid int
		var name, colType string
		var notNull, pk int
		var dflt sql.NullString
		if err := rows.Scan(&cid, &name, &colType, &notNull, &dflt, &pk); err != nil {
			rows.Close()
			return err
		}
		existing[name] = true
	}
	if err := rows.Err(); err != nil {
		return err
	}
	rows.Close()

	if !existing["bookmark_token_hash"] {
		if _, err := s.sql.Exec(`ALTER TABLE users ADD COLUMN bookmark_token_hash TEXT`); err != nil {
			return err
		}
	}
	if !existing["bookmark_token_created_at"] {
		if _, err := s.sql.Exec(`ALTER TABLE users ADD COLUMN bookmark_token_created_at INTEGER`); err != nil {
			return err
		}
	}
	return nil
}

func (s *Store) Close() error {
	return s.sql.Close()
}

// Bootstrap creates the first user (as admin) from the given credentials if
// the users table is empty. Once any user exists, this is a no-op — the
// bootstrap env vars only matter for the very first startup.
func (s *Store) Bootstrap(username, password string) error {
	var n int
	if err := s.sql.QueryRow(`SELECT COUNT(*) FROM users`).Scan(&n); err != nil {
		return err
	}
	if n > 0 {
		return nil
	}
	return s.Create(username, password, true)
}

// CheckPassword reports whether username/password is a valid login.
func (s *Store) CheckPassword(username, password string) bool {
	var hash string
	err := s.sql.QueryRow(`SELECT password_hash FROM users WHERE username = ?`, username).Scan(&hash)
	if err != nil {
		bcrypt.CompareHashAndPassword(dummyHash, []byte(password))
		return false
	}
	return bcrypt.CompareHashAndPassword([]byte(hash), []byte(password)) == nil
}

// IsAdmin reports whether username exists and is an admin.
func (s *Store) IsAdmin(username string) bool {
	var isAdmin int
	err := s.sql.QueryRow(`SELECT is_admin FROM users WHERE username = ?`, username).Scan(&isAdmin)
	return err == nil && isAdmin != 0
}

// List returns all users ordered by username.
func (s *Store) List() ([]User, error) {
	rows, err := s.sql.Query(`SELECT id, username, is_admin FROM users ORDER BY username`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []User
	for rows.Next() {
		var u User
		var isAdmin int
		if err := rows.Scan(&u.ID, &u.Username, &isAdmin); err != nil {
			return nil, err
		}
		u.IsAdmin = isAdmin != 0
		out = append(out, u)
	}
	return out, rows.Err()
}

// Create adds a new user with a bcrypt-hashed password.
func (s *Store) Create(username, password string, isAdmin bool) error {
	if username == "" || password == "" {
		return fmt.Errorf("username and password are required")
	}
	hash, err := bcrypt.GenerateFromPassword([]byte(password), bcrypt.DefaultCost)
	if err != nil {
		return fmt.Errorf("hash password: %w", err)
	}
	_, err = s.sql.Exec(
		`INSERT INTO users (username, password_hash, is_admin, created_at) VALUES (?, ?, ?, ?)`,
		username, string(hash), boolToInt(isAdmin), time.Now().Unix(),
	)
	return err
}

// Delete removes a user by id, refusing to delete the last remaining admin.
func (s *Store) Delete(id int64) error {
	var isAdmin int
	if err := s.sql.QueryRow(`SELECT is_admin FROM users WHERE id = ?`, id).Scan(&isAdmin); err != nil {
		return err
	}
	if isAdmin != 0 {
		var adminCount int
		if err := s.sql.QueryRow(`SELECT COUNT(*) FROM users WHERE is_admin = 1`).Scan(&adminCount); err != nil {
			return err
		}
		if adminCount <= 1 {
			return fmt.Errorf("cannot delete the last remaining admin")
		}
	}
	_, err := s.sql.Exec(`DELETE FROM users WHERE id = ?`, id)
	return err
}

// ResetPassword sets a new password for the given user id.
func (s *Store) ResetPassword(id int64, newPassword string) error {
	if newPassword == "" {
		return fmt.Errorf("password is required")
	}
	hash, err := bcrypt.GenerateFromPassword([]byte(newPassword), bcrypt.DefaultCost)
	if err != nil {
		return fmt.Errorf("hash password: %w", err)
	}
	_, err = s.sql.Exec(`UPDATE users SET password_hash = ? WHERE id = ?`, string(hash), id)
	return err
}

// GenerateBookmarkToken issues a new random bookmark token for username,
// replacing any existing one — only one is ever active at a time, matching
// a single "regenerate" action that revokes-then-creates in one step. The
// plaintext token is returned but never stored: only a bcrypt hash of it is
// kept, the same way a password would be, even though the token is meant to
// be a lower-security convenience (a bookmarkable login link) rather than a
// primary credential.
func (s *Store) GenerateBookmarkToken(username string) (string, error) {
	raw := make([]byte, 32)
	if _, err := rand.Read(raw); err != nil {
		return "", fmt.Errorf("generate bookmark token: %w", err)
	}
	token := base64.RawURLEncoding.EncodeToString(raw)

	hash, err := bcrypt.GenerateFromPassword([]byte(token), bcrypt.DefaultCost)
	if err != nil {
		return "", fmt.Errorf("hash bookmark token: %w", err)
	}
	_, err = s.sql.Exec(
		`UPDATE users SET bookmark_token_hash = ?, bookmark_token_created_at = ? WHERE username = ?`,
		string(hash), time.Now().Unix(), username,
	)
	if err != nil {
		return "", err
	}
	return token, nil
}

// RevokeBookmarkToken clears username's bookmark token, if any.
func (s *Store) RevokeBookmarkToken(username string) error {
	_, err := s.sql.Exec(
		`UPDATE users SET bookmark_token_hash = NULL, bookmark_token_created_at = NULL WHERE username = ?`,
		username,
	)
	return err
}

// HasBookmarkToken reports whether username currently has an active
// bookmark token, so the account page can show "regenerate" vs "create".
func (s *Store) HasBookmarkToken(username string) bool {
	var hash sql.NullString
	err := s.sql.QueryRow(`SELECT bookmark_token_hash FROM users WHERE username = ?`, username).Scan(&hash)
	return err == nil && hash.Valid && hash.String != ""
}

// VerifyBookmarkToken reports which username, if any, a bookmark token
// belongs to. bcrypt hashes can't be looked up by index, so this checks
// against every user with an active token — fine at the scale of a personal
// library's user count.
func (s *Store) VerifyBookmarkToken(token string) (string, bool) {
	if token == "" {
		return "", false
	}
	rows, err := s.sql.Query(`SELECT username, bookmark_token_hash FROM users WHERE bookmark_token_hash IS NOT NULL`)
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

func boolToInt(b bool) int {
	if b {
		return 1
	}
	return 0
}
