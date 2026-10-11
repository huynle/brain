package calendar

import (
	"context"
	"io"
	"log/slog"
	"net/http"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"
)

// newRuntimeTestPoller returns a managed poller over an empty registry, the
// shape the server has when it has no config ics sources.
func newRuntimeTestPoller(t *testing.T, dataDir string, clock *fakeClock) (*Registry, *Poller) {
	t.Helper()
	reg, err := NewRegistry(nil)
	if err != nil {
		t.Fatalf("NewRegistry: %v", err)
	}
	p, err := NewPoller(reg, nil, dataDir, PollerOptions{
		Clock:  clock.Now,
		Logger: slog.New(slog.NewTextHandler(io.Discard, nil)),
	})
	if err != nil {
		t.Fatalf("NewPoller: %v", err)
	}
	p.markManaged()
	return reg, p
}

// attachTestSource registers a runtime source the way the manager does: the
// registry first, then the poller, with a plain test transport.
func attachTestSource(t *testing.T, reg *Registry, p *Poller, name, address string, poll time.Duration, tr http.RoundTripper, firstPoll time.Duration) {
	t.Helper()
	if err := reg.addICS(name, OriginRuntime, poll); err != nil {
		t.Fatalf("addICS: %v", err)
	}
	client := &http.Client{Transport: tr, Timeout: 5 * time.Second, CheckRedirect: refuseUnsafeRedirect}
	if err := p.attachRuntime(name, address, poll, client, snapshot{}, firstPoll); err != nil {
		t.Fatalf("attachRuntime: %v", err)
	}
}

func eventually(t *testing.T, within time.Duration, what string, cond func() bool) {
	t.Helper()
	deadline := time.Now().Add(within)
	for !cond() {
		if time.Now().After(deadline) {
			t.Fatalf("timed out waiting for %s", what)
		}
		time.Sleep(5 * time.Millisecond)
	}
}

func TestManagedRunBlocksWithoutSourcesUntilCancelled(t *testing.T) {
	clock := &fakeClock{t: time.Date(2026, 10, 11, 9, 0, 0, 0, time.UTC)}
	_, p := newRuntimeTestPoller(t, t.TempDir(), clock)
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() { p.Run(ctx); close(done) }()

	select {
	case <-done:
		t.Fatal("managed Run returned with no sources; runtime sources added later would never poll")
	case <-time.After(100 * time.Millisecond):
	}
	cancel()
	select {
	case <-done:
	case <-time.After(10 * time.Second):
		t.Fatal("managed Run did not return after cancel")
	}
}

func TestRuntimeSourceAttachedWhileRunningIsPolledUntilDetached(t *testing.T) {
	clock := &fakeClock{t: time.Date(2026, 10, 11, 9, 0, 0, 0, time.UTC)}
	srv := newFeedServer(t, serveFeed(icsFeed(icsEvent{uid: "a", title: "live", start: clock.Now().Add(time.Hour)})))
	reg, p := newRuntimeTestPoller(t, t.TempDir(), clock)

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() { p.Run(ctx); close(done) }()

	attachTestSource(t, reg, p, "personal", srv.srv.URL+"/feed/RUN-TOKEN/cal.ics", time.Minute, srv.transport(), 0)
	// The server counting a request is not the poll publishing its result, so
	// wait for the published success rather than for the hit.
	eventually(t, 10*time.Second, "first poll of an attached source to publish", func() bool {
		return !statusNamed(t, reg, "personal").LastSuccess.IsZero()
	})

	p.detach("personal")
	before := srv.hits.Load()
	p.Poll(context.Background(), "personal") // a detached name is a no-op
	time.Sleep(150 * time.Millisecond)
	if after := srv.hits.Load(); after != before {
		t.Fatalf("detached source still fetched: hits %d -> %d", before, after)
	}

	cancel()
	select {
	case <-done:
	case <-time.After(10 * time.Second):
		t.Fatal("Run did not return after cancel")
	}
}

