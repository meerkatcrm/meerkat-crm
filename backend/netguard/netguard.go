// Package netguard is the one implementation of the outbound SSRF guard. Every feature
// that fetches a user-supplied URL (webhooks, CalDAV, CardDAV, Monica, photo URLs)
// dials through DialContext, so the address that is checked is the address that is used:
// redirects and DNS rebinding cannot reach an internal host.
//
// Not guarded, on purpose: the OIDC issuer and the SMTP host. Both are set by the
// administrator in the environment, not by users, and are often internal services.
package netguard

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/http"
	"net/netip"
	"time"
)

// Errors returned by DialContext when callers pass no sentinels of their own.
var (
	ErrUnreachable    = errors.New("host could not be resolved")
	ErrPrivateAddress = errors.New("address is private, loopback or link-local")
)

// blockedPrefixes are the ranges that netip.Addr's own helpers (IsPrivate, IsLoopback, ...)
// do not cover but that reach local or internal hosts in practice. The documentation
// ranges are not listed: they are not routable, so they cannot reach anything.
var blockedPrefixes = mustPrefixes(
	"0.0.0.0/8",       // "this network"; 0.0.0.0 reaches the local host on Linux
	"100.64.0.0/10",   // carrier-grade NAT (RFC 6598); some clouds put their metadata service here
	"192.0.0.0/24",    // IETF protocol assignments
	"198.18.0.0/15",   // benchmarking
	"240.0.0.0/4",     // reserved, includes the broadcast address
	"2001::/32",       // Teredo, which tunnels to an address the prefix does not reveal
	"64:ff9b:1::/48",  // local-use NAT64
	"::ffff:0:0:0/96", // IPv4-translated (SIIT); not ::ffff:0:0/96, which is every mapped IPv4 address
	"100::/64",        // discard-only
	"fec0::/10",       // deprecated IPv6 site-local, still routed internally on some networks
)

var (
	nat64Prefix          = netip.MustParsePrefix("64:ff9b::/96")
	sixToFour            = netip.MustParsePrefix("2002::/16")
	ipv4CompatiblePrefix = netip.MustParsePrefix("::/96")
)

func mustPrefixes(s ...string) []netip.Prefix {
	out := make([]netip.Prefix, len(s))
	for i, p := range s {
		out[i] = netip.MustParsePrefix(p)
	}
	return out
}

// IsBlockedIP reports whether ip is an address that must not be reached when private
// URLs are blocked: loopback, private (RFC 1918, unique local), link-local (this
// includes the cloud metadata address 169.254.169.254), multicast, the unspecified
// and "this network" range, carrier-grade NAT, benchmarking and reserved ranges.
// Addresses that embed an IPv4 address are judged as the IPv4 address they contain:
// IPv4-mapped (::ffff:a.b.c.d), IPv4-compatible (::a.b.c.d), NAT64 (64:ff9b::/96) and
// 6to4 (2002::/16).
func IsBlockedIP(ip net.IP) bool {
	addr, ok := netip.AddrFromSlice(ip)
	if !ok {
		return true
	}
	return isBlockedAddr(addr)
}

func isBlockedAddr(a netip.Addr) bool {
	a = a.Unmap()
	if !a.IsValid() || a.IsLoopback() || a.IsPrivate() || a.IsLinkLocalUnicast() || a.IsLinkLocalMulticast() ||
		a.IsMulticast() || a.IsUnspecified() {
		return true
	}
	for _, p := range blockedPrefixes {
		if p.Contains(a) {
			return true
		}
	}
	if a.Is6() {
		b := a.As16()
		switch {
		case nat64Prefix.Contains(a), ipv4CompatiblePrefix.Contains(a):
			return isBlockedAddr(netip.AddrFrom4([4]byte{b[12], b[13], b[14], b[15]}))
		case sixToFour.Contains(a):
			return isBlockedAddr(netip.AddrFrom4([4]byte{b[2], b[3], b[4], b[5]}))
		}
	}
	return false
}

// Test seams: the resolver and the dialer. Production code uses the defaults. lookupIP
// reads net.DefaultResolver on every call, so tests that swap the default resolver work.
var (
	lookupIP = func(ctx context.Context, host string) ([]net.IP, error) {
		return net.DefaultResolver.LookupIP(ctx, "ip", host)
	}
	dialIP = func(ctx context.Context, network, addr string) (net.Conn, error) {
		dialer := &net.Dialer{Timeout: 10 * time.Second, KeepAlive: 10 * time.Second}
		return dialer.DialContext(ctx, network, addr)
	}
)

// NewTransport returns a transport whose connections go through the guard. It never
// uses a proxy (HTTP_PROXY and friends are ignored): with a proxy the dialled address
// would be the proxy's, and the guard would check the wrong host.
func NewTransport(errUnreachable, errPrivate error) *http.Transport {
	return &http.Transport{
		Proxy:               nil,
		DialContext:         NewDialContext(errUnreachable, errPrivate),
		TLSHandshakeTimeout: 10 * time.Second,
		IdleConnTimeout:     90 * time.Second,
	}
}

// NewDialContext returns a DialContext that resolves the host, drops blocked addresses
// and dials the remaining ones by IP (trying each in turn). It returns errPrivate when
// no usable address is left and errUnreachable when the name does not resolve; callers
// pass their feature's sentinel errors so controllers can map failures.
func NewDialContext(errUnreachable, errPrivate error) func(ctx context.Context, network, addr string) (net.Conn, error) {
	return func(ctx context.Context, network, addr string) (net.Conn, error) {
		host, port, err := net.SplitHostPort(addr)
		if err != nil {
			return nil, err
		}
		// One lookup per dial; the connection goes to the IP that was checked, never
		// to a second resolution of the name (DNS rebinding).
		ips, err := lookupIP(ctx, host)
		if err != nil || len(ips) == 0 {
			return nil, fmt.Errorf("%w: could not resolve host", errUnreachable)
		}
		var safe []net.IP
		for _, ip := range ips {
			if !IsBlockedIP(ip) {
				safe = append(safe, ip)
			}
		}
		if len(safe) == 0 {
			return nil, errPrivate
		}
		var lastErr error
		for _, ip := range safe {
			conn, err := dialIP(ctx, network, net.JoinHostPort(ip.String(), port))
			if err == nil {
				return conn, nil
			}
			lastErr = err
		}
		return nil, lastErr
	}
}

// DialContext is NewDialContext with this package's sentinel errors.
var DialContext = NewDialContext(ErrUnreachable, ErrPrivateAddress)
