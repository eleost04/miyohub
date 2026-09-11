package api

import (
	"encoding/json"
	"net/http"
	"path/filepath"
	"testing"
	"time"

	"github.com/eleost04/miyohub/internal/model"
	"github.com/eleost04/miyohub/internal/shop"
	"github.com/eleost04/miyohub/internal/store"
)

func TestTaskAndLoginRoutesEnforceAccountOwnership(t *testing.T) {
	s, err := store.New(filepath.Join(t.TempDir(), "state.json"))
	if err != nil {
		t.Fatal(err)
	}
	admin, _, err := s.CreateAdmin("admin", "password123")
	if err != nil {
		t.Fatal(err)
	}
	member, err := s.CreateUser("member", "password123", "user")
	if err != nil {
		t.Fatal(err)
	}
	_, token, err := s.Authenticate("member", "password123")
	if err != nil {
		t.Fatal(err)
	}
	for _, user := range []model.User{admin, member} {
		if err := s.AddAccountForUser(user.ID, model.Account{Name: "test", Cookie: "test-cookie"}); err != nil {
			t.Fatal(err)
		}
	}
	foreign, own := s.AccountsForUser(admin.ID, false)[0], s.AccountsForUser(member.ID, false)[0]
	server := NewServer(s)
	defer server.Stop()
	h := server.Handler()
	for _, tc := range []struct{ path, body string }{
		{"/api/v1/run", `{"account_ids":["` + foreign.ID + `"]}`},
		{"/api/v1/run/cancel", `{"account_ids":["` + own.ID + `","` + foreign.ID + `"]}`},
		{"/api/v1/login/qr/start", `{"account_id":"` + foreign.ID + `","account_name":"test"}`},
		{"/api/v1/login/sms/send", `{"account_id":"` + foreign.ID + `","account_name":"test","phone":"13800138000"}`},
	} {
		w := callAPI(h, http.MethodPost, tc.path, token, tc.body)
		if w.Code < 400 {
			t.Fatal("foreign account action accepted", tc.path, w.Body.String())
		}
	}
	for _, path := range []string{"/api/v1/run", "/api/v1/run/cancel"} {
		if w := callAPI(h, http.MethodPost, path, token, `{"account_ids":[]}`); w.Code != 400 {
			t.Fatal("empty selection should not mean all accounts", path, w.Code)
		}
	}
	if w := callAPI(h, http.MethodPost, "/api/v1/run/cancel", token, `{}`); w.Code != 200 {
		t.Fatal("cancelling own accounts failed", w.Body.String())
	}
	if server.runner.Running() || server.qr.State(member.ID).Running {
		t.Fatal("rejected request started external work")
	}
	for _, path := range []string{"/api/v1/run/cancel", "/api/v1/login/sms/send", "/api/v1/login/sms/verify", "/api/v1/login/sms/cancel"} {
		if w := callAPI(h, http.MethodPost, path, "", `{}`); w.Code != http.StatusUnauthorized {
			t.Fatal("unauthenticated task route", path, w.Code)
		}
		if w := callAPI(h, http.MethodGet, path, token, ""); w.Code != http.StatusMethodNotAllowed {
			t.Fatal("unsafe route accepted GET", path, w.Code)
		}
	}
}

func TestShopStatusIsScopedAndDoesNotConsumeActionQuota(t *testing.T) {
	s, err := store.New(filepath.Join(t.TempDir(), "state.json"))
	if err != nil {
		t.Fatal(err)
	}
	admin, _, err := s.CreateAdmin("admin", "password123")
	if err != nil {
		t.Fatal(err)
	}
	_, err = s.CreateUser("member", "password123", "user")
	if err != nil {
		t.Fatal(err)
	}
	_, token, err := s.Authenticate("member", "password123")
	if err != nil {
		t.Fatal(err)
	}
	if err := s.AddAccountForUser(admin.ID, model.Account{Name: "private"}); err != nil {
		t.Fatal(err)
	}
	a := s.AccountsForUser(admin.ID, false)[0]
	p, err := s.CreateExchangePlan(admin.ID, false, model.ExchangePlan{AccountID: a.ID, GoodsID: "g", Enabled: true, Auto: true, ExchangeAt: time.Now().Add(time.Minute).Unix()})
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err := s.ClaimExchangePlanAt(admin.ID, false, p.ID, true, time.Now(), 3*time.Minute); err != nil {
		t.Fatal(err)
	}
	server := NewServer(s)
	defer server.Stop()
	h := server.Handler()
	for n := 0; n < 40; n++ {
		w := callAPI(h, http.MethodGet, "/api/v1/shop/status", token, "")
		if w.Code != http.StatusOK {
			t.Fatal("status polling exhausted action quota", n, w.Code)
		}
		var body struct {
			Data shop.EngineStatus `json:"data"`
		}
		if err := json.Unmarshal(w.Body.Bytes(), &body); err != nil {
			t.Fatal(err)
		}
		if body.Data.Running != 0 || body.Data.NextRun != 0 {
			t.Fatal("foreign plans leaked through status", body.Data)
		}
	}
	if w := callAPI(h, http.MethodPost, "/api/v1/login/sms/cancel", token, `{}`); w.Code != http.StatusOK {
		t.Fatal("polling consumed action quota", w.Body.String())
	}
	if w := callAPI(h, http.MethodGet, "/api/v1/shop/status", "", ""); w.Code != http.StatusUnauthorized {
		t.Fatal("shop status is publicly accessible")
	}
}
