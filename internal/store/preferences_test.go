package store

import (
	"errors"
	"path/filepath"
	"reflect"
	"testing"
	"time"

	"github.com/eleost04/miyohub/internal/model"
)

func preferenceFixture(t *testing.T) (*Store, model.User, model.User, model.User) {
	t.Helper()
	s, err := New(filepath.Join(t.TempDir(), "state.json"))
	if err != nil {
		t.Fatal(err)
	}
	admin, _, err := s.CreateAdmin("admin", "test-password")
	if err != nil {
		t.Fatal(err)
	}
	a, err := s.CreateUser("alice", "test-password", "user")
	if err != nil {
		t.Fatal(err)
	}
	b, err := s.CreateUser("bob", "test-password", "user")
	if err != nil {
		t.Fatal(err)
	}
	for _, u := range []model.User{a, b} {
		if err := s.AddAccountForUser(u.ID, model.Account{Name: "own", Cookie: "test-only"}); err != nil {
			t.Fatal(err)
		}
	}
	return s, admin, a, b
}

func TestTaskPreferencesMigrateOnceRemainIsolatedAndSurviveRebinding(t *testing.T) {
	s, _, alice, bob := preferenceFixture(t)
	legacy := s.Config()
	legacy.Features = model.Features{BBSTasks: true}
	legacy.Games.Enabled = []string{"honkai2"}
	for i := range legacy.Accounts {
		legacy.Accounts[i].TaskSettings = nil
	}
	if err := s.ReplaceConfig(legacy); err != nil {
		t.Fatal(err)
	}
	a, b := s.AccountsForUser(alice.ID, false)[0], s.AccountsForUser(bob.ID, false)[0]
	if !a.TaskSettings.Automatic || a.TaskSettings.Revision != 1 || !reflect.DeepEqual(a.TaskSettings.Features, legacy.Features) {
		t.Fatal("legacy task choices were lost")
	}
	p := clone(*a.TaskSettings)
	p.Automatic = false
	p.Features = model.Features{GameCheckin: true}
	p.Games.Enabled = []string{"zzz"}
	p.Games.Blacklist = map[string][]string{"zzz": {"12345"}}
	if _, err := s.UpdateAccountTasks(bob, a.ID, p); err == nil {
		t.Fatal("foreign user edited tasks")
	}
	updated, err := s.UpdateAccountTasks(alice, a.ID, p)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.UpdateAccountTasks(alice, a.ID, p); err == nil {
		t.Fatal("stale task editor overwrote newer choices")
	}
	if !reflect.DeepEqual(s.AccountsForUser(bob.ID, false)[0].TaskSettings, b.TaskSettings) {
		t.Fatal("one account changed another's rules")
	}
	cfg, ok := s.ConfigForAccount(a.ID)
	if !ok || !cfg.Features.GameCheckin || cfg.Features.BBSTasks || !reflect.DeepEqual(cfg.Games.Enabled, []string{"zzz"}) {
		t.Fatal("runner still uses global task rules")
	}
	if err := s.BindAccountForUser(alice.ID, a.ID, model.Account{Name: "replacement", Cookie: "updated-test-only"}); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(s.AccountsForUser(alice.ID, false)[0].TaskSettings, updated.TaskSettings) {
		t.Fatal("rebinding reset task preferences")
	}
	reopened, err := New(s.path)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(reopened.AccountsForUser(alice.ID, false)[0].TaskSettings, updated.TaskSettings) {
		t.Fatal("restart reset preferences")
	}
	invalid := clone(*updated.TaskSettings)
	invalid.BBS.DelaySeconds = []int{5, 1}
	if _, err := s.UpdateAccountTasks(alice, a.ID, invalid); err == nil {
		t.Fatal("invalid task interval accepted")
	}
	invalid = clone(*updated.TaskSettings)
	invalid.Games.Blacklist["zzz"] = []string{"not-a-uid"}
	if _, err := s.UpdateAccountTasks(alice, a.ID, invalid); err == nil {
		t.Fatal("invalid role exclusion accepted")
	}
}

