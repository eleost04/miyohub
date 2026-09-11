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
)

func TestParseAigisVersionsAndPayloadShapes(t *testing.T) {
	for _, tc := range []struct {
		name, raw string
		version   int
	}{
		{"v3-object", `{"session_id":"risk-session","data":{"gt":"widget","challenge":"challenge","new_captcha":false}}`, 3},
		{"v3-encoded", `{"session_id":"risk-session","data":"{\"gt\":\"widget\",\"challenge\":\"challenge\"}"}`, 3},
		{"v3-mmt", `{"session_id":"risk-session","mmt_data":{"gt":"widget","challenge":"challenge"}}`, 3},
		{"v4-gt", `{"session_id":"risk-session","data":{"gt":"widget","risk_type":"slide"}}`, 4},
		{"v4-captcha-id", `{"session_id":"risk-session","data":{"captcha_id":"widget"}}`, 4},
		{"v4-encoded", `{"session_id":"risk-session","data":"{\"gt\":\"widget\",\"risk_type\":\"nine\"}"}`, 4},
		{"v4-nested-mmt", `{"session_id":"risk-session","data":{"mmt_data":{"gt":"widget","risk_type":"slide"}}}`, 4},
		{"missing-session", `{"data":{"gt":"widget","challenge":"challenge"}}`, 0},
		{"missing-widget", `{"session_id":"risk-session","data":{"risk_type":"slide"}}`, 0},
		{"unsafe-session", `{"session_id":"risk;session","data":{"gt":"widget"}}`, 0},
		{"unsafe-risk", `{"session_id":"risk-session","data":{"gt":"widget","risk_type":"<script>"}}`, 0},
		{"broken-json", `{`, 0},
	} {
		t.Run(tc.name, func(t *testing.T) {
			v, err := parseAigis(tc.raw)
			if tc.version == 0 {
				if err == nil {
					t.Fatal("malformed challenge was accepted")
				}
				return
			}
			if err != nil || v.Version != tc.version || v.GT != "widget" || v.SessionID != "risk-session" {
				t.Fatal("incorrect AIGIS variant", v.Version, err)
			}
			if tc.name == "v3-object" && v.NewCaptcha {
				t.Fatal("new_captcha flag was lost")
			}
		})
	}
}

func v4TestProof(id string) SMSCaptchaSolution {
	return SMSCaptchaSolution{ID: id, CaptchaID: "v4-widget", LotNumber: "fixture-lot", CaptchaOutput: "fixture_output+/==", PassToken: "fixture-pass", GenTime: "1700000000"}
}

func TestSMSV4ManualAndAutomaticModesUseOwnerBoundBrowserProof(t *testing.T) {
	for _, mode := range []string{"manual", "auto"} {
		t.Run(mode, func(t *testing.T) {
			m, u := smsFixture(t)
			sends := 0
			var original map[string]any
			m.client.HTTP.Transport = transport(func(r *http.Request) (*http.Response, error) {
				if r.URL.Path != smsSendPath {
					t.Error("V4 flow must not call a V3 solver or unrelated endpoint")
					return nil, errors.New("unexpected endpoint")
				}
				sends++
				var body map[string]any
				_ = json.NewDecoder(r.Body).Decode(&body)
				if sends == 1 {
					original = body
					response := authReply(`{"retcode":-3101,"message":"verification required"}`)
					response.Header.Set("x-rpc-aigis", `{"session_id":"upstream-risk-session","data":"{\"gt\":\"v4-widget\",\"risk_type\":\"slide\"}"}`)
					return response, nil
				}
				if !reflect.DeepEqual(body, original) {
					t.Error("the proof changed the original SMS request")
				}
				prefix, encoded, _ := strings.Cut(r.Header.Get("x-rpc-aigis"), ";")
				raw, _ := base64.StdEncoding.DecodeString(encoded)
				var proof map[string]string
				_ = json.Unmarshal(raw, &proof)
				if prefix != "upstream-risk-session" || len(proof) != 5 || proof["captcha_id"] != "v4-widget" || proof["lot_number"] != "fixture-lot" || proof["captcha_output"] != "fixture_output+/==" || proof["pass_token"] != "fixture-pass" || proof["gen_time"] != "1700000000" {
					t.Error("V4 proof was not encoded using the server-owned session")
				}
				return authReply(`{"retcode":0,"data":{"action_type":"login"}}`), nil
			})
			state, err := m.Send(t.Context(), u.ID, "13800138000", "V4 fixture", "", mode)
			if err != nil || state.Status != "captcha_required" || state.Challenge == nil || state.Challenge.Version != 4 || state.Challenge.RiskType != "slide" || state.Challenge.SessionID != "upstream-risk-session" || sends != 1 {
				t.Fatal("V4 did not reach the interactive flow", state.Status, err)
			}
			proof := v4TestProof(state.Challenge.ID)
			if _, err := m.CompleteCaptcha(t.Context(), "another-owner", proof); err == nil || sends != 1 {
				t.Fatal("cross-user proof accepted")
			}
			wrong := proof
			wrong.CaptchaID = "another-widget"
			if _, err := m.CompleteCaptcha(t.Context(), u.ID, wrong); err == nil || sends != 1 || m.State(u.ID).Challenge == nil {
				t.Fatal("wrong widget accepted or invalid proof consumed the pending request")
			}
			if _, err := m.CompleteCaptcha(t.Context(), u.ID, SMSCaptchaSolution{ID: proof.ID, Challenge: "v3", Validate: "answer"}); err == nil || sends != 1 {
				t.Fatal("V3 proof accepted for V4")
			}
			state, err = m.CompleteCaptcha(t.Context(), u.ID, proof)
			if err != nil || state.Status != "sent" || sends != 2 {
				t.Fatal("V4 could not resume SMS delivery", state.Status, err)
			}
			if _, err := m.CompleteCaptcha(t.Context(), u.ID, proof); err == nil || sends != 2 {
				t.Fatal("V4 proof replayed")
			}
		})
	}
}

