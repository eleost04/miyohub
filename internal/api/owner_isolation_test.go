package api

import (
	"net/http"
	"path/filepath"
	"strings"
	"testing"

	"github.com/eleost04/miyohub/internal/model"
	"github.com/eleost04/miyohub/internal/store"
)

func TestAdministratorHasManagementPowersButNoOtherUsersAccountData(t *testing.T) {
	s, err := store.New(filepath.Join(t.TempDir(), "state.json"))
	if err != nil {
		t.Fatal(err)
	}
	admin, token, _ := s.CreateAdmin("admin", "test-password")
	member, _ := s.CreateUser("member", "test-password", "user")
	_ = s.AddAccountForUser(admin.ID, model.Account{Name: "own-account", Stuid: "101"})
	_ = s.AddAccountForUser(member.ID, model.Account{Name: "OTHER-PRIVATE-ACCOUNT", Stuid: "202"})
	other := s.AccountsForUser(member.ID, false)[0]
	_ = s.UpdateUserPermissions(admin, member.ID, model.UserPermissions{Exchange: true})
	p, err := s.CreateExchangePlan(member.ID, false, model.ExchangePlan{AccountID: other.ID, GoodsID: "good", GoodsName: "OTHER-PRIVATE-GOODS", Enabled: true})
	if err != nil {
		t.Fatal(err)
	}
	_ = s.AddLogForUser(member.ID, "task", "OTHER-PRIVATE-LOG")
	server := NewServer(s)
	defer server.Stop()
	h := server.Handler()
	for _, path := range []string{"/api/v1/bootstrap", "/api/v1/config", "/api/v1/accounts", "/api/v1/status", "/api/v1/shop/plans", "/api/v1/shop/status"} {
		w := callAPI(h, "GET", path, token, "")
		if w.Code != 200 || strings.Contains(w.Body.String(), "OTHER-PRIVATE") || strings.Contains(w.Body.String(), other.ID) {
			t.Fatal("admin account data leaked", path, w.Code)
		}
	}
	for _, input := range []struct{ method, path, body string }{
		{"PUT", "/api/v1/accounts", `{"id":"` + other.ID + `","name":"stolen"}`},
		{"DELETE", "/api/v1/accounts?id=" + other.ID, ""},
		{"POST", "/api/v1/run", `{"account_ids":["` + other.ID + `"]}`},
		{"POST", "/api/v1/run/cancel", `{"account_ids":["` + other.ID + `"]}`},
		{"POST", "/api/v1/accounts/check", `{"id":"` + other.ID + `"}`},
		{"GET", "/api/v1/shop/points?account_id=" + other.ID, ""},
		{"GET", "/api/v1/shop/addresses?account_id=" + other.ID, ""},
		{"POST", "/api/v1/shop/plans/run", `{"id":"` + p.ID + `"}`},
		{"POST", "/api/v1/shop/plans/cancel", `{"id":"` + p.ID + `"}`},
		{"DELETE", "/api/v1/shop/plans?id=" + p.ID, ""},
	} {
		if w := callAPI(h, input.method, input.path, token, input.body); w.Code < 400 {
			t.Fatal("admin crossed account boundary", input.path)
		}
	}
	settings := *other.TaskSettings
	if _, err := s.UpdateAccountTasks(admin, other.ID, settings); err == nil {
		t.Fatal("admin changed another user's task selection")
	}
	if w := callAPI(h, http.MethodGet, "/api/v1/admin/users", token, ""); w.Code != 200 {
		t.Fatal("user management was disabled")
	}
}
