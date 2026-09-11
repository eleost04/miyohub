package api

import (
	"errors"
	"net/http"

	"github.com/eleost04/miyohub/internal/model"
	"github.com/eleost04/miyohub/internal/shop"
	"github.com/eleost04/miyohub/internal/store"
)

func (s *Server) preparePlan(r *http.Request, u model.User, p model.ExchangePlan) (model.ExchangePlan, error) {
	if err := s.store.CheckExchangeAccess(u.ID, u.Role == "admin", p.AccountID); err != nil {
		return p, err
	}
	a, ok := s.accountFor(u, p.AccountID)
	if !ok {
		return p, errors.New("账号不存在或无权访问")
	}
	if _, ok := s.store.AccountRunnable(a.ID); !ok {
		return p, errors.New("账号或所属用户已停用")
	}
	service := shop.Service{Client: s.shopClient, Config: s.store.Config(), Account: a}
	prepared, err := service.Prepare(r.Context(), p, false)
	if err != nil {
		return p, err
	}
	if err := s.store.SetAccountDeviceFP(a.ID, a.Device.ID, prepared.DeviceFP); err != nil {
		return p, err
	}
	prepared.DeviceFP = ""
	return prepared, nil
}
func (s *Server) shopPlans(w http.ResponseWriter, r *http.Request, u model.User) {
	switch r.Method {
	case http.MethodGet:
		writeJSON(w, 200, map[string]any{"ok": true, "data": s.store.ExchangePlansForUser(u.ID, u.Role == "admin")})
	case http.MethodPost, http.MethodPut:
		if !s.store.UserCanExchange(u.ID) {
			writeError(w, 403, store.ErrExchangePermission)
			return
		}
		var p model.ExchangePlan
		if !decodeJSON(w, r, &p) {
			return
		}
		if r.Method == http.MethodPut {
			old, ok := s.store.ExchangePlanForUser(u.ID, u.Role == "admin", p.ID)
			if !ok {
				writeError(w, 404, errors.New("兑换计划不存在或无权访问"))
				return
			}
			if old.State != "pending" || old.Revision != p.Revision {
				writeError(w, 409, errors.New("计划已执行或被修改，请刷新"))
				return
			}
		}
		prepared, err := s.preparePlan(r, u, p)
		if err != nil {
			status := 400
			if errors.Is(err, store.ErrExchangePermission) {
				status = 403
			}
			writeError(w, status, err)
			return
		}
		var result model.ExchangePlan
		if r.Method == http.MethodPost {
			result, err = s.store.CreateExchangePlan(u.ID, u.Role == "admin", prepared)
		} else {
			result, err = s.store.UpdateExchangePlan(u.ID, u.Role == "admin", prepared)
		}
		if err != nil {
			writeError(w, 400, err)
			return
		}
		s.exchange.Wake()
		writeJSON(w, 200, map[string]any{"ok": true, "data": result})
	case http.MethodDelete:
		if err := s.store.DeleteExchangePlan(u.ID, u.Role == "admin", r.URL.Query().Get("id")); err != nil {
			writeError(w, 400, err)
			return
		}
		s.exchange.Wake()
		writeJSON(w, 200, map[string]any{"ok": true})
	default:
		methodNotAllowed(w)
	}
}
func (s *Server) shopPlanRun(w http.ResponseWriter, r *http.Request, u model.User) {
	if r.Method != http.MethodPost {
		methodNotAllowed(w)
		return
	}
	if !s.store.UserCanExchange(u.ID) {
		writeError(w, 403, store.ErrExchangePermission)
		return
	}
	var input struct {
		ID string `json:"id"`
	}
	if !decodeJSON(w, r, &input) {
		return
	}
	p, err := s.exchange.Run(u.ID, u.Role == "admin", input.ID, false)
	if err != nil {
		status := 409
		if errors.Is(err, store.ErrExchangePermission) {
			status = 403
		}
		writeError(w, status, err)
		return
	}
	writeJSON(w, 202, map[string]any{"ok": true, "data": p})
}
func (s *Server) shopPlanCancel(w http.ResponseWriter, r *http.Request, u model.User) {
	if r.Method != http.MethodPost {
		methodNotAllowed(w)
		return
	}
	var input struct {
		ID string `json:"id"`
	}
	if !decodeJSON(w, r, &input) {
		return
	}
	if _, ok := s.store.ExchangePlanForUser(u.ID, u.Role == "admin", input.ID); !ok {
		writeError(w, 404, errors.New("兑换计划不存在或无权访问"))
		return
	}
	s.exchange.Cancel(input.ID)
	writeJSON(w, 200, map[string]any{"ok": true})
}
func (s *Server) shopDeviceFP(w http.ResponseWriter, r *http.Request, u model.User) {
	if r.Method != http.MethodPost {
		methodNotAllowed(w)
		return
	}
	var input struct {
		AccountID string `json:"account_id"`
	}
	if !decodeJSON(w, r, &input) {
		return
	}
	a, ok := s.accountFor(u, input.AccountID)
	if !ok {
		writeError(w, 404, errors.New("账号不存在或无权访问"))
		return
	}
	service := shop.Service{Client: s.shopClient, Config: s.store.Config(), Account: a}
	fp, err := service.DeviceFP(r.Context())
	if err != nil {
		writeError(w, 502, err)
		return
	}
	if err = s.store.SetAccountDeviceFP(a.ID, a.Device.ID, fp); err != nil {
		writeError(w, 500, err)
		return
	}
	writeJSON(w, 200, map[string]any{"ok": true})
}
