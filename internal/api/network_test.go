package api

import (
	"net/http"
	"path/filepath"
	"strings"
	"testing"

	"github.com/eleost04/miyohub/internal/model"
	"github.com/eleost04/miyohub/internal/store"
)

func TestProxySettingsAreAdminOnlyAndNeverExposeCredentials(t *testing.T) {
	s, err := store.New(filepath.Join(t.TempDir(), "state.json"))
	if err != nil {
		t.Fatal(err)
	}
	_, adminToken, _ := s.CreateAdmin("admin", "fixture-password")
	user, _ := s.CreateUser("user", "fixture-password", "user")
	_, token, _ := s.Authenticate("user", "fixture-password")
	server := NewServer(s)
	defer server.Stop()
	h := server.Handler()
	proxy := model.ProxyConfig{Enabled: true, URL: "socks5://proxy.example:1080", Username: "private-proxy-user", Password: "private-proxy-password"}
	body := jsonBody(t, map[string]any{"network": model.NetworkConfig{Proxy: proxy}})
	if response := callAPI(h, http.MethodPut, "/api/v1/config", token, body); response.Code != 403 || s.NetworkConfig().Proxy.Enabled {
		t.Fatal("ordinary user changed site proxy")
	}
	response := callAPI(h, http.MethodPut, "/api/v1/config", adminToken, body)
	if response.Code != 200 || strings.Contains(response.Body.String(), proxy.Password) || !strings.Contains(response.Body.String(), `"has_password":true`) {
		t.Fatal("admin proxy save failed or exposed password", response.Code)
	}
	for _, path := range []string{"/api/v1/config", "/api/v1/bootstrap", "/api/v1/status"} {
		response = callAPI(h, http.MethodGet, path, token, "")
		if response.Code != 200 {
			t.Fatal("public config failed", path, response.Code)
		}
		for _, secret := range []string{proxy.URL, proxy.Username, proxy.Password} {
			if strings.Contains(response.Body.String(), secret) {
				t.Fatal("site proxy details leaked to another user", path)
			}
		}
	}
	_ = s.AddLogForUser(user.ID, "network", "fixture "+proxy.URL+" "+proxy.Username+" "+proxy.Password)
	for _, entry := range s.RedactedLogsForUser(user.ID, false) {
		for _, secret := range []string{proxy.URL, proxy.Username, proxy.Password} {
			if strings.Contains(entry.Message, secret) {
				t.Fatal("proxy details leaked into logs")
			}
		}
	}
}