func TestRuntimeSourceAttachedBeforeRunIsPolledWhenRunStarts(t *testing.T) {
	clock := &fakeClock{t: time.Date(2026, 10, 11, 9, 0, 0, 0, time.UTC)}
	srv := newFeedServer(t, serveFeed(icsFeed()))
	reg, p := newRuntimeTestPoller(t, t.TempDir(), clock)
	attachTestSource(t, reg, p, "early", srv.srv.URL+"/feed/EARLY/cal.ics", time.Minute, srv.transport(), time.Minute)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	done := make(chan struct{})
	go func() { p.Run(ctx); close(done) }()
	eventually(t, 10*time.Second, "Run to poll a source attached before it started", func() bool { return srv.hits.Load() >= 1 })
	cancel()
	<-done
}

func TestRuntimeAddressChangeTakesEffectOnNextPoll(t *testing.T) {
	clock := &fakeClock{t: time.Date(2026, 10, 11, 9, 0, 0, 0, time.UTC)}
	srv := newFeedServer(t, serveFeed(icsFeed()))
	reg, p := newRuntimeTestPoller(t, t.TempDir(), clock)
	attachTestSource(t, reg, p, "personal", srv.srv.URL+"/feed/OLD-TOKEN/cal.ics", time.Minute, srv.transport(), 0)
	p.Poll(context.Background(), "personal")
	if got := srv.lastPath(); got != "/feed/OLD-TOKEN/cal.ics" {
		t.Fatalf("first poll used %q", got)
	}

	if err := p.setAddress("personal", srv.srv.URL+"/feed/NEW-TOKEN/cal.ics", snapshot{Name: "personal", LastSuccess: clock.Now()}); err != nil {
		t.Fatalf("setAddress: %v", err)
	}
	p.Poll(context.Background(), "personal")
	if got := srv.lastPath(); got != "/feed/NEW-TOKEN/cal.ics" {
		t.Fatalf("address change not used on the next poll: got %q", got)
	}
}

func TestRuntimeAddressChangeToUnknownNameIsAnError(t *testing.T) {
	clock := &fakeClock{t: time.Date(2026, 10, 11, 9, 0, 0, 0, time.UTC)}
	_, p := newRuntimeTestPoller(t, t.TempDir(), clock)
	if err := p.setAddress("missing", "https://example.com/x", snapshot{}); err == nil {
		t.Fatal("setAddress for an unknown source = nil error")
	}
	if err := p.setRuntimePoll("missing", time.Minute); err == nil {
		t.Fatal("setRuntimePoll for an unknown source = nil error")
	}
}

func TestRuntimePollIntervalIsReadEachCycleWithOneMinuteFloor(t *testing.T) {
	clock := &fakeClock{t: time.Date(2026, 10, 11, 9, 0, 0, 0, time.UTC)}
	srv := newFeedServer(t, serveFeed(icsFeed()))
	reg, p := newRuntimeTestPoller(t, t.TempDir(), clock)
	attachTestSource(t, reg, p, "personal", srv.srv.URL+"/feed/T/cal.ics", 2*time.Minute, srv.transport(), time.Hour)

	st := p.lookup("personal")
	if got := st.interval(); got != 2*time.Minute {
		t.Fatalf("interval after attach: %v", got)
	}
	if err := p.setRuntimePoll("personal", 10*time.Minute); err != nil {
		t.Fatal(err)
	}
	if got := st.interval(); got != 10*time.Minute {
		t.Fatalf("interval after change: %v want 10m", got)
	}
	if err := p.setRuntimePoll("personal", 10*time.Second); err != nil {
		t.Fatal(err)
	}
	if got := st.interval(); got != time.Minute {
		t.Fatalf("interval below the floor: %v want 1m", got)
	}
}

