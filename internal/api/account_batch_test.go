package api

import (
	"encoding/json"
	"net/http"
	"path/filepath"
	"strings"
	"testing"

	"github.com/eleost04/miyohub/internal/model"
	"github.com/eleost04/miyohub/internal/store"
)

func TestAccountBatchAPIPrivacy(t *testing.T) {
	s, err := store.New(filepath.Join(t.TempDir(), "state.json"))
	if err != nil {
		t.Fatal(err)
	}
	u, token, _ := s.CreateAdmin("admin", "test-password")
	v, _ := s.CreateUser("member", "test-password", "user")
	_ = s.AddAccountForUser(u.ID, model.Account{Name: "own", Cookie: "stuid=111;cookie_token=fixture-private-value"})
	_ = s.AddAccountForUser(v.ID, model.Account{Name: "other", Stuid: "222"})
	a := s.AccountsForUser(u.ID, false)[0]
	b := s.AccountsForUser(v.ID, false)[0]
	server := NewServer(s)
	defer server.Stop()
	group := "一组"
	input, _ := json.Marshal(store.AccountBatchPatch{AccountIDs: []string{a.ID}, Group: &group})
	if res := callAPI(server.Handler(), http.MethodPut, "/api/v1/accounts/batch", "", string(input)); res.Code != 401 {
		t.Fatal("anonymous batch accepted")
	}
	res := callAPI(server.Handler(), http.MethodPut, "/api/v1/accounts/batch", token, string(input))
	if res.Code != 200 || strings.Contains(res.Body.String(), "fixture-private-value") || !strings.Contains(res.Body.String(), group) {
		t.Fatal("public batch view unsafe", res.Code)
	}
	input, _ = json.Marshal(store.AccountBatchPatch{AccountIDs: []string{a.ID, b.ID}, Group: &group})
	if res := callAPI(server.Handler(), http.MethodPut, "/api/v1/accounts/batch", token, string(input)); res.Code < 400 || strings.Contains(res.Body.String(), b.ID) {
		t.Fatal("admin crossed ownership")
	}
	if server.runner.Running() {
		t.Fatal("editing groups started tasks")
	}
}
