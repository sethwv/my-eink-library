// Package users manages login accounts in a small SQLite-backed store,
// separate from the book index so the two concerns stay decoupled.
package users

import (
	"crypto/rand"
	"database/sql"
	"encoding/base64"
	"fmt"
	"time"

	"github.com/swvn/eink-library/internal/mail"
	"golang.org/x/crypto/bcrypt"
	_ "modernc.org/sqlite"
)

// Role tiers, replacing the old single is_admin boolean. Stored as plain
// TEXT (SQLite has no enum type); admin has both capabilities, the two
// "manager" tiers have exactly one, and member has neither.
const (
	RoleAdmin         = "admin"
	RoleUserManager   = "user_manager"
	RoleServerManager = "server_manager"
	RoleMember        = "member"
)

// resetTokenTTL/inviteTokenTTL bound how long a password-reset or invite
// link stays usable after being issued.
const (
	resetTokenTTL  = time.Hour
	inviteTokenTTL = 7 * 24 * time.Hour
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

CREATE TABLE IF NOT EXISTS smtp_settings (
    id         INTEGER PRIMARY KEY CHECK (id = 1),
    host       TEXT NOT NULL DEFAULT '',
    port       INTEGER NOT NULL DEFAULT 465,
    encryption TEXT NOT NULL DEFAULT 'tls',
    username   TEXT NOT NULL DEFAULT '',
    password   TEXT NOT NULL DEFAULT '',
    from_name  TEXT NOT NULL DEFAULT '',
    from_addr  TEXT NOT NULL DEFAULT ''
);
`

// dummyHash is compared against on username-not-found so failed logins take
// roughly the same time whether or not the username exists.
var dummyHash, _ = bcrypt.GenerateFromPassword([]byte("dummy-password-for-timing"), bcrypt.DefaultCost)

type User struct {
	ID               int64
	Username         string
	IsAdmin          bool // derived: Role == RoleAdmin, kept for template/back-compat convenience
	Role             string
	CanManageUsers   bool
	CanManageServer  bool
	CanBookmark      bool
	Email            string
	DigestSubscribed bool
	InvitePending    bool
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
	if err := store.migrateColumns(); err != nil {
		sdb.Close()
		return nil, fmt.Errorf("migrate users schema: %w", err)
	}

	return store, nil
}

// migrateColumns adds every column introduced after the original schema to
// a users table that predates them. CREATE TABLE IF NOT EXISTS in schema
// only applies to brand-new databases, so an existing users.db needs its
// columns added in place. There's no migration framework here, just an
// additive, idempotent ALTER TABLE guarded by checking what columns already
// exist. role/can_bookmark are backfilled from is_admin only on the run
// that first adds them, so every existing admin keeps full admin (both
// capabilities) and every existing user keeps bookmark-link access.
func (s *Store) migrateColumns() error {
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

	newColumns := []struct{ name, ddl string }{
		{"bookmark_token_hash", "TEXT"},
		{"bookmark_token_created_at", "INTEGER"},
		{"role", "TEXT NOT NULL DEFAULT 'member'"},
		{"can_bookmark", "INTEGER NOT NULL DEFAULT 1"},
		{"email", "TEXT"},
		{"reset_token_hash", "TEXT"},
		{"reset_token_created_at", "INTEGER"},
		{"invite_token_hash", "TEXT"},
		{"invite_token_created_at", "INTEGER"},
		{"digest_subscribed", "INTEGER NOT NULL DEFAULT 0"},
	}
	roleColumnIsNew := !existing["role"]
	for _, c := range newColumns {
		if existing[c.name] {
			continue
		}
		if _, err := s.sql.Exec(fmt.Sprintf(`ALTER TABLE users ADD COLUMN %s %s`, c.name, c.ddl)); err != nil {
			return err
		}
	}
	if roleColumnIsNew {
		if _, err := s.sql.Exec(`UPDATE users SET role = 'admin' WHERE is_admin = 1`); err != nil {
			return err
		}
	}
	return nil
}

func (s *Store) Close() error {
	return s.sql.Close()
}

// Bootstrap creates the first user (as admin) from the given credentials if
// the users table is empty. Once any user exists, this is a no-op: the
// bootstrap env vars only matter for the very first startup.
func (s *Store) Bootstrap(username, password string) error {
	var n int
	if err := s.sql.QueryRow(`SELECT COUNT(*) FROM users`).Scan(&n); err != nil {
		return err
	}
	if n > 0 {
		return nil
	}
	return s.Create(username, password, RoleAdmin, true, "")
}

// CheckPassword reports whether username/password is a valid login. Users
// with a pending invite (password_hash is an unguessable random value set
// by InviteUser) always fail here until AcceptInvite sets a real password.
func (s *Store) CheckPassword(username, password string) bool {
	var hash string
	err := s.sql.QueryRow(`SELECT password_hash FROM users WHERE username = ?`, username).Scan(&hash)
	if err != nil {
		bcrypt.CompareHashAndPassword(dummyHash, []byte(password))
		return false
	}
	return bcrypt.CompareHashAndPassword([]byte(hash), []byte(password)) == nil
}

// IsAdmin reports whether username exists and has the admin role.
func (s *Store) IsAdmin(username string) bool {
	role, ok := s.role(username)
	return ok && role == RoleAdmin
}

// CanManageUsers reports whether username may reach /admin/users.
func (s *Store) CanManageUsers(username string) bool {
	role, ok := s.role(username)
	return ok && (role == RoleAdmin || role == RoleUserManager)
}

// CanManageServer reports whether username may reach /admin/server.
func (s *Store) CanManageServer(username string) bool {
	role, ok := s.role(username)
	return ok && (role == RoleAdmin || role == RoleServerManager)
}

// CanUseBookmark reports whether username is currently allowed a bookmark
// link at all (independent of whether one is issued/active).
func (s *Store) CanUseBookmark(username string) bool {
	var canBookmark int
	err := s.sql.QueryRow(`SELECT can_bookmark FROM users WHERE username = ?`, username).Scan(&canBookmark)
	return err == nil && canBookmark != 0
}

func (s *Store) role(username string) (string, bool) {
	var role string
	err := s.sql.QueryRow(`SELECT role FROM users WHERE username = ?`, username).Scan(&role)
	if err != nil {
		return "", false
	}
	return role, true
}

// List returns all users ordered by username.
func (s *Store) List() ([]User, error) {
	rows, err := s.sql.Query(`
		SELECT id, username, role, can_bookmark, email, digest_subscribed,
		       invite_token_hash IS NOT NULL AND invite_token_hash != ''
		FROM users ORDER BY username`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []User
	for rows.Next() {
		var u User
		var canBookmark, digestSubscribed, invitePending int
		var email sql.NullString
		if err := rows.Scan(&u.ID, &u.Username, &u.Role, &canBookmark, &email, &digestSubscribed, &invitePending); err != nil {
			return nil, err
		}
		u.IsAdmin = u.Role == RoleAdmin
		u.CanManageUsers = u.Role == RoleAdmin || u.Role == RoleUserManager
		u.CanManageServer = u.Role == RoleAdmin || u.Role == RoleServerManager
		u.CanBookmark = canBookmark != 0
		u.Email = email.String
		u.DigestSubscribed = digestSubscribed != 0
		u.InvitePending = invitePending != 0
		out = append(out, u)
	}
	return out, rows.Err()
}

// Create adds a new user with a bcrypt-hashed password. email may be empty,
// only needed if the user should be reachable by the forgot-password flow.
func (s *Store) Create(username, password, role string, canBookmark bool, email string) error {
	if username == "" || password == "" {
		return fmt.Errorf("username and password are required")
	}
	if !validRole(role) {
		return fmt.Errorf("invalid role %q", role)
	}
	if err := s.checkEmailAvailable(email, 0); err != nil {
		return err
	}
	hash, err := bcrypt.GenerateFromPassword([]byte(password), bcrypt.DefaultCost)
	if err != nil {
		return fmt.Errorf("hash password: %w", err)
	}
	_, err = s.sql.Exec(
		`INSERT INTO users (username, password_hash, is_admin, role, can_bookmark, email, created_at) VALUES (?, ?, ?, ?, ?, ?, ?)`,
		username, string(hash), boolToInt(role == RoleAdmin), role, boolToInt(canBookmark), nullIfEmpty(email), time.Now().Unix(),
	)
	return err
}

// SetRole updates an existing user's role and bookmark-link permission,
// refusing to demote the last remaining admin.
func (s *Store) SetRole(id int64, role string, canBookmark bool) error {
	if !validRole(role) {
		return fmt.Errorf("invalid role %q", role)
	}
	if role != RoleAdmin {
		var currentRole string
		if err := s.sql.QueryRow(`SELECT role FROM users WHERE id = ?`, id).Scan(&currentRole); err != nil {
			return err
		}
		if currentRole == RoleAdmin {
			var adminCount int
			if err := s.sql.QueryRow(`SELECT COUNT(*) FROM users WHERE role = 'admin'`).Scan(&adminCount); err != nil {
				return err
			}
			if adminCount <= 1 {
				return fmt.Errorf("cannot demote the last remaining admin")
			}
		}
	}
	_, err := s.sql.Exec(
		`UPDATE users SET role = ?, is_admin = ?, can_bookmark = ? WHERE id = ?`,
		role, boolToInt(role == RoleAdmin), boolToInt(canBookmark), id,
	)
	return err
}

func validRole(role string) bool {
	switch role {
	case RoleAdmin, RoleUserManager, RoleServerManager, RoleMember:
		return true
	}
	return false
}

// Delete removes a user by id, refusing to delete the last remaining admin.
func (s *Store) Delete(id int64) error {
	var role string
	if err := s.sql.QueryRow(`SELECT role FROM users WHERE id = ?`, id).Scan(&role); err != nil {
		return err
	}
	if role == RoleAdmin {
		var adminCount int
		if err := s.sql.QueryRow(`SELECT COUNT(*) FROM users WHERE role = 'admin'`).Scan(&adminCount); err != nil {
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

// SetDigestSubscribed toggles a user's opt-in to the weekly new-book digest.
func (s *Store) SetDigestSubscribed(username string, subscribed bool) error {
	_, err := s.sql.Exec(`UPDATE users SET digest_subscribed = ? WHERE username = ?`, boolToInt(subscribed), username)
	return err
}

// IsDigestSubscribed reports whether username is opted into the digest.
func (s *Store) IsDigestSubscribed(username string) bool {
	var subscribed int
	err := s.sql.QueryRow(`SELECT digest_subscribed FROM users WHERE username = ?`, username).Scan(&subscribed)
	return err == nil && subscribed != 0
}

// DigestSubscribers returns the email addresses of every user opted into
// the weekly new-book digest with an email on file.
func (s *Store) DigestSubscribers() ([]string, error) {
	rows, err := s.sql.Query(`SELECT email FROM users WHERE digest_subscribed = 1 AND email IS NOT NULL AND email != ''`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []string
	for rows.Next() {
		var email string
		if err := rows.Scan(&email); err != nil {
			return nil, err
		}
		out = append(out, email)
	}
	return out, rows.Err()
}

// GenerateBookmarkToken issues a new random bookmark token for username,
// replacing any existing one: only one is ever active at a time, matching
// a single "regenerate" action that revokes-then-creates in one step. The
// plaintext token is returned but never stored: only a bcrypt hash of it is
// kept, the same way a password would be, even though the token is meant to
// be a lower-security convenience (a bookmarkable login link) rather than a
// primary credential.
func (s *Store) GenerateBookmarkToken(username string) (string, error) {
	token, hash, err := newToken()
	if err != nil {
		return "", fmt.Errorf("generate bookmark token: %w", err)
	}
	_, err = s.sql.Exec(
		`UPDATE users SET bookmark_token_hash = ?, bookmark_token_created_at = ? WHERE username = ?`,
		hash, time.Now().Unix(), username,
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
// against every user with an active token (fine at the scale of a personal
// library's user count). Users whose bookmark-link permission was revoked
// (can_bookmark = 0) never match, even with a still-valid stored hash.
func (s *Store) VerifyBookmarkToken(token string) (string, bool) {
	if token == "" {
		return "", false
	}
	rows, err := s.sql.Query(`SELECT username, bookmark_token_hash FROM users WHERE bookmark_token_hash IS NOT NULL AND can_bookmark = 1`)
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

// RequestPasswordReset issues a reset token for the user with the given
// email, if any. found is false (with no error) when no user has that
// email on file. Callers should show the same generic "email sent"
// message either way, to avoid leaking which addresses are registered.
func (s *Store) RequestPasswordReset(email string) (token, username string, found bool, err error) {
	if email == "" {
		return "", "", false, nil
	}
	err = s.sql.QueryRow(`SELECT username FROM users WHERE email = ?`, email).Scan(&username)
	if err == sql.ErrNoRows {
		return "", "", false, nil
	}
	if err != nil {
		return "", "", false, err
	}

	token, hash, err := newToken()
	if err != nil {
		return "", "", false, fmt.Errorf("generate reset token: %w", err)
	}
	_, err = s.sql.Exec(
		`UPDATE users SET reset_token_hash = ?, reset_token_created_at = ? WHERE username = ?`,
		hash, time.Now().Unix(), username,
	)
	if err != nil {
		return "", "", false, err
	}
	return token, username, true, nil
}

// VerifyResetToken reports which username, if any, an unexpired reset token
// belongs to.
func (s *Store) VerifyResetToken(token string) (string, bool) {
	return s.verifyTimedToken(token, "reset_token_hash", "reset_token_created_at", resetTokenTTL)
}

// CompletePasswordReset sets a new password for the account owning token
// and clears the token so it can't be reused.
func (s *Store) CompletePasswordReset(token, newPassword string) error {
	username, ok := s.VerifyResetToken(token)
	if !ok {
		return fmt.Errorf("invalid or expired reset link")
	}
	var id int64
	if err := s.sql.QueryRow(`SELECT id FROM users WHERE username = ?`, username).Scan(&id); err != nil {
		return err
	}
	if err := s.ResetPassword(id, newPassword); err != nil {
		return err
	}
	_, err := s.sql.Exec(`UPDATE users SET reset_token_hash = NULL, reset_token_created_at = NULL WHERE id = ?`, id)
	return err
}

// InviteUser creates a new account that can't log in with a password until
// AcceptInvite is called: password_hash is set to a bcrypt hash of random
// bytes nobody knows, so ordinary login attempts always fail, without
// needing a separate "pending" flag anywhere else in the schema.
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

	token, tokenHash, err := newToken()
	if err != nil {
		return "", fmt.Errorf("generate invite token: %w", err)
	}

	_, err = s.sql.Exec(
		`INSERT INTO users (username, password_hash, is_admin, role, can_bookmark, email, invite_token_hash, invite_token_created_at, created_at)
		 VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		username, string(passwordHash), boolToInt(role == RoleAdmin), role, boolToInt(canBookmark), email,
		tokenHash, time.Now().Unix(), time.Now().Unix(),
	)
	if err != nil {
		return "", err
	}
	return token, nil
}

