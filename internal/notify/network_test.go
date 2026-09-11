package notify

import (
	"errors"
	"net/netip"
	"testing"
)

func TestNotificationNetworkRejectsLocalMetadataAndRebindingTargets(t *testing.T) {
	for _, raw := range []string{"127.0.0.1", "::1", "10.1.2.3", "172.16.0.1", "192.168.1.2", "169.254.169.254", "100.100.100.200", "::ffff:127.0.0.1", "fc00::1", "fe80::1", "0.0.0.0", "224.0.0.1", "198.18.0.1", "64:ff9b::7f00:1"} {
		if publicIP(netip.MustParseAddr(raw)) {
			t.Errorf("unsafe destination accepted: %s", raw)
		}
	}
	for _, raw := range []string{"1.1.1.1", "8.8.8.8", "2606:4700:4700::1111"} {
		if !publicIP(netip.MustParseAddr(raw)) {
			t.Error("public destination blocked", raw)
		}
	}
	t.Setenv("MIYOHUB_PUSH_ALLOW_PRIVATE", "false")
	if _, err := safeDial(t.Context(), "tcp", "127.0.0.1:1"); !errors.Is(err, ErrPrivateNetwork) {
		t.Fatal("local connection attempted without operator policy", err)
	}
	t.Setenv("MIYOHUB_PUSH_ALLOW_PRIVATE", "true")
	for _, raw := range []string{"169.254.169.254:80", "100.100.100.200:80"} {
		if _, err := safeDial(t.Context(), "tcp", raw); !errors.Is(err, ErrPrivateNetwork) {
			t.Fatal("metadata address accepted even with local integrations enabled", err)
		}
	}
	for _, raw := range []string{"127.0.0.1", "::1", "10.1.2.3", "172.16.0.1", "192.168.1.2", "::ffff:127.0.0.1", "fc00::1"} {
		if !allowedPushIP(netip.MustParseAddr(raw), true) {
			t.Error("operator-approved private integration blocked", raw)
		}
	}
	for _, raw := range []string{"169.254.169.254", "100.100.100.200", "fe80::1", "0.0.0.0", "224.0.0.1", "198.18.0.1", "64:ff9b::7f00:1"} {
		if allowedPushIP(netip.MustParseAddr(raw), true) {
			t.Error("non-private special address accepted by operator opt-in", raw)
		}
	}
}
