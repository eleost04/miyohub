package api

import (
	"errors"
	"net/http"

	"github.com/eleost04/miyohub/internal/model"
	"github.com/eleost04/miyohub/internal/store"
)

func (s *Server) visibleConfig(user model.User) model.Config {
	cfg := publicConfigForUser(s.store.Config(), user)
	for i := range cfg.Accounts {
		cfg.Accounts[i].ExchangeAllowed = s.store.AccountCanExchange(cfg.Accounts[i].ID)
	}
	return cfg
}

func (s *Server) accountTasks(w http.ResponseWriter, r *http.Request, user model.User) {
	if r.Method != http.MethodPut {
		methodNotAllowed(w)
		return
	}
	var input struct {
		ID    string                    `json:"id"`
		Tasks model.AccountTaskSettings `json:"task_settings"`
	}
	if !decodeJSON(w, r, &input) {
		return
	}
	if _, ok := s.accountFor(user, input.ID); !ok {
		writeError(w, 403, errors.New("账号不存在或无权访问"))
		return
	}
	a, err := s.store.UpdateAccountTasks(user, input.ID, input.Tasks)
	if err != nil {
		writeError(w, 400, err)
		return
	}
	// Changing daily tasks must not interrupt an unrelated exchange plan.
	s.runner.CancelAccount(a.ID)
	a = publicAccount(a)
	a.ExchangeAllowed = s.store.AccountCanExchange(a.ID)
	writeJSON(w, 200, map[string]any{"ok": true, "data": a})
}

func (s *Server) captchaConfig(w http.ResponseWriter, r *http.Request, user model.User) {
	switch r.Method {
	case http.MethodGet:
	case http.MethodPut:
		var input model.UserCaptchaConfig
		if !decodeJSON(w, r, &input) {
			return
		}
		if err := s.store.UpdateUserCaptcha(user.ID, input); err != nil {
			status := 400
			if errors.Is(err, store.ErrSiteCaptchaPermission) {
				status = 403
			}
			if errors.Is(err, store.ErrCaptchaRevision) {
				status = 409
			}
			writeError(w, status, err)
			return
		}
		// Revoke an in-flight solver request as well as preventing later calls.
		s.sms.Cancel(user.ID)
		for _, a := range s.store.AccountsForUser(user.ID, false) {
			s.runner.CancelAccount(a.ID)
		}
	default:
		methodNotAllowed(w)
		return
	}
	writeJSON(w, 200, map[string]any{"ok": true, "data": s.store.CaptchaSettingsForUser(user.ID)})
}

func (s *Server) adminUserPermissions(w http.ResponseWriter, r *http.Request, actor model.User) {
	if !requireAdmin(w, actor) {
		return
	}
	if r.Method != http.MethodPut {
		methodNotAllowed(w)
		return
	}
	var input struct {
		UserID      string                `json:"user_id"`
		Permissions model.UserPermissions `json:"permissions"`
	}
	if !decodeJSON(w, r, &input) {
		return
	}
	var before model.User
	for _, u := range s.store.ListUsers() {
		if u.ID == input.UserID {
			before = u
			break
		}
	}
	if err := s.store.UpdateUserPermissions(actor, input.UserID, input.Permissions); err != nil {
		writeError(w, 400, err)
		return
	}
	for _, a := range s.store.AccountsForUser(input.UserID, false) {
		if before.CanExchange() && !input.Permissions.Exchange {
			s.exchange.CancelAccount(a.ID)
		}
		if before.CanUseSiteCaptcha() && !input.Permissions.SiteCaptcha {
			s.runner.CancelAccount(a.ID)
		}
	}
	if before.CanUseSiteCaptcha() && !input.Permissions.SiteCaptcha {
		s.sms.Cancel(input.UserID)
	}
	s.exchange.Wake()
	_ = s.store.AddLogForUser(input.UserID, "admin", "管理员已更新你的商品兑换与站点打码权限")
	writeJSON(w, 200, map[string]any{"ok": true})
}

func (s *Server) adminUserAccess(w http.ResponseWriter, r *http.Request, actor model.User) {
	var input struct {
		UserID      string                `json:"user_id"`
		Role        string                `json:"role"`
		Status      string                `json:"status"`
		Permissions model.UserPermissions `json:"permissions"`
	}
	if !decodeJSON(w, r, &input) {
		return
	}
	before, after, err := s.store.UpdateUserAccess(actor, input.UserID, input.Role, input.Status, input.Permissions)
	if err != nil {
		writeError(w, 400, err)
		return
	}
	if after.Status != "active" || before.Role == "admin" && after.Role != "admin" {
		s.cancelUserWork(after.ID)
	} else {
		for _, a := range s.store.AccountsForUser(after.ID, false) {
			if before.CanExchange() && !after.CanExchange() {
				s.exchange.CancelAccount(a.ID)
			}
			if before.CanUseSiteCaptcha() && !after.CanUseSiteCaptcha() {
				s.runner.CancelAccount(a.ID)
			}
		}
		if before.CanUseSiteCaptcha() && !after.CanUseSiteCaptcha() {
			s.sms.Cancel(after.ID)
		}
	}
	s.exchange.Wake()
	_ = s.store.AddLogForUser(after.ID, "admin", "管理员已更新你的用户状态与服务权限")
	writeJSON(w, 200, map[string]any{"ok": true, "data": after})
}
