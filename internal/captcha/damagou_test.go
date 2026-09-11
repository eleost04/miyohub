package captcha

import (
	"context"
	"errors"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"
)

type captchaTransport func(*http.Request) (*http.Response, error)

func (f captchaTransport) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

func TestSolverPreservesZeroSuccessAndHandlesProviderFailures(t *testing.T) {
	for _, tc := range []struct {
		name, body string
		want       error
		valid      bool
	}{
		{"solved", `{"status":0,"data":"challenge|validate"}`, nil, true},
		{"balance", `{"status":1,"msg":"余额不足"}`, ErrBalanceInsufficient, false},
		{"invalid", `{"status":0,"data":"missing-separator"}`, nil, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			client := &http.Client{Transport: captchaTransport(func(r *http.Request) (*http.Response, error) {
				if r.URL.Query().Get("success") != "0" || r.URL.Query().Get("userkey") != "test-key" {
					t.Error("solver omitted zero success or configured key")
				}
				return &http.Response{StatusCode: 200, Header: make(http.Header), Body: io.NopCloser(strings.NewReader(tc.body))}, nil
			})}
			zero := 0
			result, err := (Solver{HTTP: client}).solve(context.Background(), "test-key", "gt", "challenge", "", time.Second, &zero)
			if tc.valid && (err != nil || result.Validate != "validate" || result.Challenge != "challenge") {
				t.Fatal("valid solution was lost", result, err)
			}
			if !tc.valid && err == nil || tc.want != nil && !errors.Is(err, tc.want) {
				t.Fatal("provider failure was not surfaced", err)
			}
		})
	}
}
