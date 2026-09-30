package apiserver

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/huynle/brain-api/internal/tenant"
)

type countedGraph struct{ closes atomic.Int32 }

func (g *countedGraph) ServeHTTP(w http.ResponseWriter, r *http.Request) { w.WriteHeader(204) }
func (g *countedGraph) Close()                                           { g.closes.Add(1) }
func activeGraphAuthority(context.Context, tenant.ID) (graphPermit, error) {
	return graphPermit{Generation: 1, Lifetime: context.Background()}, nil
}

func TestGraphManagerHeldEvictionSaturationAndShutdown(t *testing.T) {
	g := &countedGraph{}
	var builds atomic.Int32
	m, _ := newTenantGraphManager(1, activeGraphAuthority, func(context.Context, tenant.ID) (graphResource, error) {
		builds.Add(1)
		return g, nil
	})
	l := acquireGraph(t, m, tenant.Local)
	m.Invalidate(tenant.Local)
	select {
	case <-l.Context.Done():
	case <-time.After(time.Second):
		t.Fatal("invalidation did not cancel request")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
	defer cancel()
	if _, err := m.Acquire(ctx, tenant.MustParse("other")); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("saturated acquire: %v", err)
	}
	if g.closes.Load() != 0 || builds.Load() != 1 {
		t.Fatal("retiring graph freed capacity or closed while referenced")
	}
	if err := m.Shutdown(ctx); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("shutdown must wait for lease: %v", err)
	}
	if _, err := m.Acquire(context.Background(), tenant.Local); err == nil {
		t.Fatal("shutdown admitted request")
	}
	l.Release()
	l.Release()
	shutdownGraphs(t, m)
	if g.closes.Load() != 1 {
		t.Fatal("graph not closed exactly once")
	}
}

func TestGraphManagerLRU(t *testing.T) {
	graphs := map[tenant.ID]*countedGraph{}
	m, _ := newTenantGraphManager(2, activeGraphAuthority, func(_ context.Context, id tenant.ID) (graphResource, error) {
		g := &countedGraph{}
		graphs[id] = g
		return g, nil
	})
	defer shutdownGraphs(t, m)
	a, b, c := tenant.MustParse("a"), tenant.MustParse("b"), tenant.MustParse("c")
	acquireGraph(t, m, a).Release()
	acquireGraph(t, m, b).Release()
	acquireGraph(t, m, a).Release()
	acquireGraph(t, m, c).Release()
	if graphs[b].closes.Load() != 1 || graphs[a].closes.Load() != 0 {
		t.Fatal("did not evict least recently used idle graph")
	}
}

func TestGraphManagerBuildingCancellationAndFailure(t *testing.T) {
	for _, fail := range []bool{false, true} {
		t.Run(fmtBool(fail), func(t *testing.T) {
			started, finish := make(chan struct{}), make(chan struct{})
			g := &countedGraph{}
			failure := errors.New("construction failed")
			m, _ := newTenantGraphManager(1, activeGraphAuthority, func(ctx context.Context, _ tenant.ID) (graphResource, error) {
				close(started)
				<-finish
				if fail {
					return nil, failure
				}
				return g, nil
			})
			ctx, cancel := context.WithCancel(context.Background())
			result := make(chan error, 1)
			go func() {
				l, err := m.Acquire(ctx, tenant.Local)
				if l != nil {
					l.Release()
				}
				result <- err
			}()
			<-started
			if fail {
				close(finish)
				if err := <-result; !errors.Is(err, failure) {
					t.Fatalf("failure lost: %v", err)
				}
			} else {
				cancel()
				if err := <-result; !errors.Is(err, context.Canceled) {
					t.Fatalf("wait cancellation: %v", err)
				}
				short, stop := context.WithTimeout(context.Background(), 20*time.Millisecond)
				defer stop()
				if err := m.Shutdown(short); !errors.Is(err, context.DeadlineExceeded) {
					t.Fatalf("shutdown did not wait for constructor: %v", err)
				}
				close(finish)
			}
			cancel()
			shutdownGraphs(t, m)
			if !fail && g.closes.Load() != 1 {
				t.Fatal("late constructor result leaked")
			}
		})
	}
}
func fmtBool(b bool) string {
	if b {
		return "failure"
	}
	return "cancel"
}

type testGraphAuthority struct {
	mu     sync.Mutex
	permit graphPermit
	cancel context.CancelFunc
	denied bool
	checks int
}

