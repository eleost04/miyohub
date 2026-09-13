package api

import (
	"errors"
	"net/http"
	"time"

	"github.com/eleost04/miyohub/internal/mihoyo"
	"github.com/eleost04/miyohub/internal/model"
)

func (s *Server) calendarReminders(w http.ResponseWriter, r *http.Request, user model.User) {
	switch r.Method {
	case http.MethodGet:
		id, game := r.URL.Query().Get("account_id"), r.URL.Query().Get("game")
		if _, ok := s.accountFor(user, id); !ok {
			writeError(w, 404, errors.New("账号不存在或无权访问"))
			return
		}
		if !mihoyo.RecordGame(game) {
			writeError(w, 400, errors.New("游戏参数无效"))
			return
		}
		cfg := s.store.PushConfigForUser(user.ID)
		channels := 0
		for _, channel := range cfg.Channels {
			if channel.Enabled {
				channels++
			}
		}
		writeJSON(w, 200, map[string]any{"ok": true, "data": map[string]any{"reminders": s.store.CalendarReminders(user.ID, id, game), "enabled": cfg.Enabled && cfg.Calendar, "channel_count": channels}})
	case http.MethodPost:
		if !s.limits.allow("calendar-reminder:"+user.ID, 30, time.Minute) {
			rateError(w)
			return
		}
		var input struct {
			AccountID   string `json:"account_id"`
			Game        string `json:"game"`
			EventID     string `json:"event_id"`
			Target      string `json:"target"`
			LeadMinutes *int   `json:"lead_minutes"`
		}
		if !decodeJSON(w, r, &input) {
			return
		}
		if input.LeadMinutes == nil || !mihoyo.RecordGame(input.Game) {
			writeError(w, 400, errors.New("请明确选择游戏与提前提醒时间"))
			return
		}
		a, ok := s.accountFor(user, input.AccountID)
		if !ok {
			writeError(w, 404, errors.New("账号不存在或无权访问"))
			return
		}
		var event model.CalendarEvent
		var observed time.Time
		for _, manual := range s.store.CalendarEvents(user.ID, a.ID, input.Game) {
			if manual.ID == input.EventID {
				event, observed = manual.CalendarEvent, manual.CreatedAt
				break
			}
		}
		if event.ID == "" {
			var err error
			event, observed, err = s.records.CalendarEvent(a, input.Game, input.EventID)
			if err != nil {
				writeError(w, 400, err)
				return
			}
		}
		reminder, err := s.store.AddCalendarReminder(user.ID, a.ID, input.Game, event, input.Target, *input.LeadMinutes, observed, time.Now())
		if err != nil {
			writeError(w, 400, err)
			return
		}
		writeJSON(w, 201, map[string]any{"ok": true, "data": reminder})
	case http.MethodDelete:
		var input struct {
			ID string `json:"id"`
		}
		if !decodeJSON(w, r, &input) {
			return
		}
		if err := s.store.CancelCalendarReminder(user.ID, input.ID); err != nil {
			writeError(w, 400, err)
			return
		}
		writeJSON(w, 200, map[string]any{"ok": true})
	default:
		methodNotAllowed(w)
	}
}

func (s *Server) calendarReminderHistory(w http.ResponseWriter, r *http.Request, user model.User) {
	if r.Method != http.MethodDelete {
		methodNotAllowed(w)
		return
	}
	var input struct {
		AccountID string `json:"account_id"`
		Game      string `json:"game"`
	}
	if !decodeJSON(w, r, &input) {
		return
	}
	if !mihoyo.RecordGame(input.Game) {
		writeError(w, 400, errors.New("游戏参数无效"))
		return
	}
	if err := s.store.ClearCalendarReminderHistory(user.ID, input.AccountID, input.Game, time.Now()); err != nil {
		writeError(w, 400, err)
		return
	}
	writeJSON(w, 200, map[string]any{"ok": true})
}
