// Package users manages login accounts in a small SQLite-backed store,
// separate from the book index so the two concerns stay decoupled.
package users

import (
	"database/sql"
	"fmt"

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
	Enabled          bool
	Email            string
	DigestSubscribed bool
	InvitePending    bool
	Permissions      []PermissionState
}

type Store struct {
	sql *sql.DB
}

// Open opens (creating if necessary) the users database at dbPath, applying
// its embedded schema migrations and the legacy additive column migration.
func Open(dbPath string) (*Store, error) {
	sdb, err := sql.Open("sqlite", dbPath+"?_pragma=busy_timeout(5000)&_pragma=journal_mode(WAL)")
	if err != nil {
		return nil, fmt.Errorf("open users db: %w", err)
	}
	sdb.SetMaxOpenConns(1)

	if err := applyMigrations(sdb); err != nil {
		sdb.Close()
		return nil, err
	}

	store := &Store{sql: sdb}
	if err := store.migrateColumns(); err != nil {
		sdb.Close()
		return nil, fmt.Errorf("migrate users schema: %w", err)
	}
	return store, nil
}

func (s *Store) Close() error {
	return s.sql.Close()
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
