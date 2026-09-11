package store

import (
	"path/filepath"
	"testing"

	"github.com/eleost04/miyohub/internal/model"
)

func TestBBSRetrySettingsValidationAndPersistence(t *testing.T) {
	s, err := New(filepath.Join(t.TempDir(), "state.json"))
	if err != nil {
		t.Fatal(err)
	}
	if s.Config().Network.StateRetries() != 5 {
		t.Fatal("missing settings must default to five retries")
	}
	for _, retries := range []int{0, 10, -1, 11} {
		err := s.UpdateSettings(SettingsPatch{Network: &model.NetworkConfig{BBSStateRetries: &retries}})
		if (err != nil) != (retries < 0 || retries > 10) {
			t.Fatal("invalid retry limit validation", retries, err)
		}
	}
	reopened, err := New(s.path)
	if err != nil || reopened.Config().Network.StateRetries() != 10 {
		t.Fatal("valid retry settings lost after restart", err)
	}
}
