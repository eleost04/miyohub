package mihoyo

import (
	"errors"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"sync"

	"github.com/eleost04/miyohub/internal/model"
)

func ValidateProxy(p model.ProxyConfig) error {
	if len(p.Username) > 255 || len(p.Password) > 255 || strings.ContainsAny(p.Username+p.Password, "\r\n\x00") {
		return errors.New("代理用户名或密码过长或包含非法字符")
	}
	if p.URL == "" {
		if p.Enabled {
			return errors.New("开启代理前请填写代理地址")
		}
		return nil
	}
	u, err := url.Parse(p.URL)
	if err != nil || len(p.URL) > 2048 || u.Hostname() == "" || u.RawQuery != "" || u.ForceQuery || u.Fragment != "" || u.Opaque != "" || (u.Path != "" && u.Path != "/") {
		return errors.New("代理地址应为协议和主机，可包含端口，不支持路径、参数或片段")
	}
	if u.User != nil {
		return errors.New("请将代理用户名和密码填写在独立字段中，不要放在代理地址里")
	}
	switch u.Scheme {
	case "http", "https", "socks5", "socks5h":
	default:
		return errors.New("代理仅支持 http、https、socks5 或 socks5h")
	}
	if u.Port() != "" {
		port, err := strconv.Atoi(u.Port())
		if err != nil || port < 1 || port > 65535 {
			return errors.New("代理端口应在 1–65535 之间")
		}
	}
	return nil
}

// Only fixed official HTTPS destinations use this proxy. Personal captcha
// endpoints retain their DNS/IP-pinned SSRF guard; site solvers and arbitrary
// URLs never inherit the operator's proxy credentials or network privileges.
func officialDestination(u *url.URL) bool {
	if u.Scheme != "https" || u.User != nil || (u.Port() != "" && u.Port() != "443") {
		return false
	}
	switch strings.ToLower(u.Hostname()) {
	case "api-takumi.mihoyo.com", "api-takumi.miyoushe.com", "bbs-api.miyoushe.com", "passport-api.mihoyo.com", "act-nap-api.mihoyo.com", "public-data-api.mihoyo.com", "api-cloudgame.mihoyo.com":
		return true
	}
	return false
}

type upstreamTransport struct {
	direct   http.RoundTripper
	template *http.Transport
	settings func() model.NetworkConfig
	mu       sync.Mutex
	current  model.ProxyConfig
	proxied  *http.Transport
}

func newUpstreamTransport(base http.RoundTripper, settings func() model.NetworkConfig) *upstreamTransport {
	template, ok := base.(*http.Transport)
	if !ok {
		template = &http.Transport{}
	}
	template = template.Clone()
	template.Proxy = nil // Explicit UI settings, never an ambient environment proxy.
	direct := base
	if ok {
		direct = template
	}
	return &upstreamTransport{direct: direct, template: template, settings: settings}
}

func (t *upstreamTransport) RoundTrip(r *http.Request) (*http.Response, error) {
	if !officialDestination(r.URL) {
		return t.direct.RoundTrip(r)
	}
	p := t.settings().Proxy
	if !p.Enabled {
		return t.direct.RoundTrip(r)
	}
	if err := ValidateProxy(p); err != nil {
		return nil, err // Invalid or unreachable proxies never silently fall back.
	}
	t.mu.Lock()
	if t.proxied == nil || p != t.current {
		proxyURL, _ := url.Parse(p.URL)
		if p.Username != "" || p.Password != "" {
			proxyURL.User = url.UserPassword(p.Username, p.Password)
		}
		next := t.template.Clone()
		next.Proxy = http.ProxyURL(proxyURL)
		if t.proxied != nil {
			t.proxied.CloseIdleConnections()
		}
		t.current, t.proxied = p, next
	}
	transport := t.proxied
	t.mu.Unlock()
	response, err := transport.RoundTrip(r)
	if err != nil {
		return nil, &proxyRequestError{cause: err}
	}
	return response, nil
}

func (t *upstreamTransport) CloseIdleConnections() {
	if closer, ok := t.direct.(interface{ CloseIdleConnections() }); ok {
		closer.CloseIdleConnections()
	}
	t.mu.Lock()
	defer t.mu.Unlock()
	if t.proxied != nil {
		t.proxied.CloseIdleConnections()
	}
}

// Preserve the cause for retry classification, but never display proxy URLs,
// usernames, passwords or low-level CONNECT/SOCKS authentication responses.
type proxyRequestError struct{ cause error }

func (e *proxyRequestError) Error() string {
	return "米游社代理连接失败，请检查代理地址、凭据与网络"
}
func (e *proxyRequestError) Unwrap() error { return e.cause }
