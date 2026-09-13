package notify_test

import (
	"context"
	"path/filepath"
	"sync/atomic"
	"testing"
	"time"

	"github.com/eleost04/miyohub/internal/model"
	"github.com/eleost04/miyohub/internal/notify"
	"github.com/eleost04/miyohub/internal/store"
)

type calendarSender struct{ calls atomic.Int32 }

func (s *calendarSender) Send(_ context.Context, cfg model.PushConfig, _, _ string, _ bool) []notify.Result {
	s.calls.Add(1)
	return []notify.Result{{OK: true, ChannelID: cfg.Channels[0].ID, Provider: "webhook"}}
}

func TestDispatcherSchedulesCalendarWithoutUpstreamQueries(t *testing.T) {
	s, err := store.New(filepath.Join(t.TempDir(), "state.json"))
	if err != nil {
		t.Fatal(err)
	}
	u, _, _ := s.CreateAdmin("owner", "test-password")
	_ = s.AddAccountForUser(u.ID, model.Account{Name: "fixture", Stuid: "112001"})
	a := s.AccountsForUser(u.ID, false)[0]
	_, err = s.UpdatePushSettings(u.ID, store.PushSettingsPatch{Enabled: true, Calendar: true, ErrorOnly: true, Revision: 1, Channels: []store.PushChannelPatch{{PushChannel: model.PushChannel{Name: "fixture", Provider: "webhook", Enabled: true, Webhook: "https://example.invalid/calendar"}}}})
	if err != nil {
		t.Fatal(err)
	}
	now := time.Now()
	creation := now.Add(-2 * time.Minute)
	at := now.Add(9 * time.Minute)
	_, err = s.AddCalendarReminder(u.ID, a.ID, "genshin", model.CalendarEvent{ID: "official_fixture", Title: "fixture", Kind: "activity", Source: "official", StartAt: &at}, "start", 10, creation, creation)
	if err != nil {
		t.Fatal(err)
	}
	sender := &calendarSender{}
	dispatcher := notify.NewDispatcher(s, sender)
	defer dispatcher.Stop()
	if err := dispatcher.Start(); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(t.Context(), 4*time.Second)
	defer cancel()
	ticker := time.NewTicker(5 * time.Millisecond)
	defer ticker.Stop()
	for s.CalendarReminders(u.ID, a.ID, "genshin")[0].Status != "processed" {
		select {
		case <-ctx.Done():
			t.Fatal("local reminder worker did not dispatch")
		case <-ticker.C:
		}
	}
	dispatcher.Stop()
	if sender.calls.Load() != 1 {
		t.Fatal("reminder was not sent once")
	}
	if len(s.PushHistory(u.ID)) != 1 || s.PushHistory(u.ID)[0].Status != "accepted" {
		t.Fatal("service acceptance missing")
	}
}
