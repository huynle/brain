package calendar

import (
	"context"
	"crypto/tls"
	"errors"
	"net"
	"net/http"
	"net/netip"
	"time"
)

// errNonPublicAddress is returned when a runtime source's host resolves to,
// or is, an address that is not public. It names no host and no address, so
// it is safe to show.
var errNonPublicAddress = errors.New("feed host resolves to a non-public address")

// runtimeHooks are the seams a runtime source's HTTP client is built from.
// Production uses the zero value: system DNS, the default TLS trust, and only
// public addresses. Tests set lookup to control name resolution, allowed to
// reach a loopback test server, and tls to trust its certificate.
type runtimeHooks struct {
	lookup  func(ctx context.Context, host string) ([]netip.Addr, error)
	allowed func(netip.Addr) bool
	tls     *tls.Config
}

// nonPublicPrefixes are the ranges a runtime source may never reach. They
// extend the loopback, private, link-local, CGNAT, unique-local, unspecified
// and multicast checks with reserved and special-purpose blocks.
var nonPublicPrefixes = []netip.Prefix{
	netip.MustParsePrefix("0.0.0.0/8"),      // this network
	netip.MustParsePrefix("10.0.0.0/8"),     // private
	netip.MustParsePrefix("100.64.0.0/10"),  // CGNAT, includes Tailscale
	netip.MustParsePrefix("127.0.0.0/8"),    // loopback
	netip.MustParsePrefix("169.254.0.0/16"), // link-local
	netip.MustParsePrefix("172.16.0.0/12"),  // private
	netip.MustParsePrefix("192.0.0.0/24"),   // IETF protocol assignments
	netip.MustParsePrefix("192.168.0.0/16"), // private
	netip.MustParsePrefix("198.18.0.0/15"),  // benchmarking
	netip.MustParsePrefix("224.0.0.0/4"),    // multicast
	netip.MustParsePrefix("240.0.0.0/4"),    // reserved, includes broadcast
	netip.MustParsePrefix("::/96"),          // unspecified, loopback, IPv4-compatible
	netip.MustParsePrefix("100::/64"),       // discard
	netip.MustParsePrefix("2001::/32"),      // Teredo tunnel (embeds an IPv4 address)
	netip.MustParsePrefix("2002::/16"),      // 6to4 tunnel (embeds an IPv4 address)
	netip.MustParsePrefix("fc00::/7"),       // unique-local
	netip.MustParsePrefix("fe80::/10"),      // link-local
	netip.MustParsePrefix("fec0::/10"),      // site-local (deprecated)
	netip.MustParsePrefix("ff00::/8"),       // multicast
}

// nat64Prefix embeds an IPv4 address in its last 32 bits. A NAT64 gateway
// translates it, so the embedded address decides whether it is public.
var nat64Prefix = netip.MustParsePrefix("64:ff9b::/96")

// publicAddress reports whether a runtime source may dial a. IPv4-mapped and
// NAT64-embedded forms are judged by the IPv4 address they carry. allowed, when
// set, can admit one address; production passes nil.
func publicAddress(a netip.Addr, allowed func(netip.Addr) bool) bool {
	if !a.IsValid() {
		return false
	}
	a = a.WithZone("").Unmap()
	if allowed != nil && allowed(a) {
		return true
	}
	if nat64Prefix.Contains(a) {
		b := a.As16()
		return publicAddress(netip.AddrFrom4([4]byte(b[12:16])), allowed)
	}
	for _, p := range nonPublicPrefixes {
		if p.Contains(a) {
			return false
		}
	}
	return !(a.IsLoopback() || a.IsPrivate() || a.IsLinkLocalUnicast() ||
		a.IsLinkLocalMulticast() || a.IsInterfaceLocalMulticast() ||
		a.IsMulticast() || a.IsUnspecified())
}

// dial resolves the host once, refuses the connection if any answer is not
// public, and dials a checked address directly. Because the address that is
// checked is the address that is dialled, a DNS rebinding answer that changes
// between lookups cannot reach a private host.
func (h runtimeHooks) dial(ctx context.Context, network, addr string) (net.Conn, error) {
	host, port, err := net.SplitHostPort(addr)
	if err != nil {
		return nil, errors.New("feed address is malformed")
	}
	addrs, err := h.resolve(ctx, host)
	if err != nil {
		return nil, errors.New("feed host could not be resolved")
	}
	if len(addrs) == 0 {
		return nil, errors.New("feed host has no address")
	}
	for _, a := range addrs {
		if !publicAddress(a, h.allowed) {
			return nil, errNonPublicAddress
		}
	}
	var d net.Dialer
	var last error
	for _, a := range addrs {
		c, err := d.DialContext(ctx, network, net.JoinHostPort(a.Unmap().String(), port))
		if err == nil {
			return c, nil
		}
		last = err
	}
	return nil, last
}

// resolve returns the addresses of host. An IP literal is its own answer and
// is never looked up.
func (h runtimeHooks) resolve(ctx context.Context, host string) ([]netip.Addr, error) {
	if a, err := netip.ParseAddr(host); err == nil {
		return []netip.Addr{a}, nil
	}
	if h.lookup != nil {
		return h.lookup(ctx, host)
	}
	return net.DefaultResolver.LookupNetIP(ctx, "ip", host)
}

// newRuntimeClient builds the HTTP client of one runtime source. It has its
// own transport, so removing the source can close its connections. It does
// not use a proxy, because a proxy would be dialled in place of the checked
// address. Redirects keep the https-only, five-hop rule, and each redirected
// request is dialled through the same guard.
func newRuntimeClient(h runtimeHooks, timeout time.Duration) *http.Client {
	if timeout <= 0 {
		timeout = defaultFetchTimeout
	}
	tr := &http.Transport{
		Proxy:               nil,
		DialContext:         h.dial,
		TLSClientConfig:     h.tls,
		TLSHandshakeTimeout: timeout,
		IdleConnTimeout:     30 * time.Second,
		ForceAttemptHTTP2:   true,
	}
	return &http.Client{
		Transport:     tr,
		Timeout:       timeout,
		CheckRedirect: refuseUnsafeRedirect,
	}
}
