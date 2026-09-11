package captcha

import (
	"context"
	"errors"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/eleost04/miyohub/internal/model"
)

func customChannel() model.CaptchaChannel {
	return model.CaptchaChannel{ID: "local", Provider: "custom", Enabled: true, Endpoint: "http://miyohub-captcha:9645/pass_nine", Token: "private-token", Timeout: 60}
}

func solverResponse(body string) *http.Response {
	return &http.Response{StatusCode: 200, Header: make(http.Header), Body: io.NopCloser(strings.NewReader(body))}
}

func TestCustomSolverContractAndChallengeFallback(t *testing.T) {
	for _, replacement := range []string{"", ",\"challenge\":\"replacement\""} {
		ch := customChannel()
		client := &http.Client{Transport: captchaTransport(func(r *http.Request) (*http.Response, error) {
			if r.Method != "GET" || r.URL.Path != "/pass_nine" || r.URL.Query().Get("gt") != "gt&encoded" || r.URL.Query().Get("challenge") != "challenge+#" || r.URL.Query().Get("use_v3_model") != "true" {
				t.Error("custom solver contract mismatch")
			}
			if r.Header.Get("Authorization") != "Bearer private-token" || r.Header.Get("Cookie") != "" || len(r.URL.Query()) != 3 {
				t.Error("credentials were leaked or custom authentication was missing")
			}
			return solverResponse(`{"data":{"result":"success","validate":" solved "` + replacement + `}}`), nil
		})}
		solution, err := SolveConfigured(t.Context(), client, model.CaptchaConfig{Channels: []model.CaptchaChannel{ch}}, "gt&encoded", "challenge+#", nil)
		wantChallenge := "challenge+#"
		if replacement != "" {
			wantChallenge = "replacement"
		}
		if err != nil || solution.Validate != "solved" || solution.Challenge != wantChallenge {
			t.Fatalf("custom solution lost: %+v %v", solution, err)
		}
	}
}

func TestCustomSolverRejectsMalformedUnconfirmedAndOversizedResponses(t *testing.T) {
	for _, body := range []string{`not-json-private-token`, `null`, `{}`, `{"data":{"result":"fail","validate":"private-value"}}`, `{"data":{"result":"success","validate":"  "}}`, `{"data":{"result":"success","validate":12}}`, strings.Repeat("x", (1<<20)+1)} {
		client := &http.Client{Transport: captchaTransport(func(*http.Request) (*http.Response, error) { return solverResponse(body), nil })}
		_, err := solveCustom(t.Context(), client, customChannel(), "gt", "challenge")
		if err == nil || strings.Contains(err.Error(), "private-") {
			t.Fatal("unconfirmed response accepted or exposed", err)
		}
	}
}

func TestCaptchaNeverFollowsRedirects(t *testing.T) {
	calls := 0
	client := &http.Client{Transport: captchaTransport(func(r *http.Request) (*http.Response, error) {
		calls++
		return &http.Response{StatusCode: 302, Header: http.Header{"Location": {"https://other.invalid/"}}, Body: io.NopCloser(strings.NewReader("")), Request: r}, nil
	})}
	_, err := solveCustom(t.Context(), client, customChannel(), "gt", "challenge")
	if err == nil || calls != 1 {
		t.Fatal("custom service redirect was followed", calls, err)
	}
	_, err = (Solver{HTTP: client}).Solve(t.Context(), "key", "gt", "challenge", "", time.Second)
	if err == nil || calls != 2 {
		t.Fatal("damagou redirect was followed", calls, err)
	}
}

func TestChannelTimeoutOverridesOrdinaryClientButRespectsCancellation(t *testing.T) {
	client := &http.Client{Timeout: time.Millisecond, Transport: captchaTransport(func(r *http.Request) (*http.Response, error) {
		select {
		case <-r.Context().Done():
			return nil, r.Context().Err()
		case <-time.After(20 * time.Millisecond):
		}
		if r.URL.Host == "api.damagou.top" {
			return solverResponse(`{"status":0,"data":"challenge|validate"}`), nil
		}
		return solverResponse(`{"data":{"result":"success","validate":"validate"}}`), nil
	})}
	ch := customChannel()
	ch.Timeout = 1
	if _, err := solveCustom(t.Context(), client, ch, "gt", "challenge"); err != nil {
		t.Fatal("custom timeout ignored", err)
	}
	if _, err := (Solver{HTTP: client}).Solve(t.Context(), "key", "gt", "challenge", "", time.Second); err != nil {
		t.Fatal("damagou timeout ignored", err)
	}
	ctx, cancel := context.WithTimeout(t.Context(), time.Millisecond)
	defer cancel()
	if _, err := solveCustom(ctx, client, ch, "gt", "challenge"); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatal("parent cancellation ignored", err)
	}
}

func TestConfiguredSolverOrderedFallbackAndDisabledChannels(t *testing.T) {
	calls := []string{}
	client := &http.Client{Transport: captchaTransport(func(r *http.Request) (*http.Response, error) {
		calls = append(calls, r.URL.Host)
		if r.URL.Host == "api.damagou.top" {
			return solverResponse(`{"status":1,"msg":"余额不足"}`), nil
		}
		return solverResponse(`{"data":{"result":"success","validate":"validate"}}`), nil
	})}
	paid := model.CaptchaChannel{Provider: "damagou", Enabled: true, UserKey: "key", Timeout: 60}
	disabled := paid
	disabled.Enabled = false
	channels := []model.CaptchaChannel{disabled, paid, customChannel(), paid}
	if _, err := SolveConfigured(t.Context(), client, model.CaptchaConfig{Channels: channels}, "gt", "challenge", nil); err != nil || len(calls) != 2 {
		t.Fatal("fallback order incorrect", calls, err)
	}
	if _, err := SolveConfigured(t.Context(), client, model.CaptchaConfig{Channels: []model.CaptchaChannel{paid}}, "gt", "challenge", nil); !errors.Is(err, ErrBalanceInsufficient) {
		t.Fatal("balance failure lost", err)
	}
	if _, err := SolveConfigured(t.Context(), client, model.CaptchaConfig{Channels: []model.CaptchaChannel{disabled}}, "gt", "challenge", nil); !errors.Is(err, ErrUnavailable) {
		t.Fatal("disabled provider called", err)
	}
}

func TestCustomChannelValidationAndModelChoice(t *testing.T) {
	ch := customChannel()
	for _, endpoint := range []string{"file:///etc/passwd", "http://name:password@example.com/", "https://example.com/pass_nine?token=secret", "https://example.com/#x", "http://127.0.0.1:0/pass_nine", "http://169.254.169.254/", "http://[::]/"} {
		ch.Endpoint = endpoint
		if err := ValidateChannel(ch); err == nil {
			t.Error("unsafe endpoint accepted")
		}
	}
	ch = customChannel()
	ch.Token = ""
	no := false
	ch.UseV3Model = &no
	client := &http.Client{Transport: captchaTransport(func(r *http.Request) (*http.Response, error) {
		if r.URL.Query().Get("use_v3_model") != "false" || r.Header.Get("Authorization") != "" {
			t.Error("model choice/authentication mismatch")
		}
		return solverResponse(`{"data":{"result":"success","validate":"ok"}}`), nil
	})}
	if _, err := solveCustom(t.Context(), client, ch, "gt", "challenge"); err != nil {
		t.Fatal(err)
	}
}
