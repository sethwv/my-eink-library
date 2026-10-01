package main

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/sethwv/my-eink-library/internal/config"
	"github.com/sethwv/my-eink-library/internal/users"
)

func sessionSecret(store *users.Store, override string) (string, error) {
	if override != "" {
		return override, nil
	}
	secret, _, err := store.SessionSecret()
	return secret, err
}

// runAdmin executes local break-glass user administration commands.
func runAdmin(args []string) error {
	if len(args) == 0 {
		return fmt.Errorf("usage: eink-library admin <create-user|reset-password>")
	}
	dataDir := config.DataDir()
	if err := os.MkdirAll(dataDir, 0o755); err != nil {
		return fmt.Errorf("create data directory: %w", err)
	}
	store, err := users.Open(filepath.Join(dataDir, "users.db"))
	if err != nil {
		return fmt.Errorf("open users store: %w", err)
	}
	defer store.Close()

	switch args[0] {
	case "create-user":
		if len(args) < 3 || len(args) > 4 {
			return fmt.Errorf("usage: eink-library admin create-user <username> <password> [role]")
		}
		role := users.RoleMember
		if len(args) == 4 {
			role = args[3]
		}
		if err := store.Create(args[1], args[2], role, true, ""); err != nil {
			return err
		}
		return nil
	case "reset-password":
		if len(args) != 3 {
			return fmt.Errorf("usage: eink-library admin reset-password <username> <password>")
		}
		user, err := store.UserByUsername(args[1])
		if err != nil {
			return err
		}
		if user == nil {
			return fmt.Errorf("user %q does not exist", args[1])
		}
		return store.ResetPassword(user.ID, args[2])
	default:
		return fmt.Errorf("usage: eink-library admin <create-user|reset-password>")
	}
}