// ResendInvite issues a fresh invite token for a user with a still-pending
// invite, invalidating the previous link.
func (s *Store) ResendInvite(id int64) (token string, err error) {
	var pending sql.NullString
	if err := s.sql.QueryRow(`SELECT invite_token_hash FROM users WHERE id = ?`, id).Scan(&pending); err != nil {
		return "", err
	}
	if !pending.Valid || pending.String == "" {
		return "", fmt.Errorf("user has no pending invite")
	}

	token, tokenHash, err := newToken()
	if err != nil {
		return "", fmt.Errorf("generate invite token: %w", err)
	}
	_, err = s.sql.Exec(
		`UPDATE users SET invite_token_hash = ?, invite_token_created_at = ? WHERE id = ?`,
		tokenHash, time.Now().Unix(), id,
	)
	if err != nil {
		return "", err
	}
	return token, nil
}

// VerifyInviteToken reports which username, if any, an unexpired invite
// token belongs to.
func (s *Store) VerifyInviteToken(token string) (string, bool) {
	return s.verifyTimedToken(token, "invite_token_hash", "invite_token_created_at", inviteTokenTTL)
}

// AcceptInvite sets the invited account's real password and clears the
// invite token, so it's usable exactly once.
func (s *Store) AcceptInvite(token, password string) error {
	username, ok := s.VerifyInviteToken(token)
	if !ok {
		return fmt.Errorf("invalid or expired invite link")
	}
	var id int64
	if err := s.sql.QueryRow(`SELECT id FROM users WHERE username = ?`, username).Scan(&id); err != nil {
		return err
	}
	if err := s.ResetPassword(id, password); err != nil {
		return err
	}
	_, err := s.sql.Exec(`UPDATE users SET invite_token_hash = NULL, invite_token_created_at = NULL WHERE id = ?`, id)
	return err
}

