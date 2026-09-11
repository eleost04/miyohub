package notify

import (
	"context"
	"errors"
	"net"
	"net/http"
	"net/netip"
	"net/url"
	"os"
	"strings"
	"time"
)

// Private network access is an operator decision, never an API-user setting.
// Resolve and validate at dial time to prevent DNS rebinding and redirects.
var ErrPrivateNetwork = errors.New("推送私有地址未开放；自建 OneBot 或内网中继需管理员设置 MIYOHUB_PUSH_ALLOW_PRIVATE=true")

func safeDial(ctx context.Context, network, address string) (net.Conn, error) {
	host, port, err := net.SplitHostPort(address)
	if err != nil {
		return nil, errors.New("无效的推送主机")
	}
	addresses, err := net.DefaultResolver.LookupNetIP(ctx, "ip", host)
	if err != nil {
		return nil, errors.New("推送主机解析失败")
	}
	allowPrivate := os.Getenv("MIYOHUB_PUSH_ALLOW_PRIVATE") == "true"
	var last error
	for _, ip := range addresses {
		if !allowedPushIP(ip, allowPrivate) {
			last = ErrPrivateNetwork
			continue
		}
		conn, err := (&net.Dialer{Timeout: 10 * time.Second}).DialContext(ctx, network, net.JoinHostPort(ip.String(), port))
		if err == nil {
			return conn, nil
		}
		last = err
	}
	if last == nil {
		last = errors.New("无可用推送地址")
	}
	return nil, last
}

func allowedPushIP(ip netip.Addr, allowPrivate bool) bool {
	ip = ip.Unmap()
	// The operator opt-in is only for private/loopback integrations. It must
	// not also open link-local metadata, shared carrier or reserved networks.
	return publicIP(ip) || allowPrivate && (ip.IsPrivate() || ip.IsLoopback())
}

func publicIP(ip netip.Addr) bool {
	ip = ip.Unmap()
	if !ip.IsGlobalUnicast() || ip.IsPrivate() || ip.IsLoopback() || ip.IsLinkLocalUnicast() {
		return false
	}
	for _, block := range []string{"0.0.0.0/8", "100.64.0.0/10", "192.0.0.0/24", "192.0.2.0/24", "198.18.0.0/15", "198.51.100.0/24", "203.0.113.0/24", "240.0.0.0/4", "2001:db8::/32", "64:ff9b::/96", "2002::/16"} {
		if netip.MustParsePrefix(block).Contains(ip) {
			return false
		}
	}
	return true
}

func NewHTTPClient() *http.Client {
	return &http.Client{Timeout: 40 * time.Second, Transport: &http.Transport{
		DialContext: safeDial, TLSHandshakeTimeout: 10 * time.Second,
		ResponseHeaderTimeout: 38 * time.Second, IdleConnTimeout: 60 * time.Second,
	}, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
}

func WeixinEndpoint(raw string) bool {
	u, err := url.Parse(raw)
	if err != nil || u.Scheme != "https" || u.User != nil || u.Fragment != "" || u.RawQuery != "" || u.Port() != "" || (u.Path != "" && u.Path != "/") {
		return false
	}
	host := strings.ToLower(u.Hostname())
	return host == "ilinkai.weixin.qq.com" || strings.HasSuffix(host, ".ilinkai.weixin.qq.com") || host == "ilink.weixin.qq.com" || strings.HasSuffix(host, ".ilink.weixin.qq.com")
}
