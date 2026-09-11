package store

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/eleost04/miyohub/internal/model"
	"github.com/eleost04/miyohub/internal/notify"
)

func pushFixture(t *testing.T) (*Store, model.User, model.User) {
	t.Helper()
	s, err := New(filepath.Join(t.TempDir(), "state.json"))
	if err != nil {
		t.Fatal(err)
	}
	a, _, err := s.CreateAdmin("admin", "test-password")
	if err != nil {
		t.Fatal(err)
	}
	u, err := s.CreateUser("member", "test-password", "user")
	if err != nil {
		t.Fatal(err)
	}
	return s, a, u
}
func pushPatch(view PushSettings) PushSettingsPatch {
	p := PushSettingsPatch{Enabled: view.Enabled, Tasks: view.Tasks, Exchange: view.Exchange, ErrorOnly: view.ErrorOnly, Revision: view.Revision}
	for _, c := range view.Channels {
		p.Channels = append(p.Channels, PushChannelPatch{PushChannel: c.PushChannel})
	}
	return p
}
func configurePush(t *testing.T, s *Store, userID string) PushSettings {
	t.Helper()
	p := pushPatch(s.PushSettings(userID))
	p.Enabled = true
	p.Channels = []PushChannelPatch{{PushChannel: model.PushChannel{Name: "private channel", Provider: "webhook", Enabled: true, Webhook: "https://push.example.test/private-key", Token: "sensitive-push-token"}}}
	v, err := s.UpdatePushSettings(userID, p)
	if err != nil {
		t.Fatal(err)
	}
	return v
}
func TestPersonalPushSecretsIsolationRevisionAndPersistence(t *testing.T) {
	s, admin, user := pushFixture(t)
	v := configurePush(t, s, user.ID)
	if len(s.PushSettings(admin.ID).Channels) != 0 {
		t.Fatal("admin inherited member push configuration")
	}
	raw, _ := json.Marshal(v)
	if strings.Contains(string(raw), "private-key") || strings.Contains(string(raw), "sensitive-push-token") {
		t.Fatal("push secrets echoed")
	}
	p := pushPatch(v)
	p.Channels[0].Name = "renamed"
	v, err := s.UpdatePushSettings(user.ID, p)
	if err != nil {
		t.Fatal(err)
	}
	if s.PushConfigForUser(user.ID).Channels[0].Token != "sensitive-push-token" {
		t.Fatal("redacted save erased secret")
	}
	if _, err = s.UpdatePushSettings(user.ID, p); err != ErrPushConflict {
		t.Fatal("stale revision accepted", err)
	}
	foreign := pushPatch(s.PushSettings(admin.ID))
	foreign.Channels = p.Channels
	if _, err = s.UpdatePushSettings(admin.ID, foreign); err == nil {
		t.Fatal("foreign channel was adopted")
	}
	p = pushPatch(v)
	p.Channels[0].ClearFields = []string{"token"}
	v, err = s.UpdatePushSettings(user.ID, p)
	if err != nil {
		t.Fatal(err)
	}
	if s.PushConfigForUser(user.ID).Channels[0].Token != "" {
		t.Fatal("explicit clear ignored")
	}
	read, err := OpenReadOnly(s.path)
	if err != nil {
		t.Fatal(err)
	}
	if read.PushSettings(user.ID).Revision != v.Revision {
		t.Fatal("configuration did not persist")
	}
	disk, _ := os.ReadFile(s.path)
	if strings.Contains(string(disk), "private-key") {
		t.Fatal("push credential persisted unencrypted")
	}
}
func TestWeixinBindingSurvivesRestartAndCannotResurrectDeletedChannel(t *testing.T) {
	s, _, user := pushFixture(t)
	c := model.PushChannel{Provider: "wechat_claw", Mode: "ilink", BotID: "bot_test", OpenID: "wx_user", Token: "weixin-sensitive-token", APIURL: "https://ilinkai.weixin.qq.com", BindingState: "waiting_message"}
	id, err := s.BindPushChannel(user.ID, "", 1, c)
	if err != nil {
		t.Fatal(err)
	}
	c = s.PushConfigForUser(user.ID).Channels[0]
	if !s.UpdateWeixinSession(user.ID, c, "cursor-secret", "context-secret", "ready", "") {
		t.Fatal("session update failed")
	}
	read, err := New(s.path)
	if err != nil {
		t.Fatal(err)
	}
	current := read.PushConfigForUser(user.ID).Channels[0]
	if current.APIURL != c.APIURL || current.Webhook != "" || current.Mode != "ilink" || current.ContextToken != "context-secret" {
		t.Fatal("iLink state was incorrectly migrated to webhook")
	}
	raw, _ := json.Marshal(read.PushSettings(user.ID))
	for _, secret := range []string{"weixin-sensitive-token", "context-secret", "cursor-secret", c.APIURL} {
		if strings.Contains(string(raw), secret) {
			t.Fatal("binding secrets were exposed")
		}
	}
	p := pushPatch(read.PushSettings(user.ID))
	p.Channels = nil
	if _, err = read.UpdatePushSettings(user.ID, p); err != nil {
		t.Fatal(err)
	}
	if read.UpdateWeixinSession(user.ID, current, "late-cursor", "late-token", "ready", "") {
		t.Fatal("late poll recreated deleted channel")
	}
	if _, err = read.BindPushChannel(user.ID, id, p.Revision, c); err == nil {
		t.Fatal("stale QR overwrote configuration")
	}
}
func TestOutboxIsolationDurabilityAndNoAmbiguousReplay(t *testing.T) {
	s, admin, user := pushFixture(t)
	configurePush(t, s, user.ID)
	if err := s.AddAccountForUser(user.ID, model.Account{Name: "own", Cookie: "test-cookie"}); err != nil {
		t.Fatal(err)
	}
	account := s.AccountsForUser(user.ID, false)[0]
	event := notify.Event{Kind: notify.TaskEventKind, UserID: user.ID, AccountID: account.ID, Title: "task", Message: "safe summary", Success: true}
	if ok, err := s.QueuePushEvent(event); !ok || err != nil {
		t.Fatal(ok, err)
	}
	if len(s.PushHistory(admin.ID)) != 0 {
		t.Fatal("private history leaked")
	}
	read, err := New(s.path)
	if err != nil {
		t.Fatal(err)
	}
	job, c, ok, err := read.ClaimPushDelivery()
	if !ok || err != nil || c.Token != "sensitive-push-token" {
		t.Fatal("persistent pending notification was lost")
	}
	if job.UserID != user.ID {
		t.Fatal("recipient owner mismatch")
	}
	if err = read.RecoverPushDeliveries(); err != nil {
		t.Fatal(err)
	}
	if _, _, ok, err = read.ClaimPushDelivery(); ok || err != nil {
		t.Fatal("ambiguous delivery was replayed")
	}
	if history := read.PushHistory(user.ID); len(history) != 1 || history[0].Status != "unknown" || history[0].Message != "" {
		t.Fatal("unsafe history", history)
	}
	event.UserID = admin.ID
	if ok, _ := read.QueuePushEvent(event); ok {
		t.Fatal("foreign account result reached admin channels")
	}
}
func TestOutboxRespectsFiltersAndConfigurationChanges(t *testing.T) {
	s, _, user := pushFixture(t)
	v := configurePush(t, s, user.ID)
	p := pushPatch(v)
	p.ErrorOnly = true
	v, err := s.UpdatePushSettings(user.ID, p)
	if err != nil {
		t.Fatal(err)
	}
	e := notify.Event{Kind: notify.TaskEventKind, UserID: user.ID, Success: true, Title: "result"}
	if ok, _ := s.QueuePushEvent(e); ok {
		t.Fatal("error-only gate ignored")
	}
	e.Success = false
	if ok, err := s.QueuePushEvent(e); !ok || err != nil {
		t.Fatal(ok, err)
	}
	p = pushPatch(v)
	p.Channels[0].Name = "new configuration"
	if _, err = s.UpdatePushSettings(user.ID, p); err != nil {
		t.Fatal(err)
	}
	if _, _, ok, err := s.ClaimPushDelivery(); ok || err != nil {
		t.Fatal("stale queued destination used")
	}
	if s.PushHistory(user.ID)[0].Status != "skipped" {
		t.Fatal("dropped notification missing visible status")
	}
}

