package api

import (
	"encoding/json"
	"path/filepath"
	"testing"
	"time"

	"github.com/eleost04/miyohub/internal/model"
	"github.com/eleost04/miyohub/internal/store"
)

func TestCalendarReminderAPIExplicitScopeAndNoImplicitPush(t *testing.T) {
	s, err := store.New(filepath.Join(t.TempDir(), "state.json"))
	if err != nil {
		t.Fatal(err)
	}
	u, token, _ := s.CreateAdmin("owner", "test-password")
	other, _ := s.CreateUser("other", "test-password", "user")
	_ = s.AddAccountForUser(u.ID, model.Account{Name: "fixture", Stuid: "111001"})
	_ = s.AddAccountForUser(other.ID, model.Account{Name: "private", Stuid: "111002"})
	a := s.AccountsForUser(u.ID, false)[0]
	b := s.AccountsForUser(other.ID, false)[0]
	at := time.Now().Add(2 * time.Hour)
	event, err := s.AddCalendarEvent(u.ID, a.ID, "genshin", model.CalendarEvent{Title: "fixture", Kind: "version", StartAt: &at})
	if err != nil {
		t.Fatal(err)
	}
	server := NewServer(s)
	defer server.Stop()
	h := server.Handler()
	body := map[string]any{"account_id": a.ID, "game": "genshin", "event_id": event.ID, "target": "start", "lead_minutes": 60}
	encode := func() string { raw, _ := json.Marshal(body); return string(raw) }
	if w := callAPI(h, "POST", "/api/v1/calendar/reminders", "", encode()); w.Code != 401 {
		t.Fatal("anonymous subscription")
	}
	body["account_id"] = b.ID
	if w := callAPI(h, "POST", "/api/v1/calendar/reminders", token, encode()); w.Code != 404 {
		t.Fatal("admin subscription crossed ownership")
	}
	body["account_id"], body["lead_minutes"] = a.ID, nil
	if w := callAPI(h, "POST", "/api/v1/calendar/reminders", token, encode()); w.Code != 400 {
		t.Fatal("null implicitly became on-time reminder")
	}
	body["lead_minutes"], body["event_id"] = 60, "official_forged"
	if w := callAPI(h, "POST", "/api/v1/calendar/reminders", token, encode()); w.Code != 400 {
		t.Fatal("forged snapshot accepted")
	}
	body["event_id"] = event.ID
	if w := callAPI(h, "POST", "/api/v1/calendar/reminders", token, encode()); w.Code != 201 {
		t.Fatal("valid subscription rejected", w.Code)
	}
	if w := callAPI(h, "POST", "/api/v1/calendar/reminders", token, encode()); w.Code != 400 {
		t.Fatal("duplicate subscription accepted")
	}
	if s.PushConfigForUser(u.ID).Enabled || s.PushConfigForUser(u.ID).Calendar || len(s.PushHistory(u.ID)) != 0 || server.runner.Running() {
		t.Fatal("subscription enabled push or ran work")
	}
	if w := callAPI(h, "GET", "/api/v1/calendar/reminders?game=genshin&account_id="+b.ID, token, ""); w.Code != 404 {
		t.Fatal("admin read private reminders")
	}
}
