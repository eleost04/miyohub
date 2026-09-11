package api

import (
	"context"
	"encoding/json"
	"net/http"
	"path/filepath"
	"strings"
	"testing"

	"github.com/eleost04/miyohub/internal/model"
	"github.com/eleost04/miyohub/internal/notify"
	"github.com/eleost04/miyohub/internal/store"
)

type testPushSender struct {
	calls    int
	channels []model.PushChannel
	fail     bool
}

func (s *testPushSender) Send(_ context.Context, cfg model.PushConfig, _ string, _ string, _ bool) []notify.Result {
	s.calls++
	s.channels = append(s.channels, cfg.Channels...)
	c := cfg.Channels[0]
	r := notify.Result{ChannelID: c.ID, Provider: c.Provider, Name: c.Name, OK: !s.fail}
	if s.fail {
		r.Error = "模拟渠道拒绝"
	}
	return []notify.Result{r}
}
func TestPrivatePushAPICanSaveTestAndReportFailures(t *testing.T) {
	s, err := store.New(filepath.Join(t.TempDir(), "state.json"))
	if err != nil {
		t.Fatal(err)
	}
	_, adminToken, err := s.CreateAdmin("admin", "test-password")
	if err != nil {
		t.Fatal(err)
	}
	u, err := s.CreateUser("member", "test-password", "user")
	if err != nil {
		t.Fatal(err)
	}
	_, token, err := s.Authenticate("member", "test-password")
	if err != nil {
		t.Fatal(err)
	}
	server := NewServer(s)
	defer server.Stop()
	sender := &testPushSender{}
	server.pushSender = sender
	h := server.Handler()
	body := `{"enable":false,"tasks":true,"exchange":true,"revision":1,"channels":[{"name":"私人通知","provider":"webhook","enable":false,"webhook":"https://receiver.example.test/private-hook","token":"private-api-token"}]}`
	w := callAPI(h, http.MethodPut, "/api/v1/push/config", token, body)
	if w.Code != 200 {
		t.Fatal(w.Code, w.Body.String())
	}
	if strings.Contains(w.Body.String(), "private-api-token") || strings.Contains(w.Body.String(), "private-hook") {
		t.Fatal("API echoed secrets")
	}
	var envelope struct {
		Data store.PushSettings `json:"data"`
	}
	if err = json.Unmarshal(w.Body.Bytes(), &envelope); err != nil {
		t.Fatal(err)
	}
	view := envelope.Data
	payload, _ := json.Marshal(map[string]any{"channel_id": view.Channels[0].ID, "revision": view.Revision})
	w = callAPI(h, http.MethodPost, "/api/v1/push/test", token, string(payload))
	if w.Code != 200 || !strings.Contains(w.Body.String(), `"all_ok":true`) || sender.calls != 1 || sender.channels[0].Token != "private-api-token" {
		t.Fatal("manual test failed", w.Body.String())
	}
	if s.PushConfigForUser(u.ID).Enabled {
		t.Fatal("manual test enabled automation")
	}
	if w = callAPI(h, http.MethodGet, "/api/v1/push/config", adminToken, ""); w.Code != 200 || strings.Contains(w.Body.String(), view.Channels[0].ID) {
		t.Fatal("admin received private channel")
	}
	if w = callAPI(h, http.MethodPost, "/api/v1/push/test", adminToken, string(payload)); w.Code < 400 || sender.calls != 1 {
		t.Fatal("foreign test sent a notification")
	}
	sender.fail = true
	w = callAPI(h, http.MethodPost, "/api/v1/push/test", token, string(payload))
	if w.Code != 200 || !strings.Contains(w.Body.String(), `"all_ok":false`) {
		t.Fatal("business failure reported as success")
	}
	history := s.PushHistory(u.ID)
	if len(history) != 2 || history[0].Status != "failed" || history[1].Status != "accepted" {
		t.Fatal("test history missing", history)
	}
	for _, path := range []string{"/api/v1/push/config", "/api/v1/push/history", "/api/v1/push/qr"} {
		if w = callAPI(h, http.MethodGet, path, "", ""); w.Code != 401 {
			t.Fatal("unauthenticated private endpoint", path, w.Code)
		}
	}
	if w = callAPI(h, http.MethodPost, "/api/v1/push/qr/start", token, `{"provider":"qqbot","channel_id":"foreign-channel","revision":2}`); w.Code != 404 {
		t.Fatal("foreign QR target accepted", w.Code)
	}
	if w = callAPI(h, http.MethodGet, "/api/v1/push/qr/start", token, ""); w.Code != 405 {
		t.Fatal("QR creation via GET accepted")
	}
	if w = callAPI(h, http.MethodPost, "/api/v1/push/qr/cancel", token, `{}`); w.Code != 400 {
		t.Fatal("unscoped QR cancellation accepted")
	}
}
