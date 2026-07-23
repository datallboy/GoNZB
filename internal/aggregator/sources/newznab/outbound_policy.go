package newznab

import (
	"context"
	"fmt"
	"net"
	"net/http"
	"net/netip"
	"strings"
	"time"
)

type OutboundPolicy struct {
	AllowPrivateAddresses bool
	AllowedCIDRs          []string
}

type addressPolicy struct {
	allowPrivate bool
	allowed      []netip.Prefix
}

var carrierGradeNAT = netip.MustParsePrefix("100.64.0.0/10")

func newPolicyHTTPClient(policy OutboundPolicy) *http.Client {
	addresses := addressPolicy{allowPrivate: policy.AllowPrivateAddresses}
	for _, raw := range policy.AllowedCIDRs {
		if prefix, err := netip.ParsePrefix(strings.TrimSpace(raw)); err == nil {
			addresses.allowed = append(addresses.allowed, prefix)
		}
	}

	transport := http.DefaultTransport.(*http.Transport).Clone()
	// Do not let an ambient proxy bypass target-address validation.
	transport.Proxy = nil
	dialer := &net.Dialer{Timeout: 10 * time.Second, KeepAlive: 30 * time.Second}
	transport.DialContext = addresses.dialContext(dialer)

	return &http.Client{
		Transport: transport,
		Timeout:   30 * time.Second,
	}
}

func (p addressPolicy) dialContext(dialer *net.Dialer) func(context.Context, string, string) (net.Conn, error) {
	return func(ctx context.Context, network, address string) (net.Conn, error) {
		host, port, err := net.SplitHostPort(address)
		if err != nil {
			return nil, fmt.Errorf("invalid outbound address %q: %w", address, err)
		}

		resolved, err := resolveAddresses(ctx, host)
		if err != nil {
			return nil, err
		}

		var lastDialErr error
		for _, addr := range resolved {
			if !p.allows(addr) {
				continue
			}
			conn, err := dialer.DialContext(ctx, network, net.JoinHostPort(addr.String(), port))
			if err == nil {
				return conn, nil
			}
			lastDialErr = err
		}
		if lastDialErr != nil {
			return nil, lastDialErr
		}
		return nil, fmt.Errorf("outbound address for %q is blocked by the Newznab source policy", host)
	}
}

func resolveAddresses(ctx context.Context, host string) ([]netip.Addr, error) {
	if addr, err := netip.ParseAddr(strings.TrimSpace(host)); err == nil {
		return []netip.Addr{addr.Unmap()}, nil
	}
	addrs, err := net.DefaultResolver.LookupNetIP(ctx, "ip", host)
	if err != nil {
		return nil, fmt.Errorf("resolve outbound host %q: %w", host, err)
	}
	for i := range addrs {
		addrs[i] = addrs[i].Unmap()
	}
	return addrs, nil
}

func (p addressPolicy) allows(addr netip.Addr) bool {
	for _, prefix := range p.allowed {
		if prefix.Contains(addr) {
			return true
		}
	}
	if !addr.IsValid() || addr.IsUnspecified() || addr.IsMulticast() {
		return false
	}
	if p.allowPrivate {
		return true
	}
	if !addr.IsGlobalUnicast() ||
		addr.IsPrivate() ||
		addr.IsLoopback() ||
		addr.IsLinkLocalUnicast() ||
		addr.IsLinkLocalMulticast() {
		return false
	}
	return !carrierGradeNAT.Contains(addr)
}
