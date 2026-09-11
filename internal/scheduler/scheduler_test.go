package scheduler

import (
	"context"
	"testing"
	"time"

	"github.com/eleost04/miyohub/internal/model"
)

func TestRunOnStartUpdatesStatus(t *testing.T) {
	started := make(chan struct{})
	finish := make(chan struct{})
	runner := func(ctx context.Context) error {
		close(started)
		select {
		case <-finish:
			return nil
		case <-ctx.Done():
			return ctx.Err()
		}
	}
	s := New(model.Schedule{Time: "09:00", RunOnStart: true}, runner, nil)
	if err := s.Start(); err != nil {
		t.Fatal(err)
	}
	defer s.Stop()
	select {
	case <-started:
	case <-time.After(time.Second):
		t.Fatal("scheduled run did not start")
	}
	if !s.Status().Running {
		t.Fatal("scheduler should report a running task")
	}
	close(finish)
	deadline := time.Now().Add(time.Second)
	for s.Status().Running && time.Now().Before(deadline) {
		time.Sleep(5 * time.Millisecond)
	}
	status := s.Status()
	if status.Running || status.LastRun == "" {
		t.Fatalf("unexpected completed status: %#v", status)
	}
}

func TestReloadAndValidation(t *testing.T) {
	s := New(model.Schedule{Enabled: false, Time: "09:00"}, func(context.Context) error { return nil }, nil)
	if err := s.Start(); err != nil {
		t.Fatal(err)
	}
	defer s.Stop()
	if err := s.Reload(model.Schedule{Enabled: true, Time: "10:30", JitterMins: 0}); err != nil {
		t.Fatal(err)
	}
	if status := s.Status(); !status.Enabled || status.Schedule.Time != "10:30" {
		t.Fatalf("reload was not applied: %#v", status)
	}
	if err := s.Reload(model.Schedule{Enabled: true, Time: "invalid"}); err == nil {
		t.Fatal("invalid schedule time should be rejected")
	}
}

func TestNextScheduledRunUsesConfiguredTimezone(t *testing.T) {
	now := time.Date(2026, time.September, 9, 0, 0, 0, 0, time.UTC)
	next, err := nextScheduledRun(model.Schedule{Time: "09:00", Timezone: "Asia/Shanghai"}, now)
	if err != nil {
		t.Fatal(err)
	}
	if got := next.UTC(); got.Hour() != 1 || got.Day() != 9 {
		t.Fatalf("expected 09:00 Shanghai (01:00 UTC), got %s", got)
	}
	if err := Validate(model.Schedule{Time: "09:00", Timezone: "Not/AZone"}); err == nil {
		t.Fatal("invalid timezone should be rejected")
	}
}

func TestDueScheduleActuallyRuns(t *testing.T) {
	started := make(chan struct{}, 1)
	s := New(model.Schedule{Enabled: true, Time: "09:00"}, func(context.Context) error { started <- struct{}{}; return nil }, nil)
	s.nextRun = time.Now().Add(20 * time.Millisecond)
	if err := s.Start(); err != nil {
		t.Fatal(err)
	}
	defer s.Stop()
	select {
	case <-started:
	case <-time.After(time.Second):
		t.Fatal("due schedule was skipped")
	}
}

func TestNextSchedulePreservesLocalHourAcrossDST(t *testing.T) {
	location, err := time.LoadLocation("America/New_York")
	if err != nil {
		t.Fatal(err)
	}
	for _, day := range []time.Time{time.Date(2026, 3, 7, 12, 0, 0, 0, location), time.Date(2026, 10, 31, 12, 0, 0, 0, location)} {
		next, err := nextScheduledRun(model.Schedule{Time: "09:00", Timezone: location.String()}, day)
		if err != nil || next.Hour() != 9 || next.Day() != day.AddDate(0, 0, 1).Day() {
			t.Fatalf("next run %v, err %v", next, err)
		}
	}
}
