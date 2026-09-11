package api

import (
	"encoding/json"
	"net/http"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/eleost04/miyohub/internal/model"
	"github.com/eleost04/miyohub/internal/store"
)

func TestBootstrapPublicMetadataAndRevokedSessions(t *testing.T) {
	state, err := store.New(filepath.Join(t.TempDir(), "state.json"))
	if err != nil {
		t.Fatal(err)
	}
	server := NewServer(state)
	defer server.Stop()
	h := server.Handler()
	checkAnonymous := func(token string, hasAdmin bool) {
		t.Helper()
		res := callAPI(h, http.MethodGet, "/api/v1/bootstrap", token, "")
		var body struct {
			Data struct {
				Auth   model.AuthStatus `json:"auth"`
				User   *model.User      `json:"user"`
				Config json.RawMessage  `json:"config"`
				Status json.RawMessage  `json:"status"`
			} `json:"data"`
		}
		if err := json.Unmarshal(res.Body.Bytes(), &body); err != nil || res.Code != 200 || res.Header().Get("Cache-Control") != "no-store" {
			t.Fatal("bootstrap response failed or can be cached", res.Code, err)
		}
		if body.Data.Auth.HasAdmin != hasAdmin || !body.Data.Auth.NeedAuth || body.Data.User != nil || len(body.Data.Config) != 0 || len(body.Data.Status) != 0 {
			t.Fatal("anonymous bootstrap leaked private workspace data")
		}
	}
	checkAnonymous("", false)
	_, _, _ = state.CreateAdmin("admin", "test-password")
	member, _ := state.CreateUser("member", "test-password", "user")
	_, token, _ := state.Authenticate("member", "test-password")
	checkAnonymous("", true)
	checkAnonymous("invalid-session", true)
	if err := state.UpdateUserStatus(member.ID, "disabled"); err != nil {
		t.Fatal(err)
	}
	checkAnonymous(token, true)
	if err := state.UpdateUserStatus(member.ID, "active"); err != nil {
		t.Fatal(err)
	}
	_, token, _ = state.Authenticate("member", "test-password")
	if err := state.DeleteSession(token); err != nil {
		t.Fatal(err)
	}
	checkAnonymous(token, true)
	if res := callAPI(h, http.MethodPost, "/api/v1/bootstrap", "", `{}`); res.Code != 405 {
		t.Fatal("bootstrap accepted a write")
	}
}

func TestBootstrapReusesPrivateVisibilityWithoutDuplicateCollections(t *testing.T) {
	state, err := store.New(filepath.Join(t.TempDir(), "state.json"))
	if err != nil {
		t.Fatal(err)
	}
	_, adminToken, _ := state.CreateAdmin("admin", "test-password")
	alice, _ := state.CreateUser("alice", "test-password", "user")
	bob, _ := state.CreateUser("bob", "test-password", "user")
	_, aliceToken, _ := state.Authenticate("alice", "test-password")
	for _, u := range []model.User{alice, bob} {
		if err := state.AddAccountForUser(u.ID, model.Account{Name: u.Username + " account", Cookie: "NEVER_RETURN_COOKIE", Stoken: "NEVER_RETURN_STOKEN"}); err != nil {
			t.Fatal(err)
		}
		if err := state.AddLogForUser(u.ID, "bbs", u.Username+" scoped log"); err != nil {
			t.Fatal(err)
		}
	}
	captcha := model.CaptchaConfig{MaxRetries: 2, Channels: []model.CaptchaChannel{{ID: "site", Provider: "custom", Enabled: true, Endpoint: "https://site-captcha.example/pass_nine", Token: "NEVER_RETURN_SOLVER_TOKEN", Timeout: 10}}}
	if err := state.UpdateSettings(store.SettingsPatch{Captcha: &captcha}); err != nil {
		t.Fatal(err)
	}
	server := NewServer(state)
	defer server.Stop()
	calls := 0
	server.shopClient.HTTP.Transport = noEffectTransport{calls: &calls}
	h := server.Handler()
	for _, token := range []string{adminToken, aliceToken} {
		res := callAPI(h, http.MethodGet, "/api/v1/bootstrap?user_id="+bob.ID, token, "")
		var body struct {
			Data struct {
				User   model.User                 `json:"user"`
				Config map[string]json.RawMessage `json:"config"`
				Status map[string]json.RawMessage `json:"status"`
			} `json:"data"`
		}
		if err := json.Unmarshal(res.Body.Bytes(), &body); err != nil || res.Code != 200 || strings.Contains(res.Body.String(), "NEVER_RETURN_") {
			t.Fatal("bootstrap failed or exposed credentials", res.Code, err)
		}
		var existing struct{ Data map[string]json.RawMessage }
		_ = json.Unmarshal(callAPI(h, http.MethodGet, "/api/v1/config", token, "").Body.Bytes(), &existing)
		if !reflect.DeepEqual(body.Data.Config, existing.Data) {
			t.Fatal("bootstrap bypassed existing visibility rules")
		}
		for _, key := range []string{"accounts", "exchange", "user"} {
			if _, exists := body.Data.Status[key]; exists {
				t.Fatal("bootstrap duplicated private collections", key)
			}
		}
		if token == aliceToken && (body.Data.User.ID != alice.ID || strings.Contains(res.Body.String(), "bob scoped log") || strings.Contains(res.Body.String(), "bob account") || strings.Contains(res.Body.String(), "site-captcha.example")) {
			t.Fatal("member bootstrap included another user's data")
		}
	}
	if calls != 0 || server.runner.Running() {
		t.Fatal("opening workspace triggered account operations")
	}
}

func TestPersonalScheduleStatusSkipsAccountsWithoutSelectedTasks(t *testing.T) {
	state, err := store.New(filepath.Join(t.TempDir(), "state.json"))
	if err != nil {
		t.Fatal(err)
	}
	admin, _, err := state.CreateAdmin("admin", "test-password")
	if err != nil {
		t.Fatal(err)
	}
	if err := state.AddAccountForUser(admin.ID, model.Account{Name: "empty schedule"}); err != nil {
		t.Fatal(err)
	}
	a := state.AccountsForUser(admin.ID, false)[0]
	p := *a.TaskSettings
	p.Schedule = &model.AccountSchedule{Time: "12:34", Timezone: "UTC"}
	p.Features = model.Features{}
	if _, err := state.UpdateAccountTasks(admin, a.ID, p); err != nil {
		t.Fatal(err)
	}
	server := NewServer(state)
	defer server.Stop()
	if got := server.personalScheduleStatus(admin); got.Enabled || got.NextRun != "" {
		t.Fatal("empty task selection was presented as a scheduled run", got)
	}
	a = state.AccountsForUser(admin.ID, false)[0]
	p = *a.TaskSettings
	p.Features.GameCheckin, p.Games.Enabled = true, []string{"genshin"}
	if _, err := state.UpdateAccountTasks(admin, a.ID, p); err != nil {
		t.Fatal(err)
	}
	if got := server.personalScheduleStatus(admin); !got.Enabled || got.NextRun == "" {
		t.Fatal("selected personal tasks disappeared from schedule", got)
	}
}
