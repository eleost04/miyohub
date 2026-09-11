package store

import (
	"os"
	"path/filepath"
	"strings"
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

func TestProxyPasswordsAreEncryptedPreservedAndExplicitlyCleared(t *testing.T) {
	s, err := New(filepath.Join(t.TempDir(), "state.json"))
	if err != nil {
		t.Fatal(err)
	}
	p := model.NetworkConfig{Proxy: model.ProxyConfig{Enabled: true, URL: "socks5://proxy.example:1080", Username: "fixture-user", Password: "fixture-only-secret"}}
	if err := s.UpdateSettings(SettingsPatch{Network: &p}); err != nil {
		t.Fatal(err)
	}
	raw, err := os.ReadFile(s.path)
	if err != nil || strings.Contains(string(raw), p.Proxy.Password) || strings.Contains(string(raw), p.Proxy.URL) {
		t.Fatal("proxy configuration stored in plaintext", err)
	}
	public := PublicNetwork(s.NetworkConfig())
	if public.Proxy.Password != "" || !public.Proxy.HasPassword || s.NetworkConfig().Proxy.Password != p.Proxy.Password {
		t.Fatal("public config leaked or erased proxy credentials")
	}
	if err := s.UpdateSettings(SettingsPatch{Network: &public}); err != nil || s.NetworkConfig().Proxy.Password != p.Proxy.Password {
		t.Fatal("redacted save erased proxy password", err)
	}
	public.Proxy.URL = "socks5://another.example:1080"
	if err := s.UpdateSettings(SettingsPatch{Network: &public}); err != nil || s.NetworkConfig().Proxy.Password != "" {
		t.Fatal("changed endpoint inherited old credentials", err)
	}
	if err := s.UpdateSettings(SettingsPatch{Network: &p}); err != nil {
		t.Fatal(err)
	}
	public = PublicNetwork(s.NetworkConfig())
	public.Proxy.ClearPassword = true
	if err := s.UpdateSettings(SettingsPatch{Network: &public}); err != nil || s.NetworkConfig().Proxy.Password != "" || s.NetworkConfig().Proxy.ClearPassword {
		t.Fatal("explicit password clearing was ignored", err)
	}
	reopened, err := New(s.path)
	if err != nil || reopened.NetworkConfig().Proxy.URL != p.Proxy.URL || reopened.NetworkConfig().Proxy.Password != "" {
		t.Fatal("proxy settings lost after restart", err)
	}
}
