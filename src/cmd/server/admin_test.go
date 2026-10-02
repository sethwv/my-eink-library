package main

import (
	"path/filepath"
	"testing"

	"github.com/sethwv/my-sideload-library/internal/users"
)

func TestSessionSecret_OverrideDoesNotReplacePersistedValue(t *testing.T) {
	store, err := users.Open(filepath.Join(t.TempDir(), "users.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { store.Close() })
	persisted, _, err := store.SessionSecret()
	if err != nil {
		t.Fatal(err)
	}
	secret, err := sessionSecret(store, "environment-override")
	if err != nil {
		t.Fatal(err)
	}
	if secret != "environment-override" {
		t.Errorf("sessionSecret() = %q", secret)
	}
	stored, _, err := store.SessionSecret()
	if err != nil {
		t.Fatal(err)
	}
	if stored != persisted {
		t.Error("environment override replaced the persisted secret")
	}
}

func TestRunAdmin_CreateUserAndResetPassword(t *testing.T) {
	dataDir := t.TempDir()
	t.Setenv("DATA_DIR", dataDir)
	if err := runAdmin([]string{"create-user", "fixture", "password", users.RoleAdmin}); err != nil {
		t.Fatal(err)
	}
	store, err := users.Open(filepath.Join(dataDir, "users.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { store.Close() })
	if !store.CheckPassword("fixture", "password") {
		t.Error("create-user command did not create the fixture user")
	}
	if err := runAdmin([]string{"reset-password", "fixture", "new-password"}); err != nil {
		t.Fatal(err)
	}
	if store.CheckPassword("fixture", "password") || !store.CheckPassword("fixture", "new-password") {
		t.Error("reset-password command did not set the supplied password")
	}
}

func TestRunAdmin_ResetPasswordRequiresPassword(t *testing.T) {
	t.Setenv("DATA_DIR", t.TempDir())
	if err := runAdmin([]string{"reset-password", "fixture"}); err == nil {
		t.Error("expected reset-password without a password to fail")
	}
}
