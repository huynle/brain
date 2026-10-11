package calendar

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/http"
	"net/http/httptest"
	"net/netip"
	"strconv"
	"sync"
	"testing"
	"time"
)

func mustAddr(t *testing.T, s string) netip.Addr {
	t.Helper()
	a, err := netip.ParseAddr(s)
	if err != nil {
		t.Fatalf("ParseAddr(%q): %v", s, err)
	}
	return a
}

// fixedLookup answers every name with the same addresses.
func fixedLookup(addrs ...string) func(context.Context, string) ([]netip.Addr, error) {
	return func(_ context.Context, _ string) ([]netip.Addr, error) {
		out := make([]netip.Addr, 0, len(addrs))
		for _, s := range addrs {
			a, err := netip.ParseAddr(s)
			if err != nil {
				return nil, err
			}
			out = append(out, a)
		}
		return out, nil
	}
}

func TestPublicAddressRefusesEveryBlockedRange(t *testing.T) {
	blocked := []string{
		// IPv4
		"127.0.0.1", "127.255.255.254",
		"10.0.0.1", "10.255.255.255",
		"172.16.0.1", "172.31.255.255",
		"192.168.1.1",
		"169.254.1.1", "169.254.169.254",
		"100.64.0.1", "100.127.255.254", // CGNAT, includes Tailscale
		"0.0.0.0", "0.1.2.3",
		"224.0.0.1", "239.255.255.255",
		"240.0.0.1", "255.255.255.255",
		"192.0.0.1", "198.18.0.1",
		// IPv6
		"::1", "::",
		"fe80::1", "fe80::abcd",
		"fc00::1", "fd12:3456::1",
		"fec0::1",
		"ff02::1", "ff05::2",
		"100::1",
		"::127.0.0.1", // IPv4-compatible
		// IPv4-mapped IPv6 forms of all of the above
		"::ffff:127.0.0.1", "::ffff:10.0.0.1", "::ffff:172.16.0.1", "::ffff:192.168.1.1",
		"::ffff:169.254.169.254", "::ffff:100.64.0.1", "::ffff:0.0.0.0", "::ffff:224.0.0.1",
		"::ffff:7f00:1",
		// NAT64 embedding a private IPv4 address
		"64:ff9b::a00:1", "64:ff9b::c0a8:101",
		// zone-scoped link-local
		"fe80::1%en0",
	}
	for _, s := range blocked {
		if publicAddress(mustAddr(t, s), nil) {
			t.Errorf("publicAddress(%s) = true, want false", s)
		}
	}
}

func TestPublicAddressAcceptsPublicAddresses(t *testing.T) {
	public := []string{
		"8.8.8.8", "142.250.80.46", "100.63.255.255", "100.128.0.1",
		"2606:4700:4700::1111", "2a00:1450:4001::200e",
		"::ffff:8.8.8.8",
		"64:ff9b::808:808",
	}
	for _, s := range public {
		if !publicAddress(mustAddr(t, s), nil) {
			t.Errorf("publicAddress(%s) = false, want true", s)
		}
	}
}

func TestPublicAddressAllowHookOnlyWidensOneAddress(t *testing.T) {
	loopback := mustAddr(t, "127.0.0.1")
	allow := func(a netip.Addr) bool { return a == loopback }
	if !publicAddress(loopback, allow) {
		t.Fatal("allowed loopback refused")
	}
	if publicAddress(mustAddr(t, "10.0.0.1"), allow) {
		t.Fatal("allow hook widened to an unlisted private address")
	}
}

func TestDialRefusesHostNameResolvingToPrivateAddress(t *testing.T) {
	h := runtimeHooks{lookup: fixedLookup("10.0.0.7")}
	_, err := h.dial(context.Background(), "tcp", "cal.example:443")
	if !errors.Is(err, errNonPublicAddress) {
		t.Fatalf("dial to a name resolving to 10.0.0.7: err=%v want errNonPublicAddress", err)
	}
}

func TestDialRefusesWhenAnyAnswerIsNonPublic(t *testing.T) {
	// A mixed answer is refused: one private address in the set is enough.
	h := runtimeHooks{lookup: fixedLookup("8.8.8.8", "192.168.0.10")}
	_, err := h.dial(context.Background(), "tcp", "cal.example:443")
	if !errors.Is(err, errNonPublicAddress) {
		t.Fatalf("mixed public/private answer: err=%v want errNonPublicAddress", err)
	}
}

func TestDialRefusesIPLiteralHosts(t *testing.T) {
	h := runtimeHooks{}
	for _, addr := range []string{
		"10.0.0.1:443", "127.0.0.1:443", "[::1]:443", "[::ffff:127.0.0.1]:443",
		"[fe80::1%en0]:443", "[fd00::5]:443", "100.64.1.1:443",
	} {
		_, err := h.dial(context.Background(), "tcp", addr)
		if !errors.Is(err, errNonPublicAddress) {
			t.Errorf("dial(%s): err=%v want errNonPublicAddress", addr, err)
		}
	}
}

