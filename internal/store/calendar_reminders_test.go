package store

import (
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/eleost04/miyohub/internal/model"
	"github.com/eleost04/miyohub/internal/notify"
)

func calendarReminderFixture(t *testing.T, enabled bool) (*Store, model.User, model.User, model.Account, model.CalendarReminder) {
	t.Helper()
	s, admin, user := pushFixture(t)
	view := configurePush(t, s, user.ID)
	p := pushPatch(view)
	p.Calendar, p.ErrorOnly = enabled, true
	if _, err := s.UpdatePushSettings(user.ID, p); err != nil {
		t.Fatal(err)
	}
	if err := s.AddAccountForUser(user.ID, model.Account{Name: "fixture", Cookie: "stuid=110001;cookie_token=synthetic-calendar-secret"}); err != nil {
		t.Fatal(err)
	}
	a := s.AccountsForUser(user.ID, false)[0]
	now := time.Now()
	created := now.Add(-2 * time.Minute)
	at := now.Add(9 * time.Minute)
	event := model.CalendarEvent{ID: "official_fixture", Title: "合成活动", Kind: "activity", Source: "official", StartAt: &at}
	r, err := s.AddCalendarReminder(user.ID, a.ID, "genshin", event, "start", 10, created, created)
	if err != nil {
		t.Fatal(err)
	}
	return s, admin, user, a, r
}

func TestCalendarReminderDurableAtomicQueueAndNoReplay(t *testing.T) {
	s, admin, user, a, reminder := calendarReminderFixture(t, true)
	if len(s.CalendarReminders(admin.ID, a.ID, "genshin")) != 0 {
		t.Fatal("admin saw private reminder")
	}
	if err := s.CancelCalendarReminder(admin.ID, reminder.ID); err == nil {
		t.Fatal("admin cancelled private reminder")
	}
	if queued, err := s.QueueDueCalendarReminders(reminder.RemindAt.Add(-time.Second)); err != nil || queued {
		t.Fatal("reminder queued early")
	}
	var wg sync.WaitGroup
	results := make(chan bool, 2)
	for i := 0; i < 2; i++ {
		wg.Go(func() {
			queued, err := s.QueueDueCalendarReminders(time.Now())
			if err != nil {
				t.Error(err)
			}
			results <- queued
		})
	}
	wg.Wait()
	close(results)
	count := 0
	for queued := range results {
		if queued {
			count++
		}
	}
	if count != 1 || len(s.PushHistory(user.ID)) != 1 || s.CalendarReminders(user.ID, a.ID, "genshin")[0].Status != "queued" {
		t.Fatal("subscription and outbox did not commit once")
	}
	if queued, _ := s.QueuePushEvent(notify.CalendarEvent(s.Config(), a, reminder)); queued {
		t.Fatal("calendar reference replayed")
	}
	reopened, err := New(s.path)
	if err != nil {
		t.Fatal(err)
	}
	if queued, err := reopened.QueueDueCalendarReminders(time.Now()); queued || err != nil {
		t.Fatal("restart repeated reminder")
	}
	job, _, ok, err := reopened.ClaimPushDelivery()
	if err != nil || !ok || job.UserID != user.ID || job.ReferenceID != reminder.ID || strings.Contains(job.Message, "synthetic-calendar-secret") {
		t.Fatal("private or missing reminder delivery", err)
	}
	if err := reopened.FinishPushDelivery(job.ID, notify.Result{OK: true}); err != nil {
		t.Fatal(err)
	}
	result := reopened.CalendarReminders(user.ID, a.ID, "genshin")[0]
	if result.Status != "processed" || result.AcceptedChannels != 1 || result.Detail != "推送服务已接收通知" {
		t.Fatal("acceptance misreported")
	}
	if err := reopened.CancelCalendarReminder(user.ID, reminder.ID); err == nil {
		t.Fatal("accepted notification claimed to be recalled")
	}
	if _, _, ok, _ := reopened.ClaimPushDelivery(); ok {
		t.Fatal("processed reminder replayed")
	}
}

func TestCalendarReminderOptInCancellationAndExpiry(t *testing.T) {
	for _, scenario := range []string{"disabled", "cancelled", "expired", "revoked", "queue-cancel", "changed-channel"} {
		t.Run(scenario, func(t *testing.T) {
			s, _, user, a, r := calendarReminderFixture(t, scenario != "disabled")
			now := time.Now()
			switch scenario {
			case "cancelled":
				if err := s.CancelCalendarReminder(user.ID, r.ID); err != nil {
					t.Fatal(err)
				}
			case "expired":
				now = r.TargetAt.Add(time.Hour)
			case "revoked":
				disabled := true
				if _, err := s.UpdateAccount(user, AccountPatch{ID: a.ID, Disabled: &disabled}); err != nil {
					t.Fatal(err)
				}
			}
			queued, err := s.QueueDueCalendarReminders(now)
			if err != nil {
				t.Fatal(err)
			}
			if scenario == "queue-cancel" {
				if !queued {
					t.Fatal("fixture not queued")
				}
				if err := s.CancelCalendarReminder(user.ID, r.ID); err != nil {
					t.Fatal(err)
				}
			} else if scenario == "changed-channel" {
				p := pushPatch(s.PushSettings(user.ID))
				p.Calendar = false
				if _, err := s.UpdatePushSettings(user.ID, p); err != nil {
					t.Fatal(err)
				}
			} else if queued {
				t.Fatal("ineligible reminder queued")
			}
			if _, _, ok, err := s.ClaimPushDelivery(); ok || err != nil {
				t.Fatal("ineligible reminder delivered")
			}
		})
	}
}

