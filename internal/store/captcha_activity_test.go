package store

import (
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"testing"

	"github.com/eleost04/miyohub/internal/captcha"
	"github.com/eleost04/miyohub/internal/model"
)

type observationTransport func(*http.Request) (*http.Response, error)

func (f observationTransport) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

func TestCaptchaActivityIsPrivateBoundedAndContainsNoPayloads(t *testing.T) {
	s, admin, alice, bob := preferenceFixture(t)
	cfg := model.CaptchaConfig{MaxRetries: 2, Channels: []model.CaptchaChannel{{ID: "site-custom", Provider: "custom", Enabled: true, Endpoint: "https://solver.invalid/pass_nine", Token: "NEVER_SHOW_TOKEN", Timeout: 10}}}
	if err := s.UpdateSettings(SettingsPatch{Captcha: &cfg}); err != nil {
		t.Fatal(err)
	}
	if err := s.UpdateUserPermissions(admin, alice.ID, model.UserPermissions{SiteCaptcha: true}); err != nil {
		t.Fatal(err)
	}
	p := s.CaptchaSettingsForUser(alice.ID).UserCaptchaConfig
	p.Source = "site"
	if err := s.UpdateUserCaptcha(alice.ID, p); err != nil {
		t.Fatal(err)
	}
	client := &http.Client{Transport: observationTransport(func(r *http.Request) (*http.Response, error) {
		if r.Header.Get("Cookie") != "" {
			t.Fatal("solver received account credentials")
		}
		return &http.Response{StatusCode: 200, Body: io.NopCloser(strings.NewReader(`{"data":{"result":"success","validate":"NEVER_SHOW_VALIDATE"}}`)), Header: make(http.Header)}, nil
	})}
	runtime := s.CaptchaForUser(alice.ID)
	if _, err := captcha.SolveConfigured(t.Context(), client, runtime, "NEVER_SHOW_GT", "NEVER_SHOW_CHALLENGE", nil); err != nil {
		t.Fatal(err)
	}
	view := s.CaptchaSettingsForUser(alice.ID)
	if len(view.Activity) != 1 || !view.Activity[0].OK || view.Activity[0].Source != "site" || view.Activity[0].Kind != "task" {
		t.Fatal("missing real solver observation", view.Activity)
	}
	if len(s.CaptchaSettingsForUser(bob.ID).Activity) != 0 || len(s.CaptchaSettingsForUser(admin.ID).Activity) != 0 {
		t.Fatal("activity crossed user ownership")
	}
	raw, _ := json.Marshal(view)
	logs, _ := json.Marshal(s.RedactedLogsForUser(alice.ID, false))
	if strings.Contains(string(raw)+string(logs), "NEVER_SHOW") || strings.Contains(string(raw), "solver.invalid") {
		t.Fatal("captcha activity exposed secret material")
	}
	for i := 0; i < 55; i++ {
		runtime.Observe(model.CaptchaAttempt{Provider: "custom", ChannelID: "site-custom", Kind: "task", Code: "failed"})
	}
	if len(s.CaptchaSettingsForUser(alice.ID).Activity) != 50 {
		t.Fatal("unbounded captcha history")
	}
	if err := s.DeleteUser(alice.ID); err != nil {
		t.Fatal(err)
	}
	if _, ok := s.data.CaptchaActivity[alice.ID]; ok {
		t.Fatal("deleted user's captcha activity remained")
	}
}
