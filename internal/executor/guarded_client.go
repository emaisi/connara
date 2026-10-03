package executor

import (
	"context"
	"crypto/tls"
	"errors"
	"fmt"
	"net"
	"net/http"
	"net/netip"
	"net/url"
	"strings"
	"time"
)

// ClientWithTLS derives an isolated client while retaining the caller's
// guarded dialer, timeout, and redirect policy.
func ClientWithTLS(client *http.Client, config *tls.Config) (*http.Client, func(), error) {
	if config == nil {
		return client, func() {}, nil
	}
	transport, ok := client.Transport.(*http.Transport)
	if !ok {
		return nil, nil, errors.New("mTLS requires a guarded HTTP transport")
	}
	clone := transport.Clone()
	clone.TLSClientConfig = config.Clone()
	copy := *client
	copy.Transport = clone
	return &copy, clone.CloseIdleConnections, nil
}

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
		approved, _ := ctx.Value(configuredEndpointKey{}).(string)
		configured := strings.EqualFold(net.JoinHostPort(host, port), approved)
		for _, address := range addresses {
			if configured && (address.IsUnspecified() || address.IsLinkLocalUnicast() || address.IsMulticast()) {
				return nil, errors.New("configured target is a protected network endpoint")
			}
			if !configured && isBlockedAddress(address, allowedCIDRs) {
				return nil, errors.New("provider host resolves to a blocked network")
			}
		}
		if len(addresses) == 0 {
			return nil, errors.New("provider host has no IP address")
		}
		var dialErr error
		for _, resolved := range addresses {
			connection, err := dialer.DialContext(ctx, network, net.JoinHostPort(resolved.String(), port))
			if err == nil {
				return connection, nil
			}
			dialErr = err
		}
		return nil, dialErr
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

// ConfiguredEndpointClient authorizes only the origins explicitly configured by
// the control plane. Other requests retain the client's existing network guard.
func ConfiguredEndpointClient(client *http.Client, endpoints ...string) (*http.Client, func(), error) {
	origins := make(map[string]struct{})
	for _, endpoint := range endpoints {
		if endpoint == "" {
			continue
		}
		parsed, err := url.Parse(endpoint)
		if err != nil || (parsed.Scheme != "http" && parsed.Scheme != "https") || parsed.Hostname() == "" || parsed.User != nil || parsed.Fragment != "" {
			return nil, nil, errors.New("configured authentication endpoint must be an absolute http(s) URL")
		}
		origins[endpointOrigin(parsed)] = struct{}{}
	}
	copy := *client
	base := client.Transport
	if base == nil {
		base = http.DefaultTransport
	}
	cleanup := func() {}
	if transport, ok := base.(*http.Transport); ok {
		clone := transport.Clone()
		// Connect to the approved origin directly rather than delegating its network
		// authorization and DNS resolution to an environment-configured proxy.
		clone.Proxy = nil
		base = clone
		cleanup = clone.CloseIdleConnections
	}
	copy.Transport = &configuredEndpointTransport{base: base, origins: origins}
	copy.CheckRedirect = func(req *http.Request, via []*http.Request) error {
		if len(via) >= 5 {
			return errors.New("too many provider redirects")
		}
		if len(via) > 0 && endpointOrigin(req.URL) != endpointOrigin(via[len(via)-1].URL) {
			return errors.New("authentication redirects to another origin are blocked")
		}
		if client.CheckRedirect != nil {
			return client.CheckRedirect(req, via)
		}
		return nil
	}
	return &copy, cleanup, nil
}

type configuredEndpointKey struct{}
type configuredEndpointTransport struct {
	base    http.RoundTripper
	origins map[string]struct{}
}

func endpointAddress(endpoint *url.URL) string {
	port := endpoint.Port()
	if port == "" {
		if endpoint.Scheme == "https" {
			port = "443"
		} else {
			port = "80"
		}
	}
	return net.JoinHostPort(strings.ToLower(endpoint.Hostname()), port)
}

func endpointOrigin(endpoint *url.URL) string {
	return strings.ToLower(endpoint.Scheme) + "://" + endpointAddress(endpoint)
}

func (t *configuredEndpointTransport) RoundTrip(request *http.Request) (*http.Response, error) {
	approved := ""
	if _, ok := t.origins[endpointOrigin(request.URL)]; ok {
		approved = endpointAddress(request.URL)
	}
	request = request.Clone(context.WithValue(request.Context(), configuredEndpointKey{}, approved))
	return t.base.RoundTrip(request)
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