// TestDialUsesOneLookupAndDialsTheCheckedAddress proves DNS rebinding cannot
// bypass the guard: the first answer is checked and dialled, and the second
// answer (private) is never requested.
func TestDialUsesOneLookupAndDialsTheCheckedAddress(t *testing.T) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer ln.Close()
	accepted := make(chan net.Addr, 1)
	go func() {
		c, err := ln.Accept()
		if err == nil {
			accepted <- c.RemoteAddr()
			c.Close()
		}
	}()

	var mu sync.Mutex
	calls := 0
	rebinding := func(_ context.Context, _ string) ([]netip.Addr, error) {
		mu.Lock()
		defer mu.Unlock()
		calls++
		if calls == 1 {
			return []netip.Addr{netip.MustParseAddr("127.0.0.1")}, nil
		}
		return []netip.Addr{netip.MustParseAddr("10.9.9.9")}, nil
	}
	loopback := mustAddr(t, "127.0.0.1")
	h := runtimeHooks{lookup: rebinding, allowed: func(a netip.Addr) bool { return a == loopback }}
	port := ln.Addr().(*net.TCPAddr).Port

	c, err := h.dial(context.Background(), "tcp", "cal.example:"+strconv.Itoa(port))
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	c.Close()
	if got := <-accepted; got.(*net.TCPAddr).IP.String() != "127.0.0.1" {
		t.Fatalf("connected to %v, want the checked 127.0.0.1", got)
	}
	mu.Lock()
	defer mu.Unlock()
	if calls != 1 {
		t.Fatalf("resolver called %d times for one dial, want exactly 1", calls)
	}
}

func TestRuntimeClientHasNoProxyAndTheGuardedDialer(t *testing.T) {
	c := newRuntimeClient(runtimeHooks{}, 0)
	tr, ok := c.Transport.(*http.Transport)
	if !ok {
		t.Fatalf("transport is %T, want *http.Transport", c.Transport)
	}
	if tr.Proxy != nil {
		t.Fatal("runtime client routes through a proxy; the checked address would not be dialled")
	}
	if tr.DialContext == nil {
		t.Fatal("runtime client has no guarded dialer")
	}
	if c.Timeout != defaultFetchTimeout {
		t.Fatalf("timeout %v, want %v", c.Timeout, defaultFetchTimeout)
	}
	if c.CheckRedirect == nil {
		t.Fatal("runtime client has no redirect policy")
	}
}

// TestRuntimeClientRefusesRedirectToPrivateHost starts a TLS server on
// loopback that is reachable only through the allow hook. It redirects to a
// name that resolves to a private address. The redirected request must be
// refused by the same guard and never reach the server.
func TestRuntimeClientRefusesRedirectToPrivateHost(t *testing.T) {
	var mu sync.Mutex
	var paths []string
	srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		paths = append(paths, r.URL.Path)
		mu.Unlock()
		if r.URL.Path == "/start" {
			_, port, _ := net.SplitHostPort(r.Host)
			http.Redirect(w, r, "https://private.example:"+port+"/secret-token-path", http.StatusFound)
			return
		}
		_, _ = w.Write([]byte("should never be served"))
	}))
	defer srv.Close()

	loopback := netip.MustParseAddr("127.0.0.1")
	private := netip.MustParseAddr("10.0.0.9")
	tlsCfg := srv.Client().Transport.(*http.Transport).TLSClientConfig
	h := runtimeHooks{
		lookup: func(_ context.Context, host string) ([]netip.Addr, error) {
			switch host {
			case "example.com":
				return []netip.Addr{loopback}, nil
			case "private.example":
				return []netip.Addr{private}, nil
			}
			return nil, fmt.Errorf("unexpected lookup")
		},
		allowed: func(a netip.Addr) bool { return a == loopback },
		tls:     tlsCfg,
	}
	client := newRuntimeClient(h, 5*time.Second)
	port := portOf(t, srv.Listener.Addr().String())

	_, err := client.Get("https://example.com:" + port + "/start")
	if err == nil {
		t.Fatal("redirect to a private host was followed")
	}
	if !errors.Is(err, errNonPublicAddress) {
		t.Fatalf("redirect error: %v want it to wrap errNonPublicAddress", err)
	}
	mu.Lock()
	defer mu.Unlock()
	for _, p := range paths {
		if p == "/secret-token-path" {
			t.Fatal("the private redirect target was requested")
		}
	}
}

func portOf(t *testing.T, hostport string) string {
	t.Helper()
	_, port, err := net.SplitHostPort(hostport)
	if err != nil {
		t.Fatalf("SplitHostPort(%q): %v", hostport, err)
	}
	return port
}
