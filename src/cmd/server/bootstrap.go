package main

import (
	"fmt"
	"log"
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

func bootstrapAdministrator(store *users.Store) error {
	username, password, created, err := store.BootstrapPrimaryAdmin()
	if err != nil {
		return err
	}
	if created {
		log.Printf("generated administrator password for %s: %s", username, password)
	}
	return nil
}

func runAdmin(args []string) error {
	if len(args) != 1 || args[0] != "reset-password" {
		return fmt.Errorf("usage: eink-library admin reset-password")
	}
	store, err := users.Open(filepath.Join(config.DataDir(), "users.db"))
	if err != nil {
		return fmt.Errorf("open users store: %w", err)
	}
	defer store.Close()

	username, password, err := store.ResetPrimaryAdminPassword()
	if err != nil {
		return err
	}
	fmt.Printf("administrator password reset for %s: %s\n", username, password)
	return nil
}