func TestCalendarReminderValidationAndPersistenceFailure(t *testing.T) {
	s, _, user, a, r := calendarReminderFixture(t, true)
	now := time.Now()
	at := now.Add(time.Hour)
	e := model.CalendarEvent{ID: "official_new", Title: "fixture", Kind: "version", Source: "official", StartAt: &at}
	for _, lead := range []int{-1, 1, 99999999} {
		if _, err := s.AddCalendarReminder(user.ID, a.ID, "genshin", e, "start", lead, now, now); err == nil {
			t.Fatal("unbounded lead accepted")
		}
	}
	if _, err := s.AddCalendarReminder(user.ID, a.ID, "genshin", e, "end", 0, now, now); err == nil {
		t.Fatal("missing deadline accepted")
	}
	if _, err := s.AddCalendarReminder(user.ID, a.ID, "genshin", e, "start", 0, now.Add(-time.Hour), now); err == nil {
		t.Fatal("stale snapshot accepted")
	}
	// Make an isolated test path unwritable without relying on root chmod rules.
	blocked := filepath.Join(t.TempDir(), "not-a-directory")
	if err := os.WriteFile(blocked, []byte("fixture"), 0600); err != nil {
		t.Fatal(err)
	}
	s.path = filepath.Join(blocked, "state.json")
	if queued, err := s.QueueDueCalendarReminders(now); err == nil || queued {
		t.Fatal("failed write reported queued")
	}
	if len(s.data.PushDeliveries) != 0 || s.CalendarReminders(user.ID, a.ID, "genshin")[0].Status != r.Status {
		t.Fatal("failed outbox transaction consumed reminder")
	}
}

func TestCalendarReminderPreservesAmbiguityAndDoesNotReplayAfterRecovery(t *testing.T) {
	s, _, user, a, _ := calendarReminderFixture(t, true)
	if _, err := s.QueueDueCalendarReminders(time.Now()); err != nil {
		t.Fatal(err)
	}
	if _, _, ok, _ := s.ClaimPushDelivery(); !ok {
		t.Fatal("fixture not claimed")
	}
	if err := s.RecoverPushDeliveries(); err != nil {
		t.Fatal(err)
	}
	r := s.CalendarReminders(user.ID, a.ID, "genshin")[0]
	if r.Status != "processed" || r.UncertainChannels != 1 || !strings.Contains(r.Detail, "未确认") {
		t.Fatal("interrupted reminder treated as success")
	}
	if queued, _ := s.QueueDueCalendarReminders(time.Now()); queued {
		t.Fatal("interrupted reminder repeated")
	}
	if _, _, ok, _ := s.ClaimPushDelivery(); ok {
		t.Fatal("ambiguous delivery replayed")
	}
}

func TestCalendarReminderQueueCapacityDoesNotConsumeSubscription(t *testing.T) {
	s, _, user, a, _ := calendarReminderFixture(t, true)
	s.mu.Lock()
	for i := 0; i < 500; i++ {
		s.data.PushDeliveries = append(s.data.PushDeliveries, model.PushDelivery{ID: randomID("fixture_"), UserID: user.ID, Status: "pending"})
	}
	err := s.saveLocked()
	s.mu.Unlock()
	if err != nil {
		t.Fatal(err)
	}
	if queued, err := s.QueueDueCalendarReminders(time.Now()); queued || err == nil {
		t.Fatal("full queue accepted reminder")
	}
	if s.CalendarReminders(user.ID, a.ID, "genshin")[0].Status != "pending" || len(s.PushHistory(user.ID)) != 100 {
		t.Fatal("full queue consumed subscription")
	}
	if len(s.data.PushDeliveries) != 500 {
		t.Fatal("queue grew past capacity")
	}
}

func TestCalendarReminderManualSourceRevalidationAndHistoryCleanup(t *testing.T) {
	s, _, user, a, r := calendarReminderFixture(t, true)
	if err := s.ClearCalendarReminderHistory(user.ID, a.ID, "genshin", time.Now()); err != nil {
		t.Fatal(err)
	}
	if len(s.CalendarReminders(user.ID, a.ID, "genshin")) != 1 {
		t.Fatal("cleanup removed pending reminder")
	}
	if err := s.CancelCalendarReminder(user.ID, r.ID); err != nil {
		t.Fatal(err)
	}
	if err := s.ClearCalendarReminderHistory(user.ID, a.ID, "genshin", time.Now()); err != nil {
		t.Fatal(err)
	}
	if len(s.CalendarReminders(user.ID, a.ID, "genshin")) != 0 {
		t.Fatal("cancelled reminder could not be cleaned")
	}
	now := time.Now()
	at := now.Add(time.Hour)
	event, err := s.AddCalendarEvent(user.ID, a.ID, "genshin", model.CalendarEvent{Title: "fixture", Kind: "version", StartAt: &at})
	if err != nil {
		t.Fatal(err)
	}
	if err := s.DeleteCalendarEvent(user.ID, event.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := s.AddCalendarReminder(user.ID, a.ID, "genshin", event.CalendarEvent, "start", 0, now, now); err == nil {
		t.Fatal("deleted manual event used for reminder")
	}
}