func TestCaptchaOwnershipPermissionsAndRuntimeRevocation(t *testing.T) {
	s, admin, alice, bob := preferenceFixture(t)
	site := model.CaptchaConfig{MaxRetries: 3, Channels: []model.CaptchaChannel{{ID: "site", Provider: "custom", Enabled: true, Endpoint: "http://captcha:9645/pass_nine", Token: "site-private", Timeout: 60}}}
	if err := s.UpdateSettings(SettingsPatch{Captcha: &site}); err != nil {
		t.Fatal(err)
	}
	p := s.CaptchaSettingsForUser(alice.ID).UserCaptchaConfig
	if p.Source != "off" || s.CaptchaSettingsForUser(alice.ID).SiteAllowed || alice.CanExchange() {
		t.Fatal("ordinary user inherited a service grant")
	}
	p.Source = "site"
	if err := s.UpdateUserCaptcha(alice.ID, p); !errors.Is(err, ErrSiteCaptchaPermission) {
		t.Fatal("ungranted site service accepted", err)
	}
	p.Source = "personal"
	p.Channels = []model.CaptchaChannel{{ID: "own", Provider: "custom", Enabled: true, Endpoint: "https://solver.example/pass_nine", Token: "alice-private", Timeout: 60}}
	if err := s.UpdateUserCaptcha(alice.ID, p); err != nil {
		t.Fatal(err)
	}
	view := s.CaptchaSettingsForUser(alice.ID)
	if view.Channels[0].Token != "" || len(view.Channels[0].Configured) != 1 || len(s.CaptchaSettingsForUser(bob.ID).Channels) != 0 {
		t.Fatal("personal captcha credentials leaked")
	}
	personal := s.CaptchaForUser(alice.ID)
	if !personal.PublicOnly || !personal.Allowed() || len(personal.Channels) != 1 || personal.Channels[0].Token != "alice-private" {
		t.Fatal("personal solver received the wrong configuration")
	}
	if err := s.UpdateUserPermissions(alice, alice.ID, model.UserPermissions{SiteCaptcha: true}); err == nil {
		t.Fatal("self-granted site service")
	}
	if err := s.UpdateUserPermissions(admin, alice.ID, model.UserPermissions{SiteCaptcha: true}); err != nil {
		t.Fatal(err)
	}
	if s.UserCanExchange(alice.ID) {
		t.Fatal("site captcha grant enabled exchange")
	}
	p = view.UserCaptchaConfig
	p.Source = "site"
	if err := s.UpdateUserCaptcha(alice.ID, p); err != nil {
		t.Fatal(err)
	}
	snapshot := s.CaptchaForUser(alice.ID)
	if snapshot.PublicOnly || !snapshot.Allowed() || snapshot.Channels[0].ID != "site" {
		t.Fatal("site mode did not select the authorized service")
	}
	if personal.Allowed() {
		t.Fatal("old personal config remained usable after changing source")
	}
	if err := s.UpdateUserPermissions(admin, alice.ID, model.UserPermissions{}); err != nil {
		t.Fatal(err)
	}
	if snapshot.Allowed() || len(s.CaptchaForUser(alice.ID).Channels) != 0 {
		t.Fatal("revoked site access remained usable")
	}
	for _, endpoint := range []string{"http://captcha:9645/pass_nine", "http://127.0.0.1/pass_nine", "http://169.254.169.254/latest/meta-data"} {
		p = s.CaptchaSettingsForUser(bob.ID).UserCaptchaConfig
		p.Source = "personal"
		p.Channels = []model.CaptchaChannel{{ID: "site", Provider: "custom", Enabled: true, Endpoint: endpoint, Timeout: 60}}
		if err := s.UpdateUserCaptcha(bob.ID, p); err == nil {
			t.Fatal("personal configuration bypassed the site grant")
		}
	}
	adminRuntime := s.CaptchaForUser(admin.ID)
	if !adminRuntime.Allowed() || len(adminRuntime.Channels) != 1 {
		t.Fatal("administrator lost the deployed site service")
	}
	site.MaxRetries = 0
	if err := s.UpdateSettings(SettingsPatch{Captcha: &site}); err != nil {
		t.Fatal(err)
	}
	if adminRuntime.Allowed() || len(s.CaptchaForUser(admin.ID).Channels) != 0 {
		t.Fatal("disabled service remained usable")
	}
}

