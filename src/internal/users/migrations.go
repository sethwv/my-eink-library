package users

import (
	"database/sql"
	"embed"
	"fmt"

	"github.com/pressly/goose/v3"
	"github.com/sethwv/my-eink-library/internal/sqlite"
)

//go:embed migrations/*.sql
var migrationsFS embed.FS

func applyMigrations(db *sql.DB) error {
	goose.SetBaseFS(migrationsFS)
	if err := goose.SetDialect("sqlite3"); err != nil {
		return fmt.Errorf("set migration dialect: %w", err)
	}
	if err := goose.Up(db, "migrations"); err != nil {
		return fmt.Errorf("apply migrations: %w", err)
	}
	return nil
}

// migrateColumns keeps databases created before Goose's adoption compatible
// by adding every later users column. role and can_bookmark are backfilled
// only when role is first introduced.
func (s *Store) migrateColumns() error {
	existing, err := sqlite.Columns(s.sql, "users")
	if err != nil {
		return err
	}
	roleColumnIsNew := !existing["role"]
	if _, err := sqlite.EnsureColumns(s.sql, "users", []sqlite.Column{
		{Name: "bookmark_token_hash", DDL: "TEXT"},
		{Name: "bookmark_token_created_at", DDL: "INTEGER"},
		{Name: "role", DDL: "TEXT NOT NULL DEFAULT 'member'"},
		{Name: "can_bookmark", DDL: "INTEGER NOT NULL DEFAULT 1"},
		{Name: "email", DDL: "TEXT"},
		{Name: "reset_token_hash", DDL: "TEXT"},
		{Name: "reset_token_created_at", DDL: "INTEGER"},
		{Name: "invite_token_hash", DDL: "TEXT"},
		{Name: "invite_token_created_at", DDL: "INTEGER"},
		{Name: "digest_subscribed", DDL: "INTEGER NOT NULL DEFAULT 0"},
	}); err != nil {
		return err
	}
	if roleColumnIsNew {
		_, err := s.sql.Exec(`UPDATE users SET role = 'admin' WHERE is_admin = 1`)
		return err
	}
	return nil
}