// TestDetachCancelsAnInFlightPoll blocks a fetch on the server and checks
// that detach cancels it and returns promptly instead of waiting for it.
func TestDetachCancelsAnInFlightPoll(t *testing.T) {
	clock := &fakeClock{t: time.Date(2026, 10, 11, 9, 0, 0, 0, time.UTC)}
	started := make(chan struct{}, 4)
	srv := newFeedServer(t, func(w http.ResponseWriter, r *http.Request) {
		started <- struct{}{}
		<-r.Context().Done()
	})
	reg, p := newRuntimeTestPoller(t, t.TempDir(), clock)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	done := make(chan struct{})
	go func() { p.Run(ctx); close(done) }()
	attachTestSource(t, reg, p, "slow", srv.srv.URL+"/feed/SLOW/cal.ics", time.Minute, srv.transport(), 0)

	select {
	case <-started:
	case <-time.After(10 * time.Second):
		t.Fatal("fetch never started")
	}
	detached := make(chan struct{})
	go func() { p.detach("slow"); close(detached) }()
	select {
	case <-detached:
	case <-time.After(10 * time.Second):
		t.Fatal("detach waited on an in-flight fetch instead of cancelling it")
	}
	cancel()
	<-done
}

// TestConcurrentAttachDetachWhileRunning churns runtime sources while Run and
// direct polls are active. With -race it proves the source map, the per-source
// state and the registry are never touched without their locks.
func TestConcurrentAttachDetachWhileRunning(t *testing.T) {
	clock := &fakeClock{t: time.Date(2026, 10, 11, 9, 0, 0, 0, time.UTC)}
	srv := newFeedServer(t, serveFeed(icsFeed(icsEvent{uid: "c", title: "churn", start: clock.Now().Add(time.Hour)})))
	reg, p := newRuntimeTestPoller(t, t.TempDir(), clock)
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() { p.Run(ctx); close(done) }()

	var pollers sync.WaitGroup
	stop := make(chan struct{})
	pollers.Add(1)
	go func() {
		defer pollers.Done()
		for {
			select {
			case <-stop:
				return
			default:
			}
			p.Poll(ctx, "churn")
			_, _, _ = reg.Occurrences("churn")
		}
	}()
	for i := 0; i < 15; i++ {
		attachTestSource(t, reg, p, "churn", srv.srv.URL+"/feed/CHURN/cal.ics", time.Minute, srv.transport(), 0)
		time.Sleep(2 * time.Millisecond)
		p.detach("churn")
		reg.removeICS("churn")
	}
	close(stop)
	pollers.Wait()
	cancel()
	select {
	case <-done:
	case <-time.After(10 * time.Second):
		t.Fatal("Run did not return after cancel")
	}
	if srv.hits.Load() == 0 {
		t.Fatal("churned sources never reached the server")
	}
}

// TestDetachedSourceIsNotRepublished checks that once a source is detached a
// late poll cannot fetch it, write its snapshot, or re-publish it to the
// registry. Removal would otherwise be undone by a stale poll.
func TestDetachedSourceIsNotRepublished(t *testing.T) {
	clock := &fakeClock{t: time.Date(2026, 10, 11, 9, 0, 0, 0, time.UTC)}
	srv := newFeedServer(t, serveFeed(icsFeed(icsEvent{uid: "x", title: "gone", start: clock.Now().Add(time.Hour)})))
	dataDir := t.TempDir()
	reg, p := newRuntimeTestPoller(t, dataDir, clock)
	attachTestSource(t, reg, p, "gone", srv.srv.URL+"/feed/G/cal.ics", time.Minute, srv.transport(), time.Hour)
	p.Poll(context.Background(), "gone")
	if _, _, ok := reg.Occurrences("gone"); !ok {
		t.Fatal("attached source unknown before detach")
	}

	p.detach("gone")
	reg.removeICS("gone")
	hits := srv.hits.Load()
	snapPath := filepath.Join(dataDir, "calendars", "gone.json")
	if err := os.Remove(snapPath); err != nil {
		t.Fatalf("remove snapshot before late poll: %v", err)
	}
	p.Poll(context.Background(), "gone")

	if srv.hits.Load() != hits {
		t.Fatal("a detached source was fetched by a late poll")
	}
	if _, err := os.Stat(snapPath); !os.IsNotExist(err) {
		t.Fatalf("a late poll re-created the snapshot of a detached source: %v", err)
	}
	if _, _, ok := reg.Occurrences("gone"); ok {
		t.Fatal("a late poll re-published a detached source to the registry")
	}
}
