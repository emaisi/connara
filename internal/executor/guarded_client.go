package executor

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/http"
	"net/netip"
	"strings"
	"time"
)

// NewGuardedClient blocks non-public networks unless an address is explicitly
// covered by allowedCIDRs. This keeps the default safe while supporting
// enterprise APIs on approved private network ranges.
func NewGuardedClient(allowedCIDRs ...netip.Prefix) *http.Client {
	dialer := &net.Dialer{Timeout: 5 * time.Second, KeepAlive: 30 * time.Second}
	transport := http.DefaultTransport.(*http.Transport).Clone()
	transport.DialContext = func(ctx context.Context, network, address string) (net.Conn, error) {
		host, port, err := net.SplitHostPort(address)
		if err != nil {
			return nil, err
		}
		addresses, err := net.DefaultResolver.LookupNetIP(ctx, "ip", host)
		if err != nil {
			return nil, fmt.Errorf("resolve provider host: %w", err)
		}
		for _, address := range addresses {
			if isBlockedAddress(address, allowedCIDRs) {
				return nil, errors.New("provider host resolves to a blocked network")
			}
		}
		if len(addresses) == 0 {
			return nil, errors.New("provider host has no IP address")
		}
		return dialer.DialContext(ctx, network, net.JoinHostPort(addresses[0].String(), port))
	}
	return &http.Client{
		Transport: transport,
		Timeout:   35 * time.Second,
		CheckRedirect: func(req *http.Request, via []*http.Request) error {
			if len(via) >= 5 {
				return errors.New("too many provider redirects")
			}
			if len(via) > 0 && !strings.EqualFold(via[len(via)-1].URL.Host, req.URL.Host) {
				return errors.New("provider redirects to another host are blocked")
			}
			return nil
		},
	}
}

func isBlockedAddress(address netip.Addr, allowedCIDRs []netip.Prefix) bool {
	address = address.Unmap()
	for _, prefix := range allowedCIDRs {
		if prefix.Contains(address) {
			return false
		}
	}
	if !address.IsValid() || address.IsLoopback() || address.IsPrivate() || address.IsUnspecified() ||
		address.IsLinkLocalUnicast() || address.IsLinkLocalMulticast() || address.IsMulticast() {
		return true
	}
	blockedPrefixes := []netip.Prefix{
		netip.MustParsePrefix("100.64.0.0/10"),
		netip.MustParsePrefix("192.0.0.0/24"),
		netip.MustParsePrefix("198.18.0.0/15"),
		netip.MustParsePrefix("240.0.0.0/4"),
	}
	for _, prefix := range blockedPrefixes {
		if prefix.Contains(address) {
			return true
		}
	}
	return false
}
