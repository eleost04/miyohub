package api

import (
	"errors"
	"maps"
	"net/http"
	"sort"

	"github.com/eleost04/miyohub/internal/auth"
	"github.com/eleost04/miyohub/internal/model"
	"github.com/eleost04/miyohub/internal/shop"
	"github.com/eleost04/miyohub/internal/store"
)

func publicAccount(a model.Account) model.Account {
	a.HasCookie = a.Cookie != ""
	a.HasSToken = a.Stoken != ""
	a.CloudConfigured = []string{}
	for k, v := range a.CloudTokens {
		if v != "" {
			a.CloudConfigured = append(a.CloudConfigured, k)
		}
	}
	sort.Strings(a.CloudConfigured)
	a.Cookie = ""
	a.Stoken = ""
	a.Mid = ""
	a.CloudTokens = nil
	a.Device = model.Device{}
	a.ShopDeviceFP = ""
	return a
}
func (s *Server) accounts(w http.ResponseWriter, r *http.Request, u model.User) {
	switch r.Method {
	case http.MethodGet:
		accounts := s.store.AccountsForUser(u.ID, u.Role == "admin")
		for i := range accounts {
			accounts[i] = publicAccount(accounts[i])
			accounts[i].ExchangeAllowed = s.store.AccountCanExchange(accounts[i].ID)
		}
		writeJSON(w, 200, map[string]any{"ok": true, "data": accounts})
	case http.MethodPost:
		var a model.Account
		if !decodeJSON(w, r, &a) {
			return
		}
		if a.Cookie == "" {
			writeError(w, 400, errors.New("请输入完整 Cookie，或使用扫码绑定"))
			return
		}
		a = model.Account{Name: a.Name, Cookie: a.Cookie, Stuid: a.Stuid, Stoken: a.Stoken, Mid: a.Mid, CloudTokens: a.CloudTokens, Disabled: a.Disabled}
		if err := s.store.AddAccountForUser(u.ID, a); err != nil {
			writeError(w, 400, err)
			return
		}
		writeJSON(w, 201, map[string]any{"ok": true})
	case http.MethodPut:
		var p store.AccountPatch
		if !decodeJSON(w, r, &p) {
			return
		}
		before, _ := s.accountFor(u, p.ID)
		a, err := s.store.UpdateAccount(u, p)
		if err != nil {
			writeError(w, 400, err)
			return
		}
		if a.Disabled != before.Disabled || a.Cookie != before.Cookie || a.Stoken != before.Stoken || a.Mid != before.Mid || !maps.Equal(a.CloudTokens, before.CloudTokens) {
			s.cancelAccountWork(a.ID)
		}
		s.exchange.Wake()
		writeJSON(w, 200, map[string]any{"ok": true, "data": publicAccount(a)})
	case http.MethodDelete:
		if a, ok := s.accountFor(u, r.URL.Query().Get("id")); ok {
			s.cancelAccountWork(a.ID)
		}
		if err := s.store.DeleteAccount(u, r.URL.Query().Get("id")); err != nil {
			writeError(w, 404, err)
			return
		}
		writeJSON(w, 200, map[string]any{"ok": true})
	default:
		methodNotAllowed(w)
	}
}
func (s *Server) accountCheck(w http.ResponseWriter, r *http.Request, u model.User) {
	if r.Method != http.MethodPost {
		methodNotAllowed(w)
		return
	}
	var p struct {
		ID string `json:"id"`
	}
	if !decodeJSON(w, r, &p) {
		return
	}
	a, ok := s.accountFor(u, p.ID)
	if !ok {
		writeError(w, 404, errors.New("账号不存在"))
		return
	}
	cfg := s.store.Config()
	if a.Device.ID != "" {
		cfg.Device = a.Device
	}
	service := shop.Service{Client: s.shopClient, Config: cfg, Account: a}
	_, err := service.Points(r.Context())
	if err != nil && a.Stoken != "" && r.Context().Err() == nil {
		if cookie, refreshErr := auth.RefreshCookie(r.Context(), s.shopClient, a); refreshErr == nil {
			updated, saveErr := s.store.RenewAccountCookie(a, cookie)
			if saveErr != nil {
				writeError(w, 500, saveErr)
				return
			}
			service.Account = updated
			_, err = service.Points(r.Context())
		}
	}
	status := "valid"
	if err != nil {
		status = "check_failed"
	}
	if saveErr := s.store.RecordAccountCheck(a.ID, status); saveErr != nil {
		writeError(w, 500, saveErr)
		return
	}
	if err != nil {
		writeError(w, 502, err)
		return
	}
	writeJSON(w, 200, map[string]any{"ok": true, "data": map[string]string{"status": status}})
}
