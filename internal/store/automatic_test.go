package store

import (
	"path/filepath"
	"testing"
	"time"

	"github.com/eleost04/miyohub/internal/model"
)

func TestAutomaticClaimSurvivesRestartAndFollowsCurrentOwnerSettings(t *testing.T) {
	path := filepath.Join(t.TempDir(), "state.json")
	s, _ := New(path)
	u, _, _ := s.CreateAdmin("admin", "test-password")
	_ = s.AddAccountForUser(u.ID, model.Account{Name: "own"})
	a := s.AccountsForUser(u.ID, false)[0]
	settings := *a.TaskSettings
	settings.Schedule = &model.AccountSchedule{Time: "10:30", Timezone: "Asia/Shanghai"}
	a, err := s.UpdateAccountTasks(u, a.ID, settings)
	if err != nil {
		t.Fatal(err)
	}
	now := time.Date(2026, 9, 11, 2, 30, 0, 0, time.UTC)
	if claimed, err := s.ClaimAutomaticRun(a.ID, settings.Revision, now); err != nil || claimed {
		t.Fatal("stale settings were claimed")
	}
	if claimed, err := s.ClaimAutomaticRun(a.ID, a.TaskSettings.Revision, now); err != nil || !claimed {
		t.Fatal("automatic work not claimed", err)
	}
	restored, err := New(path)
	if err != nil {
		t.Fatal(err)
	}
	if claimed, _ := restored.ClaimAutomaticRun(a.ID, a.TaskSettings.Revision, now.Add(time.Minute)); claimed {
		t.Fatal("restart repeated automatic work")
	}
	if claimed, _ := restored.ClaimAutomaticRun(a.ID, a.TaskSettings.Revision, now.AddDate(0, 0, 1)); !claimed {
		t.Fatal("new local day remained blocked")
	}
}