func (a *testGraphAuthority) check(ctx context.Context, id tenant.ID) (graphPermit, error) {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.checks++
	if a.denied {
		return graphPermit{}, errGraphUnavailable
	}
	return a.permit, nil
}
func (a *testGraphAuthority) advance(denied bool) {
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.cancel != nil {
		a.cancel()
	}
	ctx, cancel := context.WithCancel(context.Background())
	a.permit = graphPermit{Generation: a.permit.Generation + 1, Lifetime: ctx}
	a.cancel = cancel
	a.denied = denied
}
func TestGraphManagerSuspensionAndReusedGeneration(t *testing.T) {
	a := &testGraphAuthority{}
	a.advance(false)
	var builds atomic.Int32
	m, _ := newTenantGraphManager(1, a.check, func(context.Context, tenant.ID) (graphResource, error) { builds.Add(1); return &countedGraph{}, nil })
	defer shutdownGraphs(t, m)
	l := acquireGraph(t, m, tenant.Local)
	hit := acquireGraph(t, m, tenant.Local)
	hit.Release()
	a.mu.Lock()
	checks := a.checks
	a.mu.Unlock()
	if checks < 3 {
		t.Fatal("cache hit bypassed authority")
	}
	a.advance(true)
	if _, err := m.Acquire(context.Background(), tenant.Local); err == nil {
		t.Fatal("suspended cache hit accepted")
	}
	select {
	case <-l.Context.Done():
	case <-time.After(time.Second):
		t.Fatal("suspension did not cancel active request")
	}
	a.advance(false)
	l.Release()
	next := acquireGraph(t, m, tenant.Local)
	defer next.Release()
	if next.Graph == l.Graph || builds.Load() != 2 {
		t.Fatal("old cached graph reused after unsuspension")
	}
}

func TestGraphManagerStaleBuildPublication(t *testing.T) {
	a := &testGraphAuthority{}
	a.advance(false)
	started, finish := make(chan struct{}), make(chan struct{})
	g := &countedGraph{}
	m, _ := newTenantGraphManager(1, a.check, func(context.Context, tenant.ID) (graphResource, error) { close(started); <-finish; return g, nil })
	result := make(chan error, 1)
	go func() {
		l, err := m.Acquire(context.Background(), tenant.Local)
		if l != nil {
			l.Release()
		}
		result <- err
	}()
	<-started
	a.advance(false)
	close(finish)
	if err := <-result; err == nil {
		t.Fatal("stale build published")
	}
	shutdownGraphs(t, m)
	if g.closes.Load() != 1 {
		t.Fatal("stale build not closed")
	}
}

func TestGraphManagerRejectsInvalidConfiguration(t *testing.T) {
	if _, err := newTenantGraphManager(0, activeGraphAuthority, nil); err == nil {
		t.Fatal("unbounded/missing factory accepted")
	}
}

func TestGraphManagerInvalidationWakesConstructorWaiter(t *testing.T) {
	started, finish := make(chan struct{}), make(chan struct{})
	m, _ := newTenantGraphManager(1, activeGraphAuthority, func(context.Context, tenant.ID) (graphResource, error) {
		close(started)
		<-finish
		return &countedGraph{}, nil
	})
	result := make(chan error, 1)
	go func() {
		l, err := m.Acquire(context.Background(), tenant.Local)
		if l != nil {
			l.Release()
		}
		result <- err
	}()
	<-started
	m.Invalidate(tenant.Local)
	select {
	case err := <-result:
		if err == nil {
			t.Error("invalidated constructor admitted waiter")
		}
	case <-time.After(100 * time.Millisecond):
		t.Error("invalidation left acquire waiting on uncooperative constructor")
	}
	close(finish)
	shutdownGraphs(t, m)
}

type slowCloseGraph struct {
	countedGraph
	closing, finish chan struct{}
}

func (g *slowCloseGraph) Close() { g.countedGraph.Close(); close(g.closing); <-g.finish }
func TestGraphManagerClosingResourceRetainsCapacity(t *testing.T) {
	g := &slowCloseGraph{closing: make(chan struct{}), finish: make(chan struct{})}
	var builds atomic.Int32
	m, _ := newTenantGraphManager(1, activeGraphAuthority, func(context.Context, tenant.ID) (graphResource, error) {
		if builds.Add(1) == 1 {
			return g, nil
		}
		return &countedGraph{}, nil
	})
	acquireGraph(t, m, tenant.Local).Release()
	m.Invalidate(tenant.Local)
	<-g.closing
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
	defer cancel()
	if _, err := m.Acquire(ctx, tenant.MustParse("other")); !errors.Is(err, context.DeadlineExceeded) {
		t.Errorf("close released capacity early: %v", err)
	}
	if builds.Load() != 1 {
		t.Error("constructor overlapped closing slot")
	}
	close(g.finish)
	acquireGraph(t, m, tenant.MustParse("other")).Release()
	shutdownGraphs(t, m)
	if g.closes.Load() != 1 {
		t.Fatal("close not exactly once")
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	if len(m.entries) != 0 {
		t.Fatal("shutdown retained tenant tombstones")
	}
}

func TestGraphManagerConcurrentShutdownAndAcquire(t *testing.T) {
	m, _ := newTenantGraphManager(3, activeGraphAuthority, func(context.Context, tenant.ID) (graphResource, error) { return &countedGraph{}, nil })
	start := make(chan struct{})
	var wg sync.WaitGroup
	for i := range 40 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			<-start
			if i%4 == 0 {
				shutdownGraphs(t, m)
				return
			}
			l, err := m.Acquire(context.Background(), tenant.MustParse(fmt.Sprintf("tenant-%d", i)))
			if err == nil {
				l.Release()
				l.Release()
			}
		}()
	}
	close(start)
	wg.Wait()
	shutdownGraphs(t, m)
}