func TestAcceptedPushLogDescribesProviderAcknowledgement(t *testing.T) {
	s, _, user := pushFixture(t)
	configurePush(t, s, user.ID)
	event := notify.Event{Kind: notify.TaskEventKind, UserID: user.ID, Success: true, Title: "task result", Message: "safe summary"}
	if ok, err := s.QueuePushEvent(event); !ok || err != nil {
		t.Fatal(ok, err)
	}
	job, _, ok, err := s.ClaimPushDelivery()
	if !ok || err != nil {
		t.Fatal(ok, err)
	}
	if err := s.FinishPushDelivery(job.ID, notify.Result{OK: true}); err != nil {
		t.Fatal(err)
	}
	history := s.PushHistory(user.ID)
	if len(history) != 1 || history[0].Status != "accepted" {
		t.Fatal("provider acknowledgement lost its distinct delivery status", history)
	}
	var message string
	for _, entry := range s.LogsForUser(user.ID, false) {
		if entry.Component == "push" {
			message = entry.Message
		}
	}
	if !strings.HasSuffix(message, "：推送服务已接收通知") || strings.Contains(message, "已读") || strings.Contains(message, "已送达") {
		t.Fatal("provider response was described as a recipient read/delivery receipt", message)
	}
}

func TestLogRedactionIncludesPersonalPushAndCookieParts(t *testing.T) {
	s, _, user := pushFixture(t)
	configurePush(t, s, user.ID)
	if err := s.AddAccountForUser(user.ID, model.Account{Name: "own", Cookie: "ltoken_v2=private-cookie-part;account_id=100"}); err != nil {
		t.Fatal(err)
	}
	_ = s.AddLogForUser(user.ID, "bbs", "private-cookie-part sensitive-push-token https://push.example.test/private-key")
	raw, _ := json.Marshal(s.RedactedLogsForUser(user.ID, false))
	for _, secret := range []string{"private-cookie-part", "sensitive-push-token", "private-key"} {
		if strings.Contains(string(raw), secret) {
			t.Fatal("diagnostic log leaked secret")
		}
	}
}
