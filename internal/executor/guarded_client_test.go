package executor

import (
	"net/http"
	"net/http/httptest"
	"net/netip"
	"net/url"
	"strings"
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

func TestConfiguredEndpointsAuthorizeOnlyTheirOrigin(t *testing.T) {
	otherCalls := 0
	other := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { otherCalls++; w.WriteHeader(http.StatusOK) }))
	defer other.Close()
	approved := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/redirect" {
			http.Redirect(w, r, other.URL, http.StatusFound)
			return
		}
		w.WriteHeader(http.StatusOK)
	}))
	defer approved.Close()
	base := NewGuardedClient()
	if response, err := base.Get(approved.URL); err == nil {
		response.Body.Close()
		t.Fatal("base client must still block loopback")
	}
	client, cleanup, err := ConfiguredEndpointClient(base, approved.URL+"/token")
	if err != nil {
		t.Fatal(err)
	}
	defer cleanup()
	response, err := client.Get(approved.URL + "/token")
	if err != nil {
		t.Fatalf("configured endpoint requires no CIDR setting: %v", err)
	}
	response.Body.Close()
	if response.StatusCode != http.StatusOK {
		t.Fatalf("unexpected HTTP %d", response.StatusCode)
	}
	if response, err := client.Get(other.URL); err == nil {
		response.Body.Close()
		t.Fatal("unconfigured port must remain blocked")
	}
	if response, err := client.Get(approved.URL + "/redirect"); err == nil {
		response.Body.Close()
		t.Fatal("redirect to another port must be rejected")
	}
	if otherCalls != 0 {
		t.Fatal("unconfigured service received a request")
	}
	if response, err := base.Get(approved.URL); err == nil {
		response.Body.Close()
		t.Fatal("scoped authorization leaked to base client")
	}
}

func TestConfiguredEndpointRejectsSchemeDowngrade(t *testing.T) {
	client, cleanup, err := ConfiguredEndpointClient(NewGuardedClient(), "https://auth.example.com/token")
	if err != nil {
		t.Fatal(err)
	}
	defer cleanup()
	previous := &http.Request{URL: &url.URL{Scheme: "https", Host: "auth.example.com"}}
	next := &http.Request{URL: &url.URL{Scheme: "http", Host: "auth.example.com"}}
	if err := client.CheckRedirect(next, []*http.Request{previous}); err == nil || !strings.Contains(err.Error(), "another origin") {
		t.Fatalf("expected scheme downgrade rejection: %v", err)
	}
}

func TestConfiguredDomainNeedsNoCIDRConfiguration(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(http.StatusOK) }))
	defer server.Close()
	endpoint := strings.Replace(server.URL, "127.0.0.1", "localhost", 1)
	client, cleanup, err := ConfiguredEndpointClient(NewGuardedClient(), endpoint+"/token")
	if err != nil {
		t.Fatal(err)
	}
	defer cleanup()
	response, err := client.Get(endpoint + "/token")
	if err != nil {
		t.Fatalf("configured domain was not authorized: %v", err)
	}
	response.Body.Close()
	if response.StatusCode != http.StatusOK {
		t.Fatalf("unexpected HTTP %d", response.StatusCode)
	}
}
