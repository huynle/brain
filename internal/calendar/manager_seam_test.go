package calendar

import (
	"context"
	"errors"
	"net/http"
	"net/netip"
	"testing"
	"time"
)

// A test seam lets another package's tests drive a runtime source at a
// loopback TLS feed through the real manager. The public-address guard is the
// product; the seam is the only way around it, so it must be explicit.
func TestManagerWithTestSeamReachesLoopbackFeed(t *testing.T) {
	now := time.Now().UTC()
	srv := newFeedServer(t, serveFeed(icsFeed(icsEvent{uid: "seam", title: "Seam", start: now.Add(time.Hour)})))
	tlsCfg := srv.srv.Client().Transport.(*http.Transport).TLSClientConfig
	loopback := netip.MustParseAddr("127.0.0.1")

	reg, err := NewRegistry(nil)
	if err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	poller, err := NewPoller(reg, nil, dir, PollerOptions{})
	if err != nil {
		t.Fatal(err)
	}
	opts := ManagerOptions{}.WithTestSeam(TestSeam{
		Allowed: func(a netip.Addr) bool { return a == loopback },
		TLS:     tlsCfg,
	})
	mgr, err := NewManager(reg, poller, dir, opts)
	if err != nil {
		t.Fatal(err)
	}

	st, err := mgr.Add(context.Background(), AddRequest{Name: "seam", URL: srv.srv.URL + "/feed.ics"}, "test")
	if err != nil {
		t.Fatalf("Add through the seam: %v", err)
	}
	if st.EventCount != 1 {
		t.Fatalf("first snapshot events = %d, want 1", st.EventCount)
	}
}

func TestManagerWithoutTestSeamRefusesLoopbackFeed(t *testing.T) {
	now := time.Now().UTC()
	srv := newFeedServer(t, serveFeed(icsFeed(icsEvent{uid: "guard", title: "Guard", start: now.Add(time.Hour)})))

	reg, err := NewRegistry(nil)
	if err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	poller, err := NewPoller(reg, nil, dir, PollerOptions{})
	if err != nil {
		t.Fatal(err)
	}
	mgr, err := NewManager(reg, poller, dir, ManagerOptions{})
	if err != nil {
		t.Fatal(err)
	}
	_, err = mgr.Add(context.Background(), AddRequest{Name: "guard", URL: srv.srv.URL + "/feed.ics"}, "test")
	if !errors.Is(err, ErrRejectedURL) {
		t.Fatalf("Add to loopback without a seam = %v, want ErrRejectedURL", err)
	}
}
