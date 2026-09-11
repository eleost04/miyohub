package auth

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/eleost04/miyohub/internal/captcha"
	"github.com/eleost04/miyohub/internal/model"
	"github.com/eleost04/miyohub/internal/store"
)

func authReply(body string) *http.Response {
	return &http.Response{StatusCode: http.StatusOK, Header: make(http.Header), Body: io.NopCloser(strings.NewReader(body))}
}

func smsFixture(t *testing.T) (*SMSManager, model.User) {
	t.Helper()
	s, err := store.New(filepath.Join(t.TempDir(), "state.json"))
	if err != nil {
		t.Fatal(err)
	}
	u, _, err := s.CreateAdmin("admin", "password123")
	if err != nil {
		t.Fatal(err)
	}
	m := NewSMSManager(s)
	t.Cleanup(m.Stop)
	return m, u
}

func TestSMSLoginEncryptsPhoneAndConsumesSession(t *testing.T) {
	m, u := smsFixture(t)
	calls := map[string]int{}
	device := ""
	m.client.HTTP.Transport = transport(func(r *http.Request) (*http.Response, error) {
		calls[r.URL.Path]++
		if device == "" {
			device = r.Header.Get("x-rpc-device_id")
		}
		if device == "" || device != r.Header.Get("x-rpc-device_id") {
			t.Error("SMS and token requests must share a device")
		}
		switch r.URL.Path {
		case smsSendPath, smsVerifyPath:
			if r.Method != http.MethodPost || r.Header.Get("DS") != "" {
				t.Error("incorrect SMS method or headers")
			}
			if r.Header.Get("x-rpc-client_type") != "2" || r.Header.Get("x-rpc-app_version") != "2.106.2" {
				t.Error("SMS headers retained the QR client identity")
			}
			var body map[string]string
			if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
				t.Fatal(err)
			}
			for _, key := range []string{"mobile", "area_code"} {
				raw, err := base64.StdEncoding.DecodeString(body[key])
				if err != nil || len(raw) != 128 || strings.Contains(body[key], "13800138000") {
					t.Errorf("%s is not an RSA-encrypted value", key)
				}
			}
			if r.URL.Path == smsSendPath {
				return authReply(`{"retcode":0,"data":{"action_type":"login","countdown":75}}`), nil
			}
			if body["captcha"] != "123456" || body["action_type"] != "login" {
				t.Error("verification did not use the server-owned action")
			}
			return authReply(`{"retcode":0,"data":{"user_info":{"aid":"100","mid":"mid"},"token":{"token":"st"}}}`), nil
		case ltokenPath, cookieTokenPath:
			if r.URL.Query().Get("stoken") != "st" || !strings.Contains(r.Header.Get("Cookie"), "mid=mid") {
				t.Error("token exchange used the wrong credentials")
			}
			wantClient := "5"
			if r.URL.Path == cookieTokenPath {
				wantClient = "2"
			}
			if r.Header.Get("x-rpc-client_type") != wantClient {
				t.Error("token exchange used the wrong client identity")
			}
			return authReply(`{"retcode":0,"data":{"ltoken":"lt","cookie_token":"ct"}}`), nil
		default:
			t.Errorf("unexpected upstream: %s", r.URL.Path)
			return nil, errors.New("unexpected upstream")
		}
	})
	state, err := m.Send(context.Background(), u.ID, "+8613800138000", "主账号", "")
	if err != nil {
		t.Fatal(err)
	}
	if state.Phone != "138****8000" || time.Until(state.RetryAt) < 74*time.Second || time.Until(state.ExpiresAt) < 9*time.Minute {
		t.Fatalf("incorrect masked phone/countdown: %+v", state)
	}
	if _, err := m.Send(context.Background(), u.ID, "13800138000", "主账号", ""); err == nil {
		t.Fatal("resend cooldown bypassed")
	}
	if _, err := m.Send(context.Background(), "another-user", "13800138000", "另一个账号", ""); err == nil {
		t.Fatal("per-phone cooldown bypassed")
	}
	if err := m.Verify(context.Background(), u.ID, " 123456 "); err != nil {
		t.Fatal(err)
	}
	accounts := m.store.AccountsForUser(u.ID, false)
	if len(accounts) != 1 || accounts[0].Stuid != "100" || accounts[0].Device.ID != device || !strings.Contains(accounts[0].Cookie, "cookie_token=ct") {
		t.Fatal("verified account was not saved correctly")
	}
	if err := m.Verify(context.Background(), u.ID, "123456"); err == nil || calls[smsVerifyPath] != 1 || calls[smsSendPath] != 1 {
		t.Fatal("SMS session was reused")
	}
}