// verifyTimedToken bcrypt-scans every user with a non-null value in
// hashColumn, matching token against the hash and rejecting anything older
// than ttl. Same linear-scan approach as VerifyBookmarkToken (bcrypt hashes
// can't be looked up by index) with an added expiry check.
func (s *Store) verifyTimedToken(token, hashColumn, createdAtColumn string, ttl time.Duration) (string, bool) {
	if token == "" {
		return "", false
	}
	query := fmt.Sprintf(`SELECT username, %s, %s FROM users WHERE %s IS NOT NULL`, hashColumn, createdAtColumn, hashColumn)
	rows, err := s.sql.Query(query)
	if err != nil {
		return "", false
	}
	defer rows.Close()

	cutoff := time.Now().Add(-ttl).Unix()
	for rows.Next() {
		var username, hash string
		var createdAt int64
		if err := rows.Scan(&username, &hash, &createdAt); err != nil {
			continue
		}
		if createdAt < cutoff {
			continue
		}
		if bcrypt.CompareHashAndPassword([]byte(hash), []byte(token)) == nil {
			return username, true
		}
	}
	return "", false
}

// checkEmailAvailable errors if email is already used by a user other than
// excludeID (0 for "no exclusion", i.e. a new user). Enforced here rather
// than with a SQL UNIQUE constraint so multiple users can share a NULL/
// empty email (everyone created before this feature existed).
func (s *Store) checkEmailAvailable(email string, excludeID int64) error {
	if email == "" {
		return nil
	}
	var existingID int64
	err := s.sql.QueryRow(`SELECT id FROM users WHERE email = ? AND id != ?`, email, excludeID).Scan(&existingID)
	if err == sql.ErrNoRows {
		return nil
	}
	if err != nil {
		return err
	}
	return fmt.Errorf("email %q is already in use", email)
}

