package api

import (
	"encoding/json"
	"errors"
	"net/http"
	"path/filepath"
	"strings"
	"testing"

	"github.com/eleost04/miyohub/internal/model"
	"github.com/eleost04/miyohub/internal/store"
)

func jsonBody(t *testing.T, value any) string {
	t.Helper()
	raw, err := json.Marshal(value)
	if err != nil {
		t.Fatal(err)
	}
	return string(raw)
}

type noEffectTransport struct{ calls *int }

func (n noEffectTransport) RoundTrip(*http.Request) (*http.Response, error) {
	*n.calls++
	return nil, errors.New("external effects are forbidden in this test")
}

func TestPersonalAPIsEnforceOwnershipAndSeparateServiceGrants(t *testing.T) {
	s, err := store.New(filepath.Join(t.TempDir(), "state.json"))
	if err != nil {
		t.Fatal(err)
	}
	admin, adminToken, _ := s.CreateAdmin("admin", "test-password")
	alice, _ := s.CreateUser("alice", "test-password", "user")
	bob, _ := s.CreateUser("bob", "test-password", "user")
	_, aliceToken, _ := s.Authenticate("alice", "test-password")
	_, bobToken, _ := s.Authenticate("bob", "test-password")
	for _, u := range []model.User{alice, bob} {
		if err := s.AddAccountForUser(u.ID, model.Account{Name: "account", Cookie: "test-only"}); err != nil {
			t.Fatal(err)
		}
	}
	a, b := s.AccountsForUser(alice.ID, false)[0], s.AccountsForUser(bob.ID, false)[0]
	server := NewServer(s)
	defer server.Stop()
	calls := 0
	server.shopClient.HTTP.Transport = noEffectTransport{calls: &calls}
	h := server.Handler()
	for _, path := range []string{"/api/v1/accounts/tasks", "/api/v1/captcha/config", "/api/v1/admin/users/permissions"} {
		if res := callAPI(h, "PUT", path, "", `{}`); res.Code != 401 {
			t.Fatal("unauthenticated personal config", path, res.Code)
		}
	}
	p := *a.TaskSettings
	p.Automatic = false
	p.Games.Enabled = []string{"zzz"}
	if res := callAPI(h, "PUT", "/api/v1/accounts/tasks", aliceToken, jsonBody(t, map[string]any{"id": b.ID, "task_settings": p})); res.Code != 403 {
		t.Fatal("cross-user task configuration", res.Code)
	}
	if res := callAPI(h, "PUT", "/api/v1/accounts/tasks", aliceToken, jsonBody(t, map[string]any{"id": a.ID, "task_settings": p})); res.Code != 200 {
		t.Fatal("own task choices rejected", res.Code)
	}
	if s.AccountsForUser(bob.ID, false)[0].TaskSettings.Automatic != true {
		t.Fatal("own choice changed a foreign account")
	}
	personal := model.UserCaptchaConfig{Source: "personal", Revision: 1, MaxRetries: 2, Channels: []model.CaptchaChannel{{ID: "own", Provider: "custom", Enabled: true, Endpoint: "https://solver.example/pass_nine", Token: "ALICE_TEST_ONLY", Timeout: 60}}}
	res := callAPI(h, "PUT", "/api/v1/captcha/config", aliceToken, jsonBody(t, personal))
	if res.Code != 200 || strings.Contains(res.Body.String(), "ALICE_TEST_ONLY") {
		t.Fatal("own provider rejected or its token leaked", res.Code)
	}
	res = callAPI(h, "GET", "/api/v1/captcha/config?user_id="+alice.ID, bobToken, "")
	if res.Code != 200 || strings.Contains(res.Body.String(), "solver.example") {
		t.Fatal("query parameter accessed another user's provider")
	}
	personal.Source, personal.Revision = "site", 2
	if res := callAPI(h, "PUT", "/api/v1/captcha/config", aliceToken, jsonBody(t, personal)); res.Code != 403 {
		t.Fatal("ungranted site captcha accepted", res.Code)
	}
	grant := map[string]any{"user_id": alice.ID, "permissions": model.UserPermissions{SiteCaptcha: true}}
	if res := callAPI(h, "PUT", "/api/v1/admin/users/permissions", aliceToken, jsonBody(t, grant)); res.Code != 403 {
		t.Fatal("user self-granted services")
	}
	if res := callAPI(h, "PUT", "/api/v1/admin/users/permissions", adminToken, jsonBody(t, grant)); res.Code != 200 {
		t.Fatal("admin could not grant site captcha", res.Code)
	}
	if res := callAPI(h, "PUT", "/api/v1/captcha/config", aliceToken, jsonBody(t, personal)); res.Code != 200 {
		t.Fatal("authorized site mode rejected", res.Code)
	}
	for _, path := range []string{"/api/v1/shop/plans", "/api/v1/shop/plans/run", "/api/v1/shop/exchange"} {
		if res := callAPI(h, "POST", path, aliceToken, jsonBody(t, map[string]string{"account_id": a.ID, "goods_id": "test"})); res.Code != 403 {
			t.Fatal("site captcha permission enabled exchanges", path, res.Code)
		}
	}
	if res := callAPI(h, "PUT", "/api/v1/admin/users", aliceToken, jsonBody(t, map[string]any{"user_id": alice.ID, "role": "admin", "status": "active"})); res.Code != 403 {
		t.Fatal("management endpoint accepted self escalation")
	}
	if res := callAPI(h, "PUT", "/api/v1/admin/users", adminToken, jsonBody(t, map[string]any{"user_id": alice.ID, "role": "user", "status": "active", "permissions": model.UserPermissions{Exchange: true}})); res.Code != 200 {
		t.Fatal("atomic user permission dialog rejected", res.Code)
	}
	res = callAPI(h, "GET", "/api/v1/auth/me", aliceToken, "")
	var me struct {
		Data model.User `json:"data"`
	}
	if err := json.Unmarshal(res.Body.Bytes(), &me); err != nil || !me.Data.CanExchange() || me.Data.CanUseSiteCaptcha() {
		t.Fatal("permissions were stale in current session")
	}
	if server.runner.Running() || calls != 0 {
		t.Fatal("saving preferences triggered external account actions")
	}
	_ = admin
}
