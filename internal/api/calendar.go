package api

import (
	"errors"
	"net/http"
	"time"

	"github.com/eleost04/miyohub/internal/mihoyo"
	"github.com/eleost04/miyohub/internal/model"
)

func (s *Server) customCalendar(w http.ResponseWriter, r *http.Request, user model.User) {
	switch r.Method {
	case http.MethodGet:
		accountID, game := r.URL.Query().Get("account_id"), r.URL.Query().Get("game")
		if _, ok := s.accountFor(user, accountID); !ok {
			writeError(w, 404, errors.New("账号不存在或无权访问"))
			return
		}
		if !mihoyo.RecordGame(game) {
			writeError(w, 400, errors.New("游戏参数无效"))
			return
		}
		writeJSON(w, 200, map[string]any{"ok": true, "data": s.store.CalendarEvents(user.ID, accountID, game)})
	case http.MethodPost:
		var input struct {
			AccountID string     `json:"account_id"`
			Game      string     `json:"game"`
			Title     string     `json:"title"`
			Kind      string     `json:"kind"`
			StartAt   *time.Time `json:"start_at"`
			EndAt     *time.Time `json:"end_at"`
		}
		if !decodeJSON(w, r, &input) {
			return
		}
		event, err := s.store.AddCalendarEvent(user.ID, input.AccountID, input.Game, model.CalendarEvent{Title: input.Title, Kind: input.Kind, StartAt: input.StartAt, EndAt: input.EndAt})
		if err != nil {
			writeError(w, 400, err)
			return
		}
		writeJSON(w, 201, map[string]any{"ok": true, "data": event})
	case http.MethodDelete:
		var input struct {
			ID string `json:"id"`
		}
		if !decodeJSON(w, r, &input) {
			return
		}
		if err := s.store.DeleteCalendarEvent(user.ID, input.ID); err != nil {
			writeError(w, 404, err)
			return
		}
		writeJSON(w, 200, map[string]any{"ok": true})
	default:
		methodNotAllowed(w)
	}
}
