package mihoyo

import (
	"context"
	"encoding/base64"
	"encoding/binary"
	"errors"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/eleost04/miyohub/internal/model"
)

func TestProxyValidationAndFixedDestinationBoundary(t *testing.T) {
	for _, raw := range []string{"http://127.0.0.1:8080", "https://proxy.example:443", "socks5://proxy:1080", "socks5h://[::1]:1080"} {
		if err := ValidateProxy(model.ProxyConfig{Enabled: true, URL: raw}); err != nil {
			t.Fatal("supported proxy rejected", err)
		}
	}
	for _, raw := range []string{"", "file:///tmp/socket", "ftp://proxy.example", "http://proxy:0", "http://proxy:65536", "http://proxy/?key=value", "http://proxy/#fragment", "http://proxy/path", "socks5://private-user:private-password@proxy:1080"} {
		if err := ValidateProxy(model.ProxyConfig{Enabled: true, URL: raw}); err == nil || strings.Contains(err.Error(), "private-password") {
			t.Fatal("malformed proxy accepted or credential echoed")
		}
	}
	for _, host := range []string{"api-takumi.mihoyo.com", "api-takumi.miyoushe.com", "bbs-api.miyoushe.com", "passport-api.mihoyo.com", "act-nap-api.mihoyo.com", "public-data-api.mihoyo.com", "api-cloudgame.mihoyo.com"} {
		u, _ := url.Parse("https://" + host + "/fixture")
		if !officialDestination(u) {
			t.Fatal("official request excluded from proxy", host)
		}
	}
	for _, raw := range []string{"http://api-takumi.mihoyo.com", "https://api-takumi.mihoyo.com:8080", "https://api-takumi.mihoyo.com.attacker.example", "https://127.0.0.1", "https://solver.example/pass_nine", "https://api-takumi.mihoyo.com@other.example"} {
		u, _ := url.Parse(raw)
		if officialDestination(u) {
			t.Fatal("non-official destination inherited the operator proxy")
		}
	}
}