func TestSMSV4ProofBoundsAndSolverIsolation(t *testing.T) {
	proof := v4TestProof("local-id")
	for _, mutate := range []func(*SMSCaptchaSolution){
		func(p *SMSCaptchaSolution) { p.CaptchaOutput = strings.Repeat("x", 8193) },
		func(p *SMSCaptchaSolution) { p.CaptchaOutput = "proof\nheader" },
		func(p *SMSCaptchaSolution) { p.PassToken = "" },
		func(p *SMSCaptchaSolution) { p.GenTime = "not-a-time" },
	} {
		invalid := proof
		mutate(&invalid)
		if _, err := smsCaptchaAnswer(SMSChallenge{Version: 4, GT: proof.CaptchaID}, invalid); err == nil {
			t.Fatal("malformed V4 proof accepted")
		}
	}
	m, _ := smsFixture(t)
	m.client.HTTP.Transport = transport(func(*http.Request) (*http.Response, error) { t.Fatal("V4 reached automatic solver"); return nil, nil })
	if _, err := solveAigis(context.Background(), m.client.HTTP, m.store.Config().Captcha, `{"session_id":"risk-session","data":{"gt":"v4-widget"}}`); err == nil {
		t.Fatal("V4 was treated as a V3 automatic solve")
	}
}

func TestSMSV4CanResumeLoginVerification(t *testing.T) {
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
				response.Header.Set("x-rpc-aigis", `{"session_id":"verify-risk","data":{"captcha_id":"v4-widget","risk_type":"nine"}}`)
				return response, nil
			}
			var body map[string]any
			_ = json.NewDecoder(r.Body).Decode(&body)
			if body["captcha"] != "123456" || body["action_type"] != "login" {
				t.Error("resumption lost the original SMS code")
			}
			return authReply(`{"retcode":0,"data":{"user_info":{"aid":"100","mid":"mid"},"token":{"token":"st"}}}`), nil
		case ltokenPath, cookieTokenPath:
			return authReply(`{"retcode":0,"data":{"ltoken":"lt","cookie_token":"ct"}}`), nil
		}
		return nil, errors.New("unexpected endpoint")
	})
	if _, err := m.Send(t.Context(), u.ID, "13800138000", "V4 fixture", ""); err != nil {
		t.Fatal(err)
	}
	if err := m.Verify(t.Context(), u.ID, "123456"); !errors.Is(err, ErrSMSCaptchaRequired) {
		t.Fatal("login verification did not expose V4", err)
	}
	pending := m.State(u.ID)
	if pending.Challenge == nil || pending.Challenge.Version != 4 || pending.Challenge.Operation != "verify" {
		t.Fatal("wrong pending V4 operation")
	}
	state, err := m.CompleteCaptcha(t.Context(), u.ID, v4TestProof(pending.Challenge.ID))
	if err != nil || state.Status != "verified" || verifies != 2 || len(m.store.AccountsForUser(u.ID, false)) != 1 {
		t.Fatal("V4 login did not bind exactly one owned account", state.Status, err)
	}
}
