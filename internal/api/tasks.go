package api

import (
	"errors"
	"net/http"

	"github.com/eleost04/miyohub/internal/auth"
	"github.com/eleost04/miyohub/internal/model"
	"github.com/eleost04/miyohub/internal/tasks"
)

type taskInput struct {
	AccountIDs []string `json:"account_ids"`
	tasks.RunOptions
}

func (s *Server) readTaskSelection(w http.ResponseWriter, r *http.Request, user model.User) (taskInput, bool) {
	var input taskInput
	if !decodeJSON(w, r, &input) {
		return input, false
	}
	if err := input.RunOptions.Validate(); err != nil {
		writeError(w, 400, err)
		return input, false
	}
	if input.AccountIDs == nil {
		input.AccountIDs = []string{}
		for _, a := range s.store.AccountsForUser(user.ID, user.Role == "admin") {
			input.AccountIDs = append(input.AccountIDs, a.ID)
		}
	}
	if len(input.AccountIDs) == 0 {
		writeError(w, 400, errors.New("请选择需要执行的账号"))
		return input, false
	}
	for _, id := range input.AccountIDs {
		if _, ok := s.accountFor(user, id); !ok {
			writeError(w, 403, errors.New("账号不存在或无权访问"))
			return input, false
		}
	}
	return input, true
}

func (s *Server) cancelRun(w http.ResponseWriter, r *http.Request, user model.User) {
	if r.Method != http.MethodPost {
		methodNotAllowed(w)
		return
	}
	input, ok := s.readTaskSelection(w, r, user)
	if !ok {
		return
	}
	for _, id := range input.AccountIDs {
		s.runner.CancelAccount(id, "用户主动停止任务")
	}
	writeJSON(w, 200, map[string]any{"ok": true})
}

func (s *Server) smsSend(w http.ResponseWriter, r *http.Request, user model.User) {
	if r.Method != http.MethodPost {
		methodNotAllowed(w)
		return
	}
	var input struct {
		Phone       string `json:"phone"`
		Name        string `json:"account_name"`
		AccountID   string `json:"account_id"`
		CaptchaMode string `json:"captcha_mode"`
	}
	if !decodeJSON(w, r, &input) {
		return
	}
	state, err := s.sms.Send(r.Context(), user.ID, input.Phone, input.Name, input.AccountID, input.CaptchaMode)
	if err != nil {
		writeJSON(w, 400, map[string]any{"ok": false, "error": err.Error(), "data": s.sms.State(user.ID)})
		return
	}
	writeJSON(w, 200, map[string]any{"ok": true, "data": state})
}
func (s *Server) smsVerify(w http.ResponseWriter, r *http.Request, user model.User) {
	if r.Method != http.MethodPost {
		methodNotAllowed(w)
		return
	}
	var input struct {
		Code string `json:"captcha"`
	}
	if !decodeJSON(w, r, &input) {
		return
	}
	if err := s.sms.Verify(r.Context(), user.ID, input.Code); err != nil {
		if errors.Is(err, auth.ErrSMSCaptchaRequired) {
			writeJSON(w, 200, map[string]any{"ok": true, "data": s.sms.State(user.ID)})
			return
		}
		writeJSON(w, 400, map[string]any{"ok": false, "error": err.Error(), "data": s.sms.State(user.ID)})
		return
	}
	writeJSON(w, 200, map[string]any{"ok": true, "data": auth.SMSState{Status: "verified", Message: "米游社账号已绑定"}})
}

func (s *Server) smsState(w http.ResponseWriter, r *http.Request, user model.User) {
	if r.Method != http.MethodGet {
		methodNotAllowed(w)
		return
	}
	writeJSON(w, 200, map[string]any{"ok": true, "data": s.sms.State(user.ID)})
}
func (s *Server) smsCaptcha(w http.ResponseWriter, r *http.Request, user model.User) {
	if r.Method != http.MethodPost {
		methodNotAllowed(w)
		return
	}
	var input auth.SMSCaptchaSolution
	if !decodeJSON(w, r, &input) {
		return
	}
	state, err := s.sms.CompleteCaptcha(r.Context(), user.ID, input)
	if err != nil {
		writeJSON(w, 400, map[string]any{"ok": false, "error": err.Error(), "data": state})
		return
	}
	writeJSON(w, 200, map[string]any{"ok": true, "data": state})
}
func (s *Server) smsCancel(w http.ResponseWriter, r *http.Request, user model.User) {
	if r.Method != http.MethodPost {
		methodNotAllowed(w)
		return
	}
	s.sms.Cancel(user.ID)
	writeJSON(w, 200, map[string]any{"ok": true})
}
func (s *Server) shopStatus(w http.ResponseWriter, r *http.Request, user model.User) {
	if r.Method != http.MethodGet {
		methodNotAllowed(w)
		return
	}
	writeJSON(w, 200, map[string]any{"ok": true, "data": s.exchange.Status(user)})
}