func TestSMSCancellationStopsInFlightLogin(t *testing.T) {
	m, u := smsFixture(t)
	started := make(chan struct{})
	m.client.HTTP.Transport = transport(func(r *http.Request) (*http.Response, error) {
		if r.URL.Path == smsSendPath {
			return authReply(`{"retcode":0,"data":{"action_type":"login"}}`), nil
		}
		if r.URL.Path == smsVerifyPath {
			close(started)
			<-r.Context().Done()
			return nil, r.Context().Err()
		}
		t.Error("cancelled login attempted token exchange")
		return nil, errors.New("unexpected request")
	})
	if _, err := m.Send(context.Background(), u.ID, "13800138000", "测试账号", ""); err != nil {
		t.Fatal(err)
	}
	done := make(chan error, 1)
	go func() { done <- m.Verify(context.Background(), u.ID, "123456") }()
	select {
	case <-started:
	case <-time.After(2 * time.Second):
		t.Fatal("verification did not start")
	}
	m.Cancel(u.ID)
	select {
	case err := <-done:
		if err == nil {
			t.Fatal("cancelled login succeeded")
		}
	case <-time.After(2 * time.Second):
		t.Fatal("verification did not stop")
	}
	if len(m.store.AccountsForUser(u.ID, false)) != 0 {
		t.Fatal("cancelled login saved credentials")
	}
	if err := m.Verify(context.Background(), u.ID, "123456"); err == nil {
		t.Fatal("cancelled session remained valid")
	}
}

func TestSMSRejectsExpiredAndExhaustedSessions(t *testing.T) {
	m, u := smsFixture(t)
	m.client.HTTP.Transport = transport(func(*http.Request) (*http.Response, error) {
		t.Error("invalid session contacted upstream")
		return nil, errors.New("unexpected request")
	})
	for _, session := range []*smsSession{
		{action: "login", state: SMSState{ExpiresAt: time.Now().Add(-time.Second)}},
		{action: "login", attempts: 5, state: SMSState{ExpiresAt: time.Now().Add(time.Minute)}},
	} {
		m.sessions[u.ID] = session
		if err := m.Verify(context.Background(), u.ID, "123456"); err == nil {
			t.Fatal("invalid session accepted")
		}
	}
}

func TestSMSAigisChallengeUsesConfiguredSolver(t *testing.T) {
	m, u := smsFixture(t)
	cfg := m.store.Config()
	cfg.Captcha = model.CaptchaConfig{MaxRetries: 2, Channels: []model.CaptchaChannel{{Enabled: true, Provider: "damagou", UserKey: "test-key"}}}
	if err := m.store.ReplaceConfig(cfg); err != nil {
		t.Fatal(err)
	}
	sends, solves := 0, 0
	m.client.HTTP.Transport = transport(func(r *http.Request) (*http.Response, error) {
		if r.URL.Host == "api.damagou.top" {
			solves++
			if r.URL.Query().Get("gt") != "gt" || r.URL.Query().Get("challenge") != "challenge" {
				t.Error("incorrect captcha challenge")
			}
			return authReply(`{"status":0,"data":"solved-challenge|validation"}`), nil
		}
		if r.URL.Path != smsSendPath {
			t.Error("unexpected SMS request")
			return nil, errors.New("unexpected request")
		}
		sends++
		if sends == 1 {
			response := authReply(`{"retcode":-3101,"message":"verification required"}`)
			response.Header.Set("x-rpc-aigis", `{"session_id":"session","data":"{\"gt\":\"gt\",\"challenge\":\"challenge\"}"}`)
			return response, nil
		}
		prefix, encoded, ok := strings.Cut(r.Header.Get("x-rpc-aigis"), ";")
		raw, err := base64.StdEncoding.DecodeString(encoded)
		var solution map[string]string
		if !ok || prefix != "session" || err != nil || json.Unmarshal(raw, &solution) != nil || solution["geetest_seccode"] != "validation|jordan" || solution["geetest_challenge"] != "solved-challenge" {
			t.Error("incorrect AIGIS solution")
		}
		return authReply(`{"retcode":0,"data":{"action_type":"login"}}`), nil
	})
	if _, err := m.Send(context.Background(), u.ID, "13800138000", "测试账号", "", "auto"); err != nil || sends != 2 || solves != 1 {
		t.Fatalf("AIGIS flow failed: %v, sends %d, solves %d", err, sends, solves)
	}
	if _, err := solveAigis(context.Background(), m.client.HTTP, model.CaptchaConfig{}, `{"session_id":"s","data":{"gt":"gt","challenge":"challenge"}}`); !errors.Is(err, captcha.ErrUnavailable) {
		t.Fatal("unconfigured solver did not fail clearly", err)
	}
}
