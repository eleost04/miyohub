package store

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/eleost04/miyohub/internal/model"
)

func TestCustomCalendarOwnershipValidationAndCleanup(t *testing.T) {
	path := filepath.Join(t.TempDir(), "state.json")
	s, err := New(path)
	if err != nil {
		t.Fatal(err)
	}
	u, _, _ := s.CreateAdmin("owner", "test-password")
	other, _ := s.CreateUser("other", "test-password", "user")
	_ = s.AddAccountForUser(u.ID, model.Account{Name: "fixture", Stuid: "101001"})
	_ = s.AddAccountForUser(other.ID, model.Account{Name: "other", Stuid: "101002"})
	a := s.AccountsForUser(u.ID, false)[0]
	b := s.AccountsForUser(other.ID, false)[0]
	start, end := time.Now().Add(time.Hour), time.Now().Add(2*time.Hour)
	event := model.CalendarEvent{Title: "示例版本", Kind: "version", StartAt: &start, EndAt: &end, Source: "official"}
	before, _ := os.ReadFile(path)
	if _, err := s.AddCalendarEvent(u.ID, b.ID, "genshin", event); err == nil {
		t.Fatal("admin added another owner's event")
	}
	for _, mutate := range []func(*model.CalendarEvent){
		func(e *model.CalendarEvent) { e.StartAt, e.EndAt = nil, nil },
		func(e *model.CalendarEvent) { e.EndAt = &start },
		func(e *model.CalendarEvent) { e.Title = "bad\ntext" },
		func(e *model.CalendarEvent) { e.Kind = "unknown" },
		func(e *model.CalendarEvent) { future := time.Now().Add(800 * 24 * time.Hour); e.EndAt = &future },
	} {
		bad := clone(event)
		mutate(&bad)
		if _, err := s.AddCalendarEvent(u.ID, a.ID, "genshin", bad); err == nil {
			t.Fatal("invalid custom event accepted")
		}
	}
	after, _ := os.ReadFile(path)
	if !bytes.Equal(before, after) {
		t.Fatal("rejected event mutated state")
	}
	created, err := s.AddCalendarEvent(u.ID, a.ID, "genshin", event)
	if err != nil || created.Source != "manual" || created.ID == "" {
		t.Fatal("custom source forged", err)
	}
	if len(s.CalendarEvents(other.ID, a.ID, "genshin")) != 0 || len(s.CalendarEvents(u.ID, a.ID, "genshin")) != 1 {
		t.Fatal("calendar crossed ownership")
	}
	if err := s.DeleteCalendarEvent(other.ID, created.ID); err == nil {
		t.Fatal("another owner deleted event")
	}
	if err := s.DeleteAccount(u, a.ID); err != nil {
		t.Fatal(err)
	}
	if len(s.data.CalendarEvents) != 0 {
		t.Fatal("deleted account retained calendar")
	}
}
