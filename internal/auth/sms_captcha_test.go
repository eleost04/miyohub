package auth

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"net/http"
	"reflect"
	"strings"
	"testing"
	"time"
)

func TestSMSManualCaptchaIsOwnerBoundSingleUseAndDoesNotCallSolver(t *testing.T) {
	m, u := smsFixture(t)
	sends := 0
	var original map[string]any
	m.client.HTTP.Transport = transport(func(r *http.Request) (*http.Response, error) {
		if r.URL.Path != smsSendPath {
			t.Error("manual flow called a solver or another endpoint")
			return nil, errors.New("unexpected endpoint")
		}
		sends++
		var body map[string]any
		_ = json.NewDecoder(r.Body).Decode(&body)
		if sends == 1 {
			original = body
			response := authReply(`{"retcode":-3101,"message":"verification required"}`)
			response.Header.Set("x-rpc-aigis", `{"session_id":"PRIVATE_AIGIS_SESSION","data":{"gt":"gt","challenge":"challenge"}}`)
			return response, nil
		}
		if !reflect.DeepEqual(body, original) {
			t.Error("manual solution changed the original request")
		}
		prefix, encoded, _ := strings.Cut(r.Header.Get("x-rpc-aigis"), ";")
		raw, _ := base64.StdEncoding.DecodeString(encoded)
		var solution map[string]string
		_ = json.Unmarshal(raw, &solution)
		if prefix != "PRIVATE_AIGIS_SESSION" || solution["geetest_validate"] != "validated" || solution["geetest_seccode"] != "validated|jordan" {
			t.Error("invalid server-owned AIGIS answer")
		}
		return authReply(`{"retcode":0,"data":{"action_type":"login"}}`), nil
	})
	state, err := m.Send(context.Background(), u.ID, "13800138000", "测试账号", "")
	if err != nil || state.Status != "captcha_required" || state.Challenge == nil || sends != 1 {
		t.Fatal("manual challenge not returned", err, state.Status)
	}
	raw, _ := json.Marshal(state)
	if strings.Contains(string(raw), "PRIVATE_AIGIS_SESSION") || strings.Contains(string(raw), "13800138000") || strings.Contains(state.Message, "已发送") {
		t.Fatal("leaked private state or claimed delivery")
	}
	if _, err := m.Send(context.Background(), u.ID, "13800138000", "测试账号", ""); err != nil || sends != 1 {
		t.Fatal("duplicate request contacted upstream")
	}
	answer := SMSCaptchaSolution{ID: state.Challenge.ID, Challenge: "challenge", Validate: "validated"}
	if _, err := m.CompleteCaptcha(context.Background(), "different-user", answer); err == nil || sends != 1 {
		t.Fatal("cross-user challenge accepted")
	}
	wrong := answer
	wrong.ID = "wrong"
	if _, err := m.CompleteCaptcha(context.Background(), u.ID, wrong); err == nil || sends != 1 {
		t.Fatal("wrong challenge ID accepted")
	}
	state, err = m.CompleteCaptcha(context.Background(), u.ID, answer)
	if err != nil || state.Status != "sent" || state.Challenge != nil || sends != 2 {
		t.Fatal("could not resume SMS send", state.Status, err)
	}
	if _, err := m.CompleteCaptcha(context.Background(), u.ID, answer); err == nil || sends != 2 {
		t.Fatal("solution replayed")
	}
}

func TestSMSManualCaptchaExpiresAndCancelDiscardsPendingRequest(t *testing.T) {
	m, u := smsFixture(t)
	challenge := SMSChallenge{ID: "owned", GT: "gt", Challenge: "challenge", ExpiresAt: time.Now().Add(-time.Second)}
	m.sessions[u.ID] = &smsSession{state: SMSState{ExpiresAt: time.Now().Add(time.Minute)}, pending: &smsPending{public: challenge}}
	answer := SMSCaptchaSolution{ID: "owned", Challenge: "challenge", Validate: "answer"}
	if _, err := m.CompleteCaptcha(context.Background(), u.ID, answer); err == nil {
		t.Fatal("expired challenge accepted")
	}
	if m.State(u.ID).Challenge != nil {
		t.Fatal("expired challenge retained")
	}
	m.Cancel(u.ID)
	if _, exists := m.sessions[u.ID]; exists {
		t.Fatal("cancel retained phone or pending SMS code")
	}
}

func TestSMSFailedSendCooldownNeverClaimsDelivery(t *testing.T) {
	m, u := smsFixture(t)
	calls := 0
	m.client.HTTP.Transport = transport(func(r *http.Request) (*http.Response, error) { calls++; return nil, errors.New("upstream offline") })
	state, err := m.Send(context.Background(), u.ID, "13800138000", "测试账号", "")
	if err == nil || state.Status != "failed" || strings.Contains(state.Message, "已发送") {
		t.Fatal("incorrect failure state")
	}
	_, err = m.Send(context.Background(), u.ID, "13800138000", "测试账号", "")
	if err == nil || strings.Contains(err.Error(), "已发送") || calls != 1 {
		t.Fatal("misleading cooldown", err, calls)
	}
}

func TestSMSVerificationCanResumeAfterManualCaptcha(t *testing.T) {
	m, u := smsFixture(t)
	verifies := 0
	m.client.HTTP.Transport = transport(func(r *http.Request) (*http.Response, error) {
		switch r.URL.Path {
		case smsSendPath:
			return authReply(`{"retcode":0,"data":{"action_type":"login"}}`), nil
		case smsVerifyPath:
			verifies++
			if verifies == 1 {
				response := authReply(`{"retcode":-3101}`)
				response.Header.Set("x-rpc-aigis", `{"session_id":"verify-session","data":{"gt":"gt","challenge":"challenge"}}`)
				return response, nil
			}
			var body map[string]any
			_ = json.NewDecoder(r.Body).Decode(&body)
			if body["captcha"] != "123456" || body["action_type"] != "login" {
				t.Error("resume lost original SMS code")
			}
			return authReply(`{"retcode":0,"data":{"user_info":{"aid":"100","mid":"mid"},"token":{"token":"st"}}}`), nil
		case ltokenPath, cookieTokenPath:
			return authReply(`{"retcode":0,"data":{"ltoken":"lt","cookie_token":"ct"}}`), nil
		}
		return nil, errors.New("unexpected endpoint")
	})
	if _, err := m.Send(context.Background(), u.ID, "13800138000", "测试账号", ""); err != nil {
		t.Fatal(err)
	}
	if err := m.Verify(context.Background(), u.ID, "123456"); !errors.Is(err, ErrSMSCaptchaRequired) {
		t.Fatal("missing verify challenge", err)
	}
	pending := m.State(u.ID)
	if pending.Challenge == nil || pending.Challenge.Operation != "verify" {
		t.Fatal("wrong challenge operation")
	}
	state, err := m.CompleteCaptcha(context.Background(), u.ID, SMSCaptchaSolution{ID: pending.Challenge.ID, Challenge: "challenge", Validate: "validated"})
	if err != nil || state.Status != "verified" || len(m.store.AccountsForUser(u.ID, false)) != 1 || verifies != 2 {
		t.Fatal("verify resume failed", err, state.Status)
	}
}
