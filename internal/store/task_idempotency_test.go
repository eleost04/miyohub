package store

import (
	"testing"

	"github.com/eleost04/miyohub/internal/model"
)

func TestEquivalentTaskAndCaptchaSavesKeepRevisionAndAuthorization(t *testing.T) {
	s, _, user, _ := preferenceFixture(t)
	a := s.AccountsForUser(user.ID, false)[0]
	for range 3 {
		saved, err := s.UpdateAccountTasks(user, a.ID, *a.TaskSettings)
		if err != nil || saved.TaskSettings.Revision != a.TaskSettings.Revision {
			t.Fatal("unchanged task save advanced the revision", err)
		}
	}
	p := *a.TaskSettings
	p.Schedule = &model.AccountSchedule{Time: "12:34", Timezone: model.DefaultTimezone}
	p.Automatic = !p.Automatic
	saved, err := s.UpdateAccountTasks(user, a.ID, p)
	if err != nil || saved.TaskSettings.Revision != a.TaskSettings.Revision+1 || !saved.TaskSettings.SameWork(a.TaskSettings) {
		t.Fatal("schedule change lost optimistic locking or revoked identical work", err)
	}
	if _, err := s.UpdateAccountTasks(user, a.ID, *a.TaskSettings); err == nil {
		t.Fatal("stale task editor overwrote a changed schedule")
	}
	c := s.CaptchaSettingsForUser(user.ID).UserCaptchaConfig
	c.Source = "personal"
	c.Channels = []model.CaptchaChannel{{ID: "solver", Provider: "custom", Enabled: true, Endpoint: "https://solver.example/pass_nine", Token: "fixture-only-secret", Timeout: 30}}
	if err := s.UpdateUserCaptcha(user.ID, c); err != nil {
		t.Fatal(err)
	}
	c = s.CaptchaSettingsForUser(user.ID).UserCaptchaConfig
	runtime := s.CaptchaForUser(user.ID)
	for range 3 {
		if err := s.UpdateUserCaptcha(user.ID, c); err != nil || s.CaptchaSettingsForUser(user.ID).Revision != c.Revision || !runtime.Allowed() {
			t.Fatal("redacted no-op captcha save revoked its active snapshot", err)
		}
	}
	c.Source = "off"
	if err := s.UpdateUserCaptcha(user.ID, c); err != nil || runtime.Allowed() {
		t.Fatal("real captcha change retained old authorization", err)
	}
}
