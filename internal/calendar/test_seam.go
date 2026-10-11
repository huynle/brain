package calendar

import (
	"context"
	"crypto/tls"
	"net/netip"
)

// TestSeam routes a runtime source's connections to a loopback test server.
// It exists only so tests in other packages can drive the real manager and
// router. Production never sets it: a ManagerOptions without a seam keeps the
// public-address guard unchanged.
type TestSeam struct {
	// Lookup resolves a host name. Nil means system DNS.
	Lookup func(ctx context.Context, host string) ([]netip.Addr, error)
	// Allowed reports whether an address may be dialled. Nil means the
	// production rule, which refuses every non-public address.
	Allowed func(netip.Addr) bool
	// TLS trusts the test server's certificate. Nil means the default trust.
	TLS *tls.Config
}

// WithTestSeam returns opts with every runtime source routed through seam.
func (o ManagerOptions) WithTestSeam(seam TestSeam) ManagerOptions {
	o.hooks = runtimeHooks{lookup: seam.Lookup, allowed: seam.Allowed, tls: seam.TLS}
	return o
}
