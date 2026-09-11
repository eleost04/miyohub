package store

import (
	"path/filepath"
	"testing"

	"github.com/eleost04/miyohub/internal/model"
)

func TestCaptchaSecretsFollowIdentityNotOrderAndNeverMoveToNewEndpoint(t *testing.T) {
	s, err := New(filepath.Join(t.TempDir(), "state.json"))
	if err != nil {
		t.Fatal(err)
	}
	config := model.CaptchaConfig{MaxRetries: 3, Channels: []model.CaptchaChannel{
		{ID: "local", Provider: "custom", Enabled: true, Endpoint: "http://captcha:9645/pass_nine", Token: "private-local", Timeout: 60},
		{ID: "paid", Provider: "damagou", Enabled: true, UserKey: "private-paid", Timeout: 60},
	}}
	if err := s.UpdateSettings(SettingsPatch{Captcha: &config}); err != nil {
		t.Fatal(err)
	}
	reordered := s.Config().Captcha
	reordered.Channels[0], reordered.Channels[1] = reordered.Channels[1], reordered.Channels[0]
	for i := range reordered.Channels {
		reordered.Channels[i].UserKey = ""
		reordered.Channels[i].Token = ""
	}
	if err := s.UpdateSettings(SettingsPatch{Captcha: &reordered}); err != nil {
		t.Fatal(err)
	}
	stored := s.Config().Captcha
	if stored.Channels[0].UserKey != "private-paid" || stored.Channels[1].Token != "private-local" {
		t.Fatal("reordering changed credential ownership")
	}
	if reordered.Channels[0].UserKey != "" {
		t.Fatal("patch input was mutated")
	}
	reordered.Channels[1].Endpoint = "https://other.example/pass_nine"
	if err := s.UpdateSettings(SettingsPatch{Captcha: &reordered}); err != nil {
		t.Fatal(err)
	}
	if s.Config().Captcha.Channels[1].Token != "" {
		t.Fatal("old token moved to a new endpoint")
	}
	reordered.Channels[1].Token = "new-private-token"
	if err := s.UpdateSettings(SettingsPatch{Captcha: &reordered}); err != nil {
		t.Fatal(err)
	}
	reordered.Channels[1].Token = ""
	reordered.Channels[1].ClearToken = true
	if err := s.UpdateSettings(SettingsPatch{Captcha: &reordered}); err != nil {
		t.Fatal(err)
	}
	if got := s.Config().Captcha.Channels[1]; got.Token != "" || got.ClearToken {
		t.Fatal("explicit removal did not clear token")
	}
	if err := s.AddLog("bbs", "private-paid"); err != nil {
		t.Fatal(err)
	}
	if got := s.RedactedLogsForUser("", true); len(got) != 1 || got[0].Message == "private-paid" {
		t.Fatal("captcha key not redacted")
	}
}

func TestCaptchaSettingsRejectInvalidDraftAndDuplicateIDsWithoutMutation(t *testing.T) {
	s, err := New(filepath.Join(t.TempDir(), "state.json"))
	if err != nil {
		t.Fatal(err)
	}
	channel := model.CaptchaChannel{ID: "same", Provider: "custom", Enabled: true, Endpoint: "http://captcha:9645/pass_nine", Timeout: 60}
	config := model.CaptchaConfig{Channels: []model.CaptchaChannel{channel, channel}}
	if err := s.UpdateSettings(SettingsPatch{Captcha: &config}); err == nil {
		t.Fatal("duplicate captcha ID accepted")
	}
	config.Channels = config.Channels[:1]
	config.Channels[0].Endpoint = "https://example.com/?key=secret"
	if err := s.UpdateSettings(SettingsPatch{Captcha: &config}); err == nil {
		t.Fatal("query credentials accepted")
	}
	if s.Config().Captcha.Channels[0].Provider != "damagou" {
		t.Fatal("failed validation changed settings")
	}
}
