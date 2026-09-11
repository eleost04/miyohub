package api

import (
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"reflect"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/eleost04/miyohub/internal/model"
	"github.com/eleost04/miyohub/internal/store"
)

func TestLegacyGlobalPushCannotLeakIntoPersonalChannels(t *testing.T) {
	var sends atomic.Int32
	receiver := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		sends.Add(1)
		w.WriteHeader(http.StatusNoContent)
	}))
	defer receiver.Close()
	s, err := store.New(filepath.Join(t.TempDir(), "state.json"))
	if err != nil {
		t.Fatal(err)
	}
	_, adminToken, err := s.CreateAdmin("admin", "password123")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.CreateUser("ordinary", "password123", "user"); err != nil {
		t.Fatal(err)
	}
	_, userToken, err := s.Authenticate("ordinary", "password123")
	if err != nil {
		t.Fatal(err)
	}
	cfg := s.Config()
	cfg.Push.Channels = []model.PushChannel{{Provider: "webhook", Enabled: true, Webhook: receiver.URL}}
	if err := s.ReplaceConfig(cfg); err != nil {
		t.Fatal(err)
	}
	before := s.Config().Push
	server := NewServer(s)
	defer server.Stop()
	h := server.Handler()
	for _, test := range []struct {
		token, method, path string
		want                int
	}{
		{"", "POST", "/api/v1/push/test", 401},
		{userToken, "POST", "/api/v1/push/test", 400},
		{adminToken, "GET", "/api/v1/push/test", 405},
		{adminToken, "POST", "/api/v1/push/test", 400},
		{adminToken, "PUT", "/api/v1/push/config", 409},
		{adminToken, "GET", "/api/v1/push/config", 200},
		{adminToken, "PUT", "/api/v1/config", 200},
	} {
		req := httptest.NewRequest(test.method, test.path, strings.NewReader(`{"push":{"enable":true,"channels":[]},"webhook":"`+receiver.URL+`"}`))
		secureTestRequest(req)
		if test.token != "" {
			req.AddCookie(&http.Cookie{Name: "miyohub_session", Value: test.token})
		}
		res := httptest.NewRecorder()
		h.ServeHTTP(res, req)
		if res.Code != test.want {
			t.Errorf("%s %s: got %d want %d: %s", test.method, test.path, res.Code, test.want, res.Body.String())
		}
	}
	if sends.Load() != 0 {
		t.Fatal("deferred push reached a receiver")
	}
	if !reflect.DeepEqual(before, s.Config().Push) {
		t.Fatal("deferred push configuration was changed")
	}
}
