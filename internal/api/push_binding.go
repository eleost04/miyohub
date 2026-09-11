package api

import (
	"errors"
	"net/http"
	"strings"
	"time"

	"github.com/eleost04/miyohub/internal/model"
	"github.com/eleost04/miyohub/internal/store"
)

func (s *Server) pushHistory(w http.ResponseWriter, r *http.Request, user model.User) {
	if r.Method != http.MethodGet {
		methodNotAllowed(w)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true, "data": s.store.PushHistory(user.ID)})
}
func (s *Server) pushQRState(w http.ResponseWriter, r *http.Request, user model.User) {
	if r.Method != http.MethodGet {
		methodNotAllowed(w)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true, "data": s.pushBindings.State(user.ID)})
}
func (s *Server) pushQRStart(w http.ResponseWriter, r *http.Request, user model.User) {
	if r.Method != http.MethodPost {
		methodNotAllowed(w)
		return
	}
	var input struct {
		Provider  string `json:"provider"`
		ChannelID string `json:"channel_id"`
		Revision  int    `json:"revision"`
	}
	if !decodeJSON(w, r, &input) {
		return
	}
	if !s.limits.allow("push-qr:"+user.ID, 5, time.Minute) {
		rateError(w)
		return
	}
	cfg := s.store.PushConfigForUser(user.ID)
	if cfg.Revision != input.Revision {
		writeError(w, http.StatusConflict, store.ErrPushConflict)
		return
	}
	if input.ChannelID != "" {
		found := false
		for _, channel := range cfg.Channels {
			if channel.ID == input.ChannelID && channel.Provider == input.Provider {
				found = true
			}
		}
		if !found {
			writeError(w, http.StatusNotFound, errors.New("渠道不存在"))
			return
		}
	} else if len(cfg.Channels) >= 10 {
		writeError(w, http.StatusBadRequest, errors.New("最多配置 10 个推送渠道"))
		return
	}
	state, err := s.pushBindings.Start(user.ID, input.Provider, input.ChannelID, input.Revision)
	if err != nil {
		writeError(w, http.StatusBadRequest, err)
		return
	}
	writeJSON(w, http.StatusAccepted, map[string]any{"ok": true, "data": state})
}
func (s *Server) pushQRCancel(w http.ResponseWriter, r *http.Request, user model.User) {
	if r.Method != http.MethodPost {
		methodNotAllowed(w)
		return
	}
	var input struct {
		SessionID string `json:"session_id"`
	}
	if !decodeJSON(w, r, &input) {
		return
	}
	if input.SessionID == "" {
		writeError(w, http.StatusBadRequest, errors.New("缺少扫码会话编号"))
		return
	}
	s.pushBindings.Cancel(user.ID, input.SessionID)
	writeJSON(w, http.StatusOK, map[string]any{"ok": true})
}
func (s *Server) pushQRVerify(w http.ResponseWriter, r *http.Request, user model.User) {
	if r.Method != http.MethodPost {
		methodNotAllowed(w)
		return
	}
	var input struct {
		SessionID string `json:"session_id"`
		Code      string `json:"code"`
	}
	if !decodeJSON(w, r, &input) {
		return
	}
	if !s.limits.allow("push-verify:"+user.ID, 10, time.Minute) {
		rateError(w)
		return
	}
	if err := s.pushBindings.Verify(user.ID, input.SessionID, strings.TrimSpace(input.Code)); err != nil {
		writeError(w, http.StatusBadRequest, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true})
}
