package model

import (
	"testing"
	"time"
)

func TestCustomScheduleValidationTimezoneAndDailyGuard(t *testing.T) {
	for _, value := range []AccountSchedule{{Time: "25:00", Timezone: "UTC"}, {Time: "9:00", Timezone: "UTC"}, {Time: "09:00", Timezone: "invalid/zone"}} {
		if ValidateAccountSchedule(value) == nil {
			t.Fatal("invalid schedule accepted")
		}
	}
	now := time.Date(2026, 9, 11, 1, 30, 15, 0, time.UTC)
	a := Account{TaskSettings: &AccountTaskSettings{Automatic: true, Schedule: &AccountSchedule{Time: "09:30", Timezone: "Asia/Shanghai"}}}
	if !a.CustomRunDue(now) || a.CustomRunDue(now.Add(time.Minute)) {
		t.Fatal("custom schedule ignored timezone or missed minute")
	}
	if a.EffectiveSchedule(Schedule{Time: "23:00"}).Time != "09:30" {
		t.Fatal("site overwrote personal time")
	}
	a.LastAutomaticAt = now.Add(-time.Minute)
	if a.CustomRunDue(now) {
		t.Fatal("automatic run repeated on same local day")
	}
	if next := a.NextCustomRun(now); next.In(time.FixedZone("CST", 8*3600)).Format("2006-01-02 15:04") != "2026-09-12 09:30" {
		t.Fatal("incorrect next schedule", next)
	}
	if !a.CustomRunDue(now.AddDate(0, 0, 1)) {
		t.Fatal("next day's schedule suppressed")
	}
}
