package api

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"

	"github.com/eleost04/miyohub/internal/model"
	"github.com/eleost04/miyohub/internal/store"
)

func TestAuthAndProtectedConfig(t *testing.T) {
	s, err := store.New(filepath.Join(t.TempDir(), "state.json"))
	if err != nil {
		t.Fatal(err)
	}
	handler := NewServer(s).Handler()
	setup := httptest.NewRecorder()
	setupReq := httptest.NewRequest(http.MethodPost, "/api/v1/auth/setup", strings.NewReader("{\"username\":\"admin\",\"password\":\"password123\"}"))
	secureTestRequest(setupReq)
	handler.ServeHTTP(setup, setupReq)
	if setup.Code != http.StatusOK {
		t.Fatalf("setup status=%d body=%s", setup.Code, setup.Body.String())
	}
	cookies := setup.Result().Cookies()
	if len(cookies) == 0 {
		t.Fatal("missing session cookie")
	}
	request := httptest.NewRequest(http.MethodGet, "/api/v1/config", nil)
	request.AddCookie(cookies[0])
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	if response.Code != http.StatusOK {
		t.Fatalf("config status=%d", response.Code)
	}
	var body map[string]any
	if err := json.NewDecoder(response.Body).Decode(&body); err != nil {
		t.Fatal(err)
	}
	if body["ok"] != true {
		t.Fatalf("body=%#v", body)
	}
}

func TestHealthReportsBuildVersion(t *testing.T) {
	s, err := store.New(filepath.Join(t.TempDir(), "state.json"))
	if err != nil {
		t.Fatal(err)
	}
	server := NewServerWithOptions(s, Options{Version: "test-build"})
	defer server.Stop()
	response := httptest.NewRecorder()
	server.Handler().ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/api/v1/health", nil))
	var body struct {
		Version string `json:"version"`
	}
	if err := json.Unmarshal(response.Body.Bytes(), &body); err != nil || response.Code != 200 || body.Version != "test-build" {
		t.Fatal("health version mismatch", response.Code, err)
	}
}

func TestPublicConfigRedactsWithoutMutatingStore(t *testing.T) {
	s, err := store.New(filepath.Join(t.TempDir(), "state.json"))
	if err != nil {
		t.Fatal(err)
	}
	if err := s.AddAccount(model.Account{Name: "main", Cookie: "secret-cookie", Stuid: "1001", Stoken: "secret-stoken", Mid: "mid"}); err != nil {
		t.Fatal(err)
	}

	redacted := publicConfig(s.Config())
	if redacted.Accounts[0].Cookie != "" || redacted.Accounts[0].Stoken != "" || redacted.Accounts[0].Mid != "" {
		t.Fatalf("credentials should be redacted: %#v", redacted.Accounts[0])
	}
	stored := s.Config().Accounts[0]
	if stored.Cookie != "secret-cookie" || stored.Stoken != "secret-stoken" || stored.Mid != "mid" {
		t.Fatalf("redacting public config mutated store: %#v", stored)
	}
}

func TestSettingsCannotOverwriteAccountsOrPlans(t *testing.T) {
	s, err := store.New(filepath.Join(t.TempDir(), "state.json"))
	if err != nil {
		t.Fatal(err)
	}
	admin, token, err := s.CreateAdmin("admin", "password123")
	if err != nil {
		t.Fatal(err)
	}
	if err := s.AddAccountForUser(admin.ID, model.Account{ID: "owned", Name: "owned", Cookie: "private-cookie"}); err != nil {
		t.Fatal(err)
	}
	handler := NewServer(s).Handler()
	req := httptest.NewRequest(http.MethodPut, "/api/v1/config", strings.NewReader(`{"accounts":[],"shop_exchange":{"enable":true,"retry_seconds":10,"retry_interval":0.5,"plans":[]},"features":{"bbs_tasks":true}}`))
	req.AddCookie(&http.Cookie{Name: "miyohub_session", Value: token})
	res := httptest.NewRecorder()
	secureTestRequest(req)
	handler.ServeHTTP(res, req)
	if res.Code != 200 {
		t.Fatal(res.Body.String())
	}
	cfg := s.Config()
	if len(cfg.Accounts) != 1 || cfg.Accounts[0].Cookie != "private-cookie" || cfg.Shop.RetrySeconds != 10 || cfg.Accounts[0].TaskSettings.Features.BBSTasks {
		t.Fatal("site settings erased an account, changed its tasks, or failed to save site parameters")
	}
	cfg.Accounts[0].Cookie = "changed"
	if s.Config().Accounts[0].Cookie != "private-cookie" {
		t.Fatal("config snapshot aliases store")
	}
}
