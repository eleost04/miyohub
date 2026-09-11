package api

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/cookiejar"
	"net/http/httptest"
	"net/url"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/eleost04/miyohub/internal/mihoyo"
	"github.com/eleost04/miyohub/internal/model"
	"github.com/eleost04/miyohub/internal/store"
)

type probeTransport func(*http.Request) (*http.Response, error)

func (f probeTransport) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

func TestCaptchaProbeIsAnonymousCustomOnlyAndRateLimited(t *testing.T) {
	s, err := store.New(filepath.Join(t.TempDir(), "state.json"))
	if err != nil {
		t.Fatal(err)
	}
	admin, token, _ := s.CreateAdmin("admin", "test-password")
	_, _ = s.CreateUser("member", "test-password", "user")
	_, memberToken, _ := s.Authenticate("member", "test-password")
	cfg := model.CaptchaConfig{MaxRetries: 2, Channels: []model.CaptchaChannel{
		{ID: "paid", Provider: "damagou", Enabled: true, UserKey: "NEVER_CALL_PAID", Timeout: 10},
		{ID: "own", Provider: "custom", Enabled: true, Endpoint: "https://solver.invalid/pass_nine", Timeout: 10},
	}}
	if err := s.UpdateSettings(store.SettingsPatch{Captcha: &cfg}); err != nil {
		t.Fatal(err)
	}
	server := NewServer(s)
	defer server.Stop()
	jar, _ := cookiejar.New(nil)
	origin, _ := url.Parse(mihoyo.BBSAPI)
	jar.SetCookies(origin, []*http.Cookie{{Name: "account_cookie", Value: "NEVER_SEND_COOKIE"}})
	server.shopClient.HTTP.Jar = jar
	var calls atomic.Int64
	server.shopClient.HTTP.Transport = probeTransport(func(r *http.Request) (*http.Response, error) {
		calls.Add(1)
		if r.Header.Get("Cookie") != "" || r.Header.Get("DS") != "" {
			t.Error("probe attached account credentials or signing")
		}
		body := `{"retcode":0,"data":{"gt":"NEVER_RETURN_GT","challenge":"NEVER_RETURN_CHALLENGE"}}`
		switch r.URL.Path {
		case "/misc/api/createVerification":
			if r.Method != http.MethodGet {
				t.Error("probe attempted a write")
			}
		case "/pass_nine":
			body = `{"data":{"result":"success","validate":"NEVER_RETURN_VALIDATE"}}`
		default:
			t.Error("probe called a paid or account endpoint", r.URL.Path)
		}
		return &http.Response{StatusCode: 200, Header: make(http.Header), Body: io.NopCloser(strings.NewReader(body))}, nil
	})
	h := server.Handler()
	if res := callAPI(h, "POST", "/api/v1/captcha/test", "", `{}`); res.Code != 401 {
		t.Fatal("anonymous caller allowed", res.Code)
	}
	if res := callAPI(h, "POST", "/api/v1/captcha/test", memberToken, `{}`); res.Code != 400 || calls.Load() != 0 {
		t.Fatal("ungranted user reached site service", res.Code, calls.Load())
	}
	if res := callAPI(h, "POST", "/api/v1/captcha/test", memberToken, `{"source":"site"}`); res.Code != 400 || calls.Load() != 0 {
		t.Fatal("probe accepted a client-selected service")
	}
	res := callAPI(h, "POST", "/api/v1/captcha/test", token, `{}`)
	if res.Code != 202 || strings.Contains(res.Body.String(), "NEVER_") {
		t.Fatal("unsafe or failed probe", res.Code, calls.Load())
	}
	probe := waitProbe(t, s, admin.ID)
	if probe.Status != "succeeded" || strings.Contains(probe.Message, "NEVER_") {
		t.Fatal("unexpected result", probe.Status)
	}
	if res := callAPI(h, "POST", "/api/v1/captcha/test", token, `{}`); res.Code != 429 || calls.Load() != 2 {
		t.Fatal("probe rate limit failed")
	}
	activity := s.CaptchaSettingsForUser(admin.ID).Activity
	if len(activity) != 1 || activity[0].Kind != "test" || !activity[0].OK {
		t.Fatal("probe was not distinguished from real task usage")
	}
}

func waitProbe(t *testing.T, s *store.Store, userID string) model.CaptchaProbe {
	t.Helper()
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		if p := s.CaptchaProbeForUser(userID); p != nil && p.Status != "running" {
			return *p
		}
		time.Sleep(time.Millisecond)
	}
	t.Fatal("background probe did not finish")
	return model.CaptchaProbe{}
}

func TestCaptchaProbeSurvivesRequestCancellationAndRecordsEarlyFailure(t *testing.T) {
	s, err := store.New(filepath.Join(t.TempDir(), "state.json"))
	if err != nil {
		t.Fatal(err)
	}
	admin, token, _ := s.CreateAdmin("admin", "test-password")
	member, _ := s.CreateUser("member", "test-password", "user")
	_, memberToken, _ := s.Authenticate("member", "test-password")
	cfg := model.CaptchaConfig{MaxRetries: 1, Channels: []model.CaptchaChannel{{ID: "own", Provider: "custom", Enabled: true, Endpoint: "https://solver.invalid/pass_nine", Timeout: 10}}}
	if err := s.UpdateSettings(store.SettingsPatch{Captcha: &cfg}); err != nil {
		t.Fatal(err)
	}
	server := NewServer(s)
	defer server.Stop()
	gate := make(chan struct{})
	var calls atomic.Int64
	server.shopClient.HTTP.Transport = probeTransport(func(r *http.Request) (*http.Response, error) {
		calls.Add(1)
		select {
		case <-gate:
		case <-r.Context().Done():
			return nil, r.Context().Err()
		}
		return nil, errors.New("upstream unavailable SECRET_GT")
	})
	ctx, cancel := context.WithCancel(context.Background())
	r := httptest.NewRequest("POST", "/api/v1/captcha/test", strings.NewReader(`{}`)).WithContext(ctx)
	r.Header.Set("Content-Type", "application/json")
	r.Header.Set("X-MiyoHub-Request", "1")
	r.AddCookie(&http.Cookie{Name: "miyohub_session", Value: token})
	w := httptest.NewRecorder()
	server.Handler().ServeHTTP(w, r)
	cancel()
	if w.Code != 202 {
		close(gate)
		t.Fatal("request waited or failed", w.Code)
	}
	first := s.CaptchaProbeForUser(admin.ID)
	if repeat := callAPI(server.Handler(), "POST", "/api/v1/captcha/test", token, `{}`); repeat.Code != 202 || s.CaptchaProbeForUser(admin.ID).ID != first.ID {
		close(gate)
		t.Fatal("duplicate job")
	}
	if res := callAPI(server.Handler(), "GET", "/api/v1/captcha/test", memberToken, ""); res.Code != 200 || strings.Contains(res.Body.String(), first.ID) || s.CaptchaProbeForUser(member.ID) != nil {
		close(gate)
		t.Fatal("cross-user probe leak")
	}
	close(gate)
	probe := waitProbe(t, s, admin.ID)
	if probe.Status != "failed" || !strings.Contains(probe.Message, "尚未调用") || strings.Contains(probe.Message, "SECRET_") || calls.Load() != 1 {
		t.Fatal("incorrect early failure", probe.Status, calls.Load())
	}
	logs := s.LogsForUser(admin.ID, false)
	if len(logs) == 0 || !strings.Contains(logs[len(logs)-1].Message, "获取匿名验证码失败") {
		t.Fatal("early failure missing from logs")
	}
}
