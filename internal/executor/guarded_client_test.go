package executor

import (
	"net/http"
	"net/netip"
	"net/url"
	"testing"
)

func TestPrivateAddressRequiresExplicitAllowlist(t *testing.T) {
	address := netip.MustParseAddr("10.20.1.8")
	if !isBlockedAddress(address, nil) {
		t.Fatal("private address must be blocked by default")
	}
	allowed := []netip.Prefix{netip.MustParsePrefix("10.20.0.0/16")}
	if isBlockedAddress(address, allowed) {
		t.Fatal("explicitly allowlisted private address must be accepted")
	}
}

func TestGuardedClientRejectsCrossHostRedirect(t *testing.T) {
	client := NewGuardedClient()
	previous := &http.Request{URL: &url.URL{Scheme: "https", Host: "api.example.com"}}
	next := &http.Request{URL: &url.URL{Scheme: "https", Host: "other.example.com"}}
	if err := client.CheckRedirect(next, []*http.Request{previous}); err == nil {
		t.Fatal("expected cross-host redirect to be rejected")
	}
}

func TestAllowlistDoesNotPermitOtherPrivateNetworks(t *testing.T) {
	allowed := []netip.Prefix{netip.MustParsePrefix("10.20.0.0/16")}
	if !isBlockedAddress(netip.MustParseAddr("10.21.1.8"), allowed) {
		t.Fatal("private address outside allowlist must remain blocked")
	}
}
