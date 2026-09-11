package store

import (
	"bytes"
	"encoding/json"
	"github.com/eleost04/miyohub/internal/model"
	"os"
	"path/filepath"
	"testing"
)

func TestPlaintextMigrationAndEncryptedRoundTrip(t *testing.T) {
	path := filepath.Join(t.TempDir(), "state.json")
	legacy := defaultState()
	legacy.Config.Accounts = []model.Account{{ID: "a", Name: "account", Cookie: "COOKIE-SECRET", Stoken: "TOKEN-SECRET", CloudTokens: map[string]string{"genshin": "CLOUD-SECRET"}}}
	raw, _ := json.Marshal(legacy)
	if err := os.WriteFile(path, raw, 0644); err != nil {
		t.Fatal(err)
	}
	s, err := New(path)
	if err != nil {
		t.Fatal(err)
	}
	disk, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	for _, secret := range []string{"COOKIE-SECRET", "TOKEN-SECRET", "CLOUD-SECRET"} {
		if bytes.Contains(disk, []byte(secret)) {
			t.Fatal("plaintext credential persisted")
		}
	}
	for _, file := range []string{path, path + ".key"} {
		info, err := os.Stat(file)
		if err != nil || info.Mode().Perm() != 0600 {
			t.Fatal("insecure file mode", file, err)
		}
	}
	reloaded, err := New(path)
	if err != nil {
		t.Fatal(err)
	}
	if reloaded.Config().Accounts[0].Cookie != s.Config().Accounts[0].Cookie {
		t.Fatal("encrypted round trip lost credential")
	}
	envelope := encryptedState{}
	_ = json.Unmarshal(disk, &envelope)
	envelope.Ciphertext[len(envelope.Ciphertext)-1] ^= 1
	corrupted, _ := json.Marshal(envelope)
	_ = os.WriteFile(path, corrupted, 0600)
	if _, err := New(path); err == nil {
		t.Fatal("tampering not detected")
	}
	after, _ := os.ReadFile(path)
	if !bytes.Equal(after, corrupted) {
		t.Fatal("corrupt state was overwritten")
	}
}
func TestMissingEncryptionKeyDoesNotOverwriteState(t *testing.T) {
	path := filepath.Join(t.TempDir(), "state.json")
	if _, err := New(path); err != nil {
		t.Fatal(err)
	}
	before, _ := os.ReadFile(path)
	if err := os.Rename(path+".key", path+".key.backup"); err != nil {
		t.Fatal(err)
	}
	if _, err := New(path); err == nil {
		t.Fatal("missing key accepted")
	}
	after, _ := os.ReadFile(path)
	if !bytes.Equal(before, after) {
		t.Fatal("state overwritten without original key")
	}
	if _, err := os.Stat(path + ".key"); !os.IsNotExist(err) {
		t.Fatal("replacement key generated")
	}
}
func TestFailedWriteRollsBackMemory(t *testing.T) {
	dir := t.TempDir()
	s, err := New(filepath.Join(dir, "state.json"))
	if err != nil {
		t.Fatal(err)
	}
	blocked := filepath.Join(dir, "directory")
	_ = os.Mkdir(blocked, 0700)
	s.path = blocked
	if err := s.SetRegistrationMode("open"); err == nil {
		t.Fatal("expected write failure")
	}
	if s.RegistrationMode() != "review" {
		t.Fatal("failed write changed live state")
	}
}
