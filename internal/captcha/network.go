package captcha

import (
	"context"
	"errors"
	"net"
	"net/http"
	"net/netip"
	"net/url"
	"strings"
	"time"

	"github.com/eleost04/miyohub/internal/model"
)

var ErrPrivateEndpoint = errors.New("个人打码仅支持公网地址；内网打码请使用管理员授权的站点服务")
var ErrAuthorization = errors.New("打码配置或使用权限已变化，请刷新后重试")

func publicIP(ip netip.Addr) bool {
	ip = ip.Unmap()
	if !ip.IsGlobalUnicast() || ip.IsPrivate() || ip.IsLoopback() || ip.IsLinkLocalUnicast() {
		return false
	}
	for _, block := range []string{"0.0.0.0/8", "100.64.0.0/10", "192.0.0.0/24", "192.0.2.0/24", "198.18.0.0/15", "198.51.100.0/24", "203.0.113.0/24", "240.0.0.0/4", "2001::/32", "2001:db8::/32", "64:ff9b::/96", "64:ff9b:1::/48", "2002::/16"} {
		if netip.MustParsePrefix(block).Contains(ip) {
			return false
		}
	}
	return true
}

func ValidatePersonalChannel(c model.CaptchaChannel) error {
	if err := ValidateChannel(c); err != nil {
		return err
	}
	if c.Provider != "custom" || c.Endpoint == "" {
		return nil
	}
	u, _ := url.Parse(c.Endpoint)
	host := strings.TrimSuffix(strings.ToLower(u.Hostname()), ".")
	if ip, err := netip.ParseAddr(host); err == nil {
		if !publicIP(ip) {
			return ErrPrivateEndpoint
		}
	} else if !strings.Contains(host, ".") || host == "localhost" || strings.HasSuffix(host, ".localhost") || strings.HasSuffix(host, ".local") || strings.HasSuffix(host, ".internal") {
		return ErrPrivateEndpoint
	}
	return nil
}

type lookupFunc func(context.Context, string, string) ([]netip.Addr, error)
type dialFunc func(context.Context, string, string) (net.Conn, error)

func dialPublic(ctx context.Context, network, address string, lookup lookupFunc, dial dialFunc) (net.Conn, error) {
	host, port, err := net.SplitHostPort(address)
	if err != nil {
		return nil, errors.New("打码服务地址无效")
	}
	ips, err := lookup(ctx, "ip", host)
	if err != nil || len(ips) == 0 {
		return nil, errors.New("打码服务域名解析失败")
	}
	// Reject mixed public/private answers as well; dial the validated IP
	// directly, never re-resolve the hostname after this authorization check.
	for _, ip := range ips {
		if !publicIP(ip) {
			return nil, ErrPrivateEndpoint
		}
	}
	for _, ip := range ips {
		var conn net.Conn
		conn, err = dial(ctx, network, net.JoinHostPort(ip.String(), port))
		if err == nil {
			return conn, nil
		}
	}
	return nil, errors.New("无法连接打码服务")
}

// No environment proxy, custom DialTLS, Unix sockets or redirect fallback can
// circumvent the personal-service boundary. Site services use a separate path.
var personalTransport http.RoundTripper = &http.Transport{
	DialContext: func(ctx context.Context, network, address string) (net.Conn, error) {
		return dialPublic(ctx, network, address, net.DefaultResolver.LookupNetIP, (&net.Dialer{Timeout: 10 * time.Second}).DialContext)
	}, TLSHandshakeTimeout: 10 * time.Second, IdleConnTimeout: 60 * time.Second,
}

type authorizedTransport struct {
	base    http.RoundTripper
	allowed func() bool
}

func (t authorizedTransport) RoundTrip(r *http.Request) (*http.Response, error) {
	if r.Context().Err() != nil {
		return nil, r.Context().Err()
	}
	if t.allowed != nil && !t.allowed() {
		return nil, ErrAuthorization
	}
	return t.base.RoundTrip(r)
}
