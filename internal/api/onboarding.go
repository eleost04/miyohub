package api

import (
	"github.com/eleost04/miyohub/internal/model"
	"net/http"
)

func (s *Server) onboarding(w http.ResponseWriter, r *http.Request, user model.User) {
	if r.Method != http.MethodPost {
		methodNotAllowed(w)
		return
	}
	var input struct {
		Status string `json:"status"`
	}
	if !decodeJSON(w, r, &input) {
		return
	}
	updated, err := s.store.UpdateOnboarding(user.ID, input.Status)
	if err != nil {
		writeError(w, 400, err)
		return
	}
	writeJSON(w, 200, map[string]any{"ok": true, "data": updated})
}
