package users

import (
	"database/sql"
	"fmt"
	"time"

	"golang.org/x/crypto/bcrypt"
)

const userColumns = `id, username, role, can_bookmark, email, digest_subscribed,
	invite_token_hash IS NOT NULL AND invite_token_hash != ''`

// Bootstrap creates the first user (as admin) from the given credentials if
// the users table is empty.
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

func (s *Store) IsAdmin(username string) bool {
	role, ok := s.role(username)
	return ok && role == RoleAdmin
}

func (s *Store) CanManageUsers(username string) bool {
	role, ok := s.role(username)
	return ok && (role == RoleAdmin || role == RoleUserManager)
}

func (s *Store) CanManageServer(username string) bool {
	role, ok := s.role(username)
	return ok && (role == RoleAdmin || role == RoleServerManager)
}

func (s *Store) CanUseBookmark(username string) bool {
	var canBookmark int
	err := s.sql.QueryRow(`SELECT can_bookmark FROM users WHERE username = ?`, username).Scan(&canBookmark)
	return err == nil && canBookmark != 0
}

func (s *Store) role(username string) (string, bool) {
	var role string
	err := s.sql.QueryRow(`SELECT role FROM users WHERE username = ?`, username).Scan(&role)
	return role, err == nil
}

// List returns all users ordered by username.
func (s *Store) List() ([]User, error) {
	rows, err := s.sql.Query(`SELECT ` + userColumns + ` FROM users ORDER BY username`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []User
	for rows.Next() {
		u, err := scanUser(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, u)
	}
	return out, rows.Err()
}

// UserByID returns one user, or nil when the id does not exist.
func (s *Store) UserByID(id int64) (*User, error) {
	return s.userBy(`id = ?`, id)
}

// UserByUsername returns one user, or nil when the username does not exist.
func (s *Store) UserByUsername(username string) (*User, error) {
	return s.userBy(`username = ?`, username)
}

func (s *Store) userBy(where string, arg any) (*User, error) {
	u, err := scanUser(s.sql.QueryRow(`SELECT `+userColumns+` FROM users WHERE `+where, arg))
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &u, nil
}

type userScanner interface {
	Scan(...any) error
}

func scanUser(row userScanner) (User, error) {
	var u User
	var canBookmark, digestSubscribed, invitePending int
	var email sql.NullString
	if err := row.Scan(&u.ID, &u.Username, &u.Role, &canBookmark, &email, &digestSubscribed, &invitePending); err != nil {
		return User{}, err
	}
	u.IsAdmin = u.Role == RoleAdmin
	u.CanManageUsers = u.Role == RoleAdmin || u.Role == RoleUserManager
	u.CanManageServer = u.Role == RoleAdmin || u.Role == RoleServerManager
	u.CanBookmark = canBookmark != 0
	u.Email = email.String
	u.DigestSubscribed = digestSubscribed != 0
	u.InvitePending = invitePending != 0
	return u, nil
}

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
	hash, err := passwordHash(password)
	if err != nil {
		return err
	}
	_, err = s.sql.Exec(`INSERT INTO users (username, password_hash, is_admin, role, can_bookmark, email, created_at) VALUES (?, ?, ?, ?, ?, ?, ?)`, username, hash, boolToInt(role == RoleAdmin), role, boolToInt(canBookmark), nullIfEmpty(email), time.Now().Unix())
	return err
}

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
	_, err := s.sql.Exec(`UPDATE users SET role = ?, is_admin = ?, can_bookmark = ? WHERE id = ?`, role, boolToInt(role == RoleAdmin), boolToInt(canBookmark), id)
	return err
}

func validRole(role string) bool {
	switch role {
	case RoleAdmin, RoleUserManager, RoleServerManager, RoleMember:
		return true
	}
	return false
}

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

func (s *Store) SetEmail(id int64, email string) error {
	if err := s.checkEmailAvailable(email, id); err != nil {
		return err
	}
	_, err := s.sql.Exec(`UPDATE users SET email = ? WHERE id = ?`, nullIfEmpty(email), id)
	return err
}

func (s *Store) ResetPassword(id int64, newPassword string) error {
	hash, err := passwordHash(newPassword)
	if err != nil {
		return err
	}
	_, err = s.sql.Exec(`UPDATE users SET password_hash = ? WHERE id = ?`, hash, id)
	return err
}

func passwordHash(password string) (string, error) {
	if password == "" {
		return "", fmt.Errorf("password is required")
	}
	hash, err := bcrypt.GenerateFromPassword([]byte(password), bcrypt.DefaultCost)
	if err != nil {
		return "", fmt.Errorf("hash password: %w", err)
	}
	return string(hash), nil
}

func (s *Store) SetDigestSubscribed(username string, subscribed bool) error {
	_, err := s.sql.Exec(`UPDATE users SET digest_subscribed = ? WHERE username = ?`, boolToInt(subscribed), username)
	return err
}

func (s *Store) IsDigestSubscribed(username string) bool {
	var subscribed int
	err := s.sql.QueryRow(`SELECT digest_subscribed FROM users WHERE username = ?`, username).Scan(&subscribed)
	return err == nil && subscribed != 0
}

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