// newToken generates a random URL-safe token and its bcrypt hash, the same
// generate-random/hash-at-rest shape used for bookmark, reset, and invite
// tokens alike.
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

// GetSMTPSettings returns the currently saved SMTP settings, or a disabled
// zero-value Settings if none have been saved yet.
func (s *Store) GetSMTPSettings() (mail.Settings, error) {
	var m mail.Settings
	err := s.sql.QueryRow(`SELECT host, port, encryption, username, password, from_name, from_addr FROM smtp_settings WHERE id = 1`).
		Scan(&m.Host, &m.Port, &m.Encryption, &m.Username, &m.Password, &m.FromName, &m.FromAddress)
	if err == sql.ErrNoRows {
		return mail.Settings{}, nil
	}
	if err != nil {
		return mail.Settings{}, err
	}
	return m, nil
}

// SaveSMTPSettings upserts the single SMTP settings row.
func (s *Store) SaveSMTPSettings(m mail.Settings) error {
	_, err := s.sql.Exec(`
		INSERT INTO smtp_settings (id, host, port, encryption, username, password, from_name, from_addr)
		VALUES (1, ?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT(id) DO UPDATE SET
			host = excluded.host, port = excluded.port, encryption = excluded.encryption,
			username = excluded.username, password = excluded.password,
			from_name = excluded.from_name, from_addr = excluded.from_addr`,
		m.Host, m.Port, m.Encryption, m.Username, m.Password, m.FromName, m.FromAddress,
	)
	return err
}

func boolToInt(b bool) int {
	if b {
		return 1
	}
	return 0
}

func nullIfEmpty(s string) any {
	if s == "" {
		return nil
	}
	return s
}
