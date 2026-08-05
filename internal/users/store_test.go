package users

import (
	"path/filepath"
	"testing"
)

func openTestStore(t *testing.T) *Store {
	t.Helper()
	s, err := Open(filepath.Join(t.TempDir(), "users.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { s.Close() })
	return s
}

func TestBootstrap_CreatesFirstAdmin(t *testing.T) {
	s := openTestStore(t)

	if err := s.Bootstrap("admin", "hunter2"); err != nil {
		t.Fatal(err)
	}

	if !s.CheckPassword("admin", "hunter2") {
		t.Error("expected bootstrap admin to be able to log in")
	}
	if !s.IsAdmin("admin") {
		t.Error("expected bootstrap user to be admin")
	}
}

func TestBootstrap_NoOpIfUsersExist(t *testing.T) {
	s := openTestStore(t)

	if err := s.Bootstrap("admin", "hunter2"); err != nil {
		t.Fatal(err)
	}
	if err := s.Bootstrap("someone-else", "different"); err != nil {
		t.Fatal(err)
	}

	if s.CheckPassword("someone-else", "different") {
		t.Error("expected second bootstrap call to be a no-op")
	}
	if !s.CheckPassword("admin", "hunter2") {
		t.Error("expected original admin to still work")
	}
}

func TestCreateAndCheckPassword(t *testing.T) {
	s := openTestStore(t)

	if err := s.Create("bob", "s3cret", false); err != nil {
		t.Fatal(err)
	}

	if !s.CheckPassword("bob", "s3cret") {
		t.Error("expected correct credentials to pass")
	}
	if s.CheckPassword("bob", "wrong") {
		t.Error("expected wrong password to fail")
	}
	if s.CheckPassword("nobody", "s3cret") {
		t.Error("expected unknown username to fail")
	}
	if s.IsAdmin("bob") {
		t.Error("expected non-admin user to not be admin")
	}
}

func TestDelete_RefusesLastAdmin(t *testing.T) {
	s := openTestStore(t)
	if err := s.Create("admin", "hunter2", true); err != nil {
		t.Fatal(err)
	}

	users, err := s.List()
	if err != nil {
		t.Fatal(err)
	}
	if len(users) != 1 {
		t.Fatalf("expected 1 user, got %d", len(users))
	}

	if err := s.Delete(users[0].ID); err == nil {
		t.Error("expected deleting the last admin to fail")
	}
}

func TestDelete_AllowsNonLastAdmin(t *testing.T) {
	s := openTestStore(t)
	if err := s.Create("admin1", "hunter2", true); err != nil {
		t.Fatal(err)
	}
	if err := s.Create("admin2", "hunter2", true); err != nil {
		t.Fatal(err)
	}

	users, err := s.List()
	if err != nil {
		t.Fatal(err)
	}
	if len(users) != 2 {
		t.Fatalf("expected 2 users, got %d", len(users))
	}

	if err := s.Delete(users[0].ID); err != nil {
		t.Errorf("expected deleting one of two admins to succeed, got %v", err)
	}
}

func TestDelete_AllowsNonAdmin(t *testing.T) {
	s := openTestStore(t)
	if err := s.Create("admin", "hunter2", true); err != nil {
		t.Fatal(err)
	}
	if err := s.Create("regular", "hunter2", false); err != nil {
		t.Fatal(err)
	}

	users, err := s.List()
	if err != nil {
		t.Fatal(err)
	}
	var regularID int64
	for _, u := range users {
		if u.Username == "regular" {
			regularID = u.ID
		}
	}
	if err := s.Delete(regularID); err != nil {
		t.Errorf("expected deleting a non-admin to succeed, got %v", err)
	}
	if s.CheckPassword("regular", "hunter2") {
		t.Error("expected deleted user to no longer be able to log in")
	}
}

func TestResetPassword(t *testing.T) {
	s := openTestStore(t)
	if err := s.Create("bob", "old-pass", false); err != nil {
		t.Fatal(err)
	}

	users, _ := s.List()
	if err := s.ResetPassword(users[0].ID, "new-pass"); err != nil {
		t.Fatal(err)
	}

	if s.CheckPassword("bob", "old-pass") {
		t.Error("expected old password to no longer work")
	}
	if !s.CheckPassword("bob", "new-pass") {
		t.Error("expected new password to work")
	}
}

func TestBookmarkToken_GenerateVerifyRevoke(t *testing.T) {
	s := openTestStore(t)
	if err := s.Create("bob", "s3cret", false); err != nil {
		t.Fatal(err)
	}

	if s.HasBookmarkToken("bob") {
		t.Error("expected no bookmark token before one is generated")
	}

	token, err := s.GenerateBookmarkToken("bob")
	if err != nil {
		t.Fatal(err)
	}
	if token == "" {
		t.Fatal("expected a non-empty token")
	}
	if !s.HasBookmarkToken("bob") {
		t.Error("expected HasBookmarkToken to be true after generating one")
	}

	username, ok := s.VerifyBookmarkToken(token)
	if !ok || username != "bob" {
		t.Errorf("VerifyBookmarkToken(token) = %q, %v; want bob, true", username, ok)
	}

	if _, ok := s.VerifyBookmarkToken("not-the-right-token"); ok {
		t.Error("expected a wrong token to fail verification")
	}
	if _, ok := s.VerifyBookmarkToken(""); ok {
		t.Error("expected an empty token to fail verification")
	}

	if err := s.RevokeBookmarkToken("bob"); err != nil {
		t.Fatal(err)
	}
	if s.HasBookmarkToken("bob") {
		t.Error("expected no bookmark token after revoking")
	}
	if _, ok := s.VerifyBookmarkToken(token); ok {
		t.Error("expected the old token to stop working after revoking")
	}
}

func TestBookmarkToken_RegenerateInvalidatesPrevious(t *testing.T) {
	s := openTestStore(t)
	if err := s.Create("bob", "s3cret", false); err != nil {
		t.Fatal(err)
	}

	first, err := s.GenerateBookmarkToken("bob")
	if err != nil {
		t.Fatal(err)
	}
	second, err := s.GenerateBookmarkToken("bob")
	if err != nil {
		t.Fatal(err)
	}
	if first == second {
		t.Fatal("expected regenerating to produce a different token")
	}

	if _, ok := s.VerifyBookmarkToken(first); ok {
		t.Error("expected the first token to stop working once regenerated")
	}
	username, ok := s.VerifyBookmarkToken(second)
	if !ok || username != "bob" {
		t.Errorf("VerifyBookmarkToken(second) = %q, %v; want bob, true", username, ok)
	}
}