func TestExchangeRequiresOwnerGrantIncludingScheduledClaims(t *testing.T) {
	s, admin, alice, _ := preferenceFixture(t)
	a := s.AccountsForUser(alice.ID, false)[0]
	p := model.ExchangePlan{AccountID: a.ID, GoodsID: "test", Enabled: true, Auto: true, ExchangeAt: time.Now().Add(time.Minute).Unix()}
	if _, err := s.CreateExchangePlan(alice.ID, false, p); !errors.Is(err, ErrExchangePermission) {
		t.Fatal("ordinary user exchanged without grant", err)
	}
	if _, err := s.CreateExchangePlan(admin.ID, true, p); err == nil {
		t.Fatal("admin bypassed account ownership")
	}
	if err := s.UpdateUserPermissions(admin, alice.ID, model.UserPermissions{Exchange: true}); err != nil {
		t.Fatal(err)
	}
	if s.CaptchaSettingsForUser(alice.ID).SiteAllowed {
		t.Fatal("exchange grant enabled site captcha")
	}
	p, err := s.CreateExchangePlan(alice.ID, false, p)
	if err != nil {
		t.Fatal(err)
	}
	if err := s.UpdateUserPermissions(admin, alice.ID, model.UserPermissions{}); err != nil {
		t.Fatal(err)
	}
	if _, _, err := s.ClaimExchangePlanAt("", true, p.ID, true, time.Now(), 3*time.Minute); !errors.Is(err, ErrExchangePermission) {
		t.Fatal("scheduled work bypassed revoked grant", err)
	}
	if _, err := s.UpdateExchangePlan(admin.ID, true, p); err == nil {
		t.Fatal("plan edit bypassed owner grant")
	}
	saved, _ := s.ExchangePlanForUser(alice.ID, false, p.ID)
	if saved.State != "pending" || saved.Attempt != 0 || s.AccountCanExchange(a.ID) {
		t.Fatal("denied claim mutated plan")
	}
	if err := s.DeleteExchangePlan(alice.ID, false, p.ID); err != nil {
		t.Fatal("revoked owner cannot clean up an unexecuted plan", err)
	}
}

func TestUserAccessDialogIsAtomicAndRejectsSelfEscalation(t *testing.T) {
	s, admin, alice, _ := preferenceFixture(t)
	if _, _, err := s.UpdateUserAccess(alice, alice.ID, "admin", "active", model.UserPermissions{Exchange: true}); err == nil {
		t.Fatal("user escalated privileges")
	}
	if _, _, err := s.UpdateUserAccess(admin, alice.ID, "user", "invalid", model.UserPermissions{Exchange: true}); err == nil {
		t.Fatal("invalid access settings accepted")
	}
	if s.UserCanExchange(alice.ID) {
		t.Fatal("partial grants escaped a failed update")
	}
	_, after, err := s.UpdateUserAccess(admin, alice.ID, "user", "active", model.UserPermissions{Exchange: true})
	if err != nil || !after.CanExchange() || after.CanUseSiteCaptcha() {
		t.Fatal("independent access settings not saved", err)
	}
	if _, _, err := s.UpdateUserAccess(admin, admin.ID, "user", "disabled", model.UserPermissions{}); err == nil {
		t.Fatal("operator disabled their own last admin account")
	}
}
