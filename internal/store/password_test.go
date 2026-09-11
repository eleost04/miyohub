package store

import (
	"encoding/hex"
	"path/filepath"
	"strings"
	"testing"
)

func TestLegacyPasswordMigratesAndSessionsPersist(t *testing.T) {
	path := filepath.Join(t.TempDir(), "state.json")
	s, _ := New(path)
	u, _, _ := s.CreateAdmin("admin", "password123")
	salt := strings.Repeat("01", 16)
	sum := derivePassword(salt, "password123")
	s.data.Users[0].PasswordHash = salt + "$" + hex.EncodeToString(sum[:])
	_ = s.saveLocked()
	_, token, err := s.Authenticate("admin", "password123")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(s.data.Users[0].PasswordHash, "pbkdf2-sha256$") {
		t.Fatal("legacy hash not migrated")
	}
	reloaded, err := New(path)
	if err != nil {
		t.Fatal(err)
	}
	if got, ok := reloaded.UserBySession(token); !ok || got.ID != u.ID {
		t.Fatal("login session not persisted")
	}
	if _, _, err := s.ChangePassword(u.ID, "wrong", "newpassword123"); err == nil {
		t.Fatal("wrong old password accepted")
	}
	_, newToken, err := s.ChangePassword(u.ID, "password123", "newpassword123")
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := s.UserBySession(token); ok {
		t.Fatal("old session survived password change")
	}
	if _, ok := s.UserBySession(newToken); !ok {
		t.Fatal("new session missing")
	}
	if _, _, err := s.Authenticate("admin", "password123"); err == nil {
		t.Fatal("old password survived")
	}
}
