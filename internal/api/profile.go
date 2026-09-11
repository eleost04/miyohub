package api

import (
	"github.com/eleost04/miyohub/internal/model"
	"net/http"
)

func (s *Server) changePassword(w http.ResponseWriter, r *http.Request, u model.User) {
	if r.Method != http.MethodPost {
		methodNotAllowed(w)
		return
	}
	var input struct {
		Old string `json:"old_password"`
		New string `json:"new_password"`
	}
	if !decodeJSON(w, r, &input) {
		return
	}
	_, token, err := s.store.ChangePassword(u.ID, input.Old, input.New)
	if err != nil {
		writeError(w, 400, err)
		return
	}
	s.setSession(w, r, token)
	writeJSON(w, 200, map[string]any{"ok": true})
}
