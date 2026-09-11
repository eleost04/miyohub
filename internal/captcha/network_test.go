package captcha

import (
	"context"
	"errors"
	"net"
	"net/http"
	"net/netip"
	"testing"

	"github.com/eleost04/miyohub/internal/model"
)

func TestPersonalEndpointsRejectInternalAndMetadataNetworks(t *testing.T) {
	for _, raw := range []string{"http://localhost/pass_nine", "http://miyohub-captcha:9645/pass_nine", "http://captcha.internal/pass_nine", "http://127.0.0.1/pass_nine", "http://[::1]/pass_nine", "http://[::ffff:127.0.0.1]/pass_nine", "http://10.0.0.1/pass_nine", "http://172.17.0.1/pass_nine", "http://192.168.1.1/pass_nine", "http://169.254.169.254/latest/meta-data", "http://100.100.100.200/latest/meta-data", "http://[64:ff9b::a00:1]/pass_nine", "http://[2002:7f00:1::]/pass_nine"} {
		channel := model.CaptchaChannel{Provider: "custom", Enabled: true, Timeout: 60, Endpoint: raw}
		if ValidatePersonalChannel(channel) == nil {
			t.Fatal("private personal endpoint accepted", raw)
		}
	}
	if err := ValidatePersonalChannel(model.CaptchaChannel{Provider: "custom", Enabled: true, Timeout: 60, Endpoint: "https://solver.example/pass_nine"}); err != nil {
		t.Fatal(err)
	}
}

func TestPersonalDialValidatesAllAnswersAndPinsTheIP(t *testing.T) {
	for _, address := range []string{"127.0.0.1", "169.254.169.254", "100.64.0.1", "::1", "::ffff:10.0.0.1", "fc00::1", "2001:db8::1"} {
		lookup := func(context.Context, string, string) ([]netip.Addr, error) {
			return []netip.Addr{netip.MustParseAddr("1.1.1.1"), netip.MustParseAddr(address)}, nil
		}
		called := false
		dial := func(context.Context, string, string) (net.Conn, error) {
			called = true
			return nil, errors.New("must not dial")
		}
		if _, err := dialPublic(t.Context(), "tcp", "public-looking.example:443", lookup, dial); !errors.Is(err, ErrPrivateEndpoint) || called {
			t.Fatal("DNS rebinding or mixed answers bypassed network policy")
		}
	}
	lookup := func(context.Context, string, string) ([]netip.Addr, error) {
		return []netip.Addr{netip.MustParseAddr("1.1.1.1")}, nil
	}
	a, b := net.Pipe()
	defer a.Close()
	defer b.Close()
	dial := func(_ context.Context, _, target string) (net.Conn, error) {
		if target != "1.1.1.1:443" {
			t.Fatal("hostname was re-resolved after validation")
		}
		return a, nil
	}
	if _, err := dialPublic(t.Context(), "tcp", "solver.example:443", lookup, dial); err != nil {
		t.Fatal(err)
	}
}

type policyTransport func(*http.Request) (*http.Response, error)

func (f policyTransport) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

func TestSolverRechecksPermissionImmediatelyBeforeTransport(t *testing.T) {
	calls, checks := 0, 0
	client := &http.Client{Transport: policyTransport(func(*http.Request) (*http.Response, error) { calls++; return nil, errors.New("permission bypass") })}
	cfg := model.CaptchaConfig{Channels: []model.CaptchaChannel{{Provider: "custom", Enabled: true, Endpoint: "http://captcha:9645/pass_nine", Timeout: 60}}, Allowed: func() bool { checks++; return checks < 3 }}
	if _, err := SolveConfigured(t.Context(), client, cfg, "test-gt", "test-challenge", nil); err == nil || calls != 0 || checks < 3 {
		t.Fatal("stale permissions reached solver transport")
	}
	cfg.Allowed = nil
	cfg.PublicOnly = true
	if _, err := SolveConfigured(t.Context(), client, cfg, "test-gt", "test-challenge", nil); !errors.Is(err, ErrPrivateEndpoint) || calls != 0 {
		t.Fatal("caller transport bypassed personal network policy")
	}
}
