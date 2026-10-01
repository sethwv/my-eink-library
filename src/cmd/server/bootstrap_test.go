package main

import (
	"path/filepath"
	"testing"

	"github.com/sethwv/my-eink-library/internal/users"
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