func TestGraphManagerShutdownJoinsAuthorityCheck(t *testing.T) {
	started, finish, cancelled := make(chan struct{}), make(chan struct{}), make(chan struct{})
	m, _ := newTenantGraphManager(1, func(ctx context.Context, id tenant.ID) (graphPermit, error) {
		close(started)
		select {
		case <-ctx.Done():
			close(cancelled)
		case <-finish:
			return graphPermit{}, errGraphUnavailable
		}
		<-finish
		return graphPermit{}, ctx.Err()
	}, func(context.Context, tenant.ID) (graphResource, error) {
		t.Error("constructed after shutdown")
		return &countedGraph{}, nil
	})
	done := make(chan struct{})
	go func() {
		defer close(done)
		l, _ := m.Acquire(context.Background(), tenant.Local)
		if l != nil {
			l.Release()
		}
	}()
	<-started
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
	defer cancel()
	if err := m.Shutdown(ctx); !errors.Is(err, context.DeadlineExceeded) {
		t.Errorf("shutdown abandoned authority DB read: %v", err)
	}
	select {
	case <-cancelled:
	case <-time.After(100 * time.Millisecond):
		t.Error("shutdown did not cancel authority check")
	}
	close(finish)
	<-done
	shutdownGraphs(t, m)
}

func TestGraphManagerCancelledAuthorityCallerDoesNotInvalidateHeldGraph(t *testing.T) {
	started := make(chan struct{})
	var block atomic.Bool
	m, _ := newTenantGraphManager(1, func(ctx context.Context, id tenant.ID) (graphPermit, error) {
		if block.Load() {
			close(started)
			<-ctx.Done()
			return graphPermit{}, ctx.Err()
		}
		return activeGraphAuthority(ctx, id)
	}, func(context.Context, tenant.ID) (graphResource, error) { return &countedGraph{}, nil })
	defer shutdownGraphs(t, m)
	l := acquireGraph(t, m, tenant.Local)
	defer l.Release()
	block.Store(true)
	ctx, cancel := context.WithCancel(context.Background())
	result := make(chan error, 1)
	go func() { _, err := m.Acquire(ctx, tenant.Local); result <- err }()
	<-started
	cancel()
	if err := <-result; !errors.Is(err, context.Canceled) {
		t.Fatalf("caller cancel lost: %v", err)
	}
	m.mu.Lock()
	retiring := m.entries[tenant.Local].retiring
	m.mu.Unlock()
	if retiring {
		t.Fatal("cancelled caller retired another request's graph")
	}
}
func acquireGraph(t *testing.T, m *tenantGraphManager, id tenant.ID) *graphLease {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	t.Cleanup(cancel)
	l, err := m.Acquire(ctx, id)
	if err != nil {
		t.Fatal(err)
	}
	return l
}
func shutdownGraphs(t *testing.T, m *tenantGraphManager) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	if err := m.Shutdown(ctx); err != nil {
		t.Fatal(err)
	}
}

func TestGraphManagerSingleConstructionAndIdempotentLeases(t *testing.T) {
	g := &countedGraph{}
	var builds atomic.Int32
	m, err := newTenantGraphManager(2, activeGraphAuthority, func(context.Context, tenant.ID) (graphResource, error) {
		builds.Add(1)
		return g, nil
	})
	if err != nil {
		t.Fatal(err)
	}
	defer shutdownGraphs(t, m)
	var wg sync.WaitGroup
	for range 32 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			l, err := m.Acquire(context.Background(), tenant.Local)
			if err != nil {
				t.Error(err)
				return
			}
			if l.Graph != g {
				t.Error("wrong graph")
			}
			l.Release()
			l.Release()
		}()
	}
	wg.Wait()
	if builds.Load() != 1 {
		t.Fatalf("builds=%d, want one shared construction", builds.Load())
	}
	shutdownGraphs(t, m)
	if g.closes.Load() != 1 {
		t.Fatalf("closes=%d, want exactly one", g.closes.Load())
	}
}