// A TLS fixture sits behind local HTTP CONNECT / SOCKS5 proxies. The server
// certificate is trusted only in this test; production TLS verification is
// unchanged. No query ever leaves the loopback network.
func proxyTLSFixture(t *testing.T) (*httptest.Server, *http.Transport) {
	t.Helper()
	target := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Proxy-Authorization") != "" || r.Header.Get("Cookie") != "fixture-cookie" {
			t.Error("proxy credentials leaked to target or account header lost")
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{"fixture":true}`)
	}))
	t.Cleanup(target.Close)
	base := target.Client().Transport.(*http.Transport).Clone()
	base.TLSClientConfig.ServerName = "example.com" // httptest's local certificate.
	base.DisableKeepAlives = true
	return target, base
}

func tunnelFixture(t *testing.T, client net.Conn, target string) {
	t.Helper()
	defer client.Close()
	remote, err := net.DialTimeout("tcp", strings.TrimPrefix(target, "https://"), time.Second)
	if err != nil {
		t.Error("local target unavailable", err)
		return
	}
	defer remote.Close()
	done := make(chan struct{})
	go func() { _, _ = io.Copy(remote, client); remote.Close(); close(done) }()
	_, _ = io.Copy(client, remote)
	client.Close()
	<-done
}

func TestHTTPProxyConnectAuthenticationAndLiveSettings(t *testing.T) {
	target, base := proxyTLSFixture(t)
	var connections atomic.Int32
	var tunnels sync.WaitGroup
	// This fixture expects one CONNECT. Register it before starting the server:
	// receiving a response over TCP does not synchronize Add with Wait in Go.
	tunnels.Add(1)
	proxy := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if connections.Add(1) != 1 {
			t.Error("unexpected additional CONNECT request")
			w.WriteHeader(http.StatusBadGateway)
			return
		}
		defer tunnels.Done()
		if r.Method != http.MethodConnect || r.Host != "api-takumi.mihoyo.com:443" || r.Header.Get("Cookie") != "" || r.Header.Get("Proxy-Authorization") != "Basic "+base64.StdEncoding.EncodeToString([]byte("fixture-user:fixture-password")) {
			t.Error("invalid CONNECT or proxy auth")
			w.WriteHeader(http.StatusProxyAuthRequired)
			return
		}
		conn, rw, err := w.(http.Hijacker).Hijack()
		if err != nil {
			t.Error(err)
			return
		}
		_, _ = rw.WriteString("HTTP/1.1 200 Connection Established\r\n\r\n")
		_ = rw.Flush()
		tunnelFixture(t, conn, target.URL)
	}))
	defer proxy.Close()
	cfg := model.NetworkConfig{Proxy: model.ProxyConfig{Enabled: true, URL: proxy.URL, Username: "fixture-user", Password: "fixture-password"}}
	transport := newUpstreamTransport(base, func() model.NetworkConfig { return cfg })
	defer transport.CloseIdleConnections()
	client := NewClient("")
	client.HTTP.Transport = transport
	var result map[string]any
	if err := client.JSON(t.Context(), "GET", TakumiAPI+"/fixture", nil, nil, http.Header{"Cookie": {"fixture-cookie"}}, &result); err != nil || result["fixture"] != true {
		t.Fatal("CONNECT proxy failed", err)
	}
	tunnels.Wait()
	if connections.Load() != 1 {
		t.Fatal("unexpected retry")
	}
	// The existing client must pick up disabled proxy settings, without changing
	// a transport concurrently or silently falling back on an earlier failure.
	direct := 0
	transport.direct = fakeTransport(func(*http.Request) (*http.Response, error) {
		direct++
		return &http.Response{StatusCode: 200, Body: io.NopCloser(strings.NewReader(`{}`)), Header: http.Header{}}, nil
	})
	cfg.Proxy.Enabled = false
	if err := client.JSON(t.Context(), "GET", TakumiAPI+"/fixture", nil, nil, nil, &result); err != nil || direct != 1 || connections.Load() != 1 {
		t.Fatal("disabled proxy did not take effect", err)
	}
}

func TestSOCKS5ProxyRemoteDNSAndAuthentication(t *testing.T) {
	for _, scheme := range []string{"socks5", "socks5h"} {
		t.Run(scheme, func(t *testing.T) {
			target, base := proxyTLSFixture(t)
			listener, err := net.Listen("tcp", "127.0.0.1:0")
			if err != nil {
				t.Fatal(err)
			}
			defer listener.Close()
			done := make(chan struct{})
			go func() {
				defer close(done)
				conn, err := listener.Accept()
				if err != nil {
					return
				}
				defer conn.Close()
				_ = conn.SetDeadline(time.Now().Add(3 * time.Second))
				read := func(size int) []byte { b := make([]byte, size); _, _ = io.ReadFull(conn, b); return b }
				greeting := read(2)
				if greeting[0] != 5 {
					t.Error("wrong SOCKS version")
					return
				}
				_ = read(int(greeting[1]))
				_, _ = conn.Write([]byte{5, 2})
				auth := read(2)
				username := string(read(int(auth[1])))
				password := string(read(int(read(1)[0])))
				if auth[0] != 1 || username != "fixture-user" || password != "fixture-password" {
					t.Error("SOCKS credentials mismatch")
					return
				}
				_, _ = conn.Write([]byte{1, 0})
				command := read(4)
				if command[0] != 5 || command[1] != 1 || command[3] != 3 {
					t.Error("SOCKS did not use remote hostname resolution")
					return
				}
				host := string(read(int(read(1)[0])))
				port := binary.BigEndian.Uint16(read(2))
				if host != "bbs-api.miyoushe.com" || port != 443 {
					t.Error("wrong SOCKS destination")
					return
				}
				_, _ = conn.Write([]byte{5, 0, 0, 1, 127, 0, 0, 1, 0, 1})
				tunnelFixture(t, conn, target.URL)
			}()
			cfg := model.NetworkConfig{Proxy: model.ProxyConfig{Enabled: true, URL: scheme + "://" + listener.Addr().String(), Username: "fixture-user", Password: "fixture-password"}}
			transport := newUpstreamTransport(base, func() model.NetworkConfig { return cfg })
			defer transport.CloseIdleConnections()
			client := NewClient("")
			client.HTTP.Transport = transport
			var result map[string]any
			if err := client.JSON(t.Context(), "GET", BBSAPI+"/fixture", nil, nil, http.Header{"Cookie": {"fixture-cookie"}}, &result); err != nil || result["fixture"] != true {
				t.Fatal("SOCKS request failed", err)
			}
			<-done
		})
	}
}

func TestProxyErrorsNeverFallbackOrExposeCredentials(t *testing.T) {
	proxy := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(http.StatusProxyAuthRequired) }))
	defer proxy.Close()
	directCalls := 0
	base := fakeTransport(func(*http.Request) (*http.Response, error) { directCalls++; return nil, errors.New("direct fixture") })
	cfg := model.NetworkConfig{Proxy: model.ProxyConfig{Enabled: true, URL: proxy.URL, Username: "private-user", Password: "private-password"}}
	client := NewClient("")
	client.HTTP.Transport = newUpstreamTransport(base, func() model.NetworkConfig { return cfg })
	err := client.JSON(context.Background(), "GET", TakumiAPI+"/fixture", nil, nil, nil, nil)
	if err == nil || directCalls != 0 || !strings.Contains(err.Error(), "代理连接失败") {
		t.Fatal("proxy error silently fell back", err, directCalls)
	}
	for _, secret := range []string{proxy.URL, cfg.Proxy.Username, cfg.Proxy.Password} {
		if strings.Contains(err.Error(), secret) {
			t.Fatal("proxy secret leaked")
		}
	}
	_ = client.JSON(t.Context(), "GET", "https://solver.example/pass_nine", nil, nil, nil, nil)
	if directCalls != 1 {
		t.Fatal("unrelated solver inherited proxy")
	}
}
