package apiserver

import (
	"context"
	"errors"
	"net/http"
	"sync"

	"github.com/huynle/brain-api/internal/tenant"
)

var errGraphUnavailable = errors.New("tenant graph unavailable")

type graphResource interface {
	http.Handler
	Close()
}

// graphPermit is a lifecycle snapshot, NOT a principal authorization grant.
// The authoritative owner must cancel Lifetime on suspension/invalidation and
// issue a fresh, nonzero Generation on every reactivation/replacement. This
// includes transitions while the tenant has no resident graph. Do not implement
// authority using cache residency or caller headers. P6 supplies real authority.
type graphPermit struct {
	Generation uint64
	Lifetime   context.Context
}
type graphAuthority func(context.Context, tenant.ID) (graphPermit, error)

// Factories borrow shared storage. They must return a fresh resource, clean up
// partial failures, honor cancellation, and never launch residency-owned workers.
// A nonnil resource returned with an error is still closed by the manager.
type graphFactory func(context.Context, tenant.ID) (graphResource, error)

// One slot per tenant includes building, leased, retiring AND closing resources.
// Retired slots remain until Close returns, preventing duplicate live graphs.
// Waiting callers allocate no registry entries/tombstones. Capacity is explicit;
// no production default is selected before phase-3 measurements.
type tenantGraphManager struct {
	mu         sync.Mutex
	ctx        context.Context
	cancel     context.CancelFunc
	admissions int
	capacity   int
	authority  graphAuthority
	factory    graphFactory
	entries    map[tenant.ID]*graphSlot
	changed    chan struct{}
	closed     bool
	clock      uint64
}
type graphSlot struct {
	id                          tenant.ID
	permit                      graphPermit
	ctx                         context.Context
	cancel                      context.CancelFunc
	stopAuthority               func() bool
	ready                       chan struct{}
	building, retiring, closing bool
	graph                       graphResource
	err                         error
	refs                        int
	used                        uint64
}
type graphLease struct {
	Graph   graphResource
	Context context.Context
	once    sync.Once
	release func()
}

func (l *graphLease) Release() {
	if l != nil {
		l.once.Do(l.release)
	}
}

func newTenantGraphManager(capacity int, authority graphAuthority, factory graphFactory) (*tenantGraphManager, error) {
	if capacity < 1 || authority == nil || factory == nil {
		return nil, errGraphUnavailable
	}
	ctx, cancel := context.WithCancel(context.Background())
	return &tenantGraphManager{ctx: ctx, cancel: cancel, capacity: capacity, authority: authority, factory: factory, entries: make(map[tenant.ID]*graphSlot), changed: make(chan struct{})}, nil
}
func validGraphPermit(p graphPermit) bool {
	return p.Generation != 0 && p.Lifetime != nil && p.Lifetime.Err() == nil
}
func (m *tenantGraphManager) notifyLocked() { close(m.changed); m.changed = make(chan struct{}) }

func (m *tenantGraphManager) Acquire(ctx context.Context, id tenant.ID) (*graphLease, error) {
	if !id.Valid() {
		return nil, tenant.ErrNoTenant
	}
	// Track the entire admission, not only constructors: an online authority
	// check may also be using shared storage when shutdown begins.
	m.mu.Lock()
	if m.closed {
		m.mu.Unlock()
		return nil, errGraphUnavailable
	}
	m.admissions++
	m.mu.Unlock()
	requestParent := ctx
	ctx, cancel := context.WithCancel(ctx)
	stop := context.AfterFunc(m.ctx, cancel)
	defer func() {
		stop()
		cancel()
		m.mu.Lock()
		defer m.mu.Unlock()
		m.admissions--
		m.notifyLocked()
	}()
	for {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		m.mu.Lock()
		closed := m.closed
		m.mu.Unlock()
		if closed {
			return nil, errGraphUnavailable
		}
		// Online on EVERY attempt, including cache hits and after admission waits.
		p, err := m.authority(ctx, id)
		// A disconnected caller is not an authoritative lifecycle denial.
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		if err != nil || !validGraphPermit(p) {
			m.Invalidate(id)
			if err != nil {
				return nil, err
			}
			return nil, errGraphUnavailable
		}
		m.mu.Lock()
		if m.closed || !validGraphPermit(p) || ctx.Err() != nil {
			m.mu.Unlock()
			if err := ctx.Err(); err != nil {
				return nil, err
			}
			return nil, errGraphUnavailable
		}
		e := m.entries[id]
		if e != nil && (e.permit.Generation != p.Generation || e.permit.Lifetime.Err() != nil) {
			m.retireLocked(e)
		}
		if e != nil && !e.retiring && !e.building {
			m.clock++
			e.used = m.clock
			e.refs++
			request, cancel := context.WithCancel(requestParent)
			stop := context.AfterFunc(e.ctx, cancel)
			l := &graphLease{Graph: e.graph, Context: request}
			l.release = func() {
				stop()
				cancel()
				m.mu.Lock()
				defer m.mu.Unlock()
				e.refs--
				m.closeIfDrainedLocked(e)
				m.notifyLocked()
			}
			m.mu.Unlock()
			return l, nil
		}
		if e == nil && len(m.entries) >= m.capacity {
			var oldest *graphSlot
			for _, candidate := range m.entries {
				if !candidate.building && !candidate.retiring && candidate.refs == 0 && (oldest == nil || candidate.used < oldest.used) {
					oldest = candidate
				}
			}
			if oldest != nil {
				m.retireLocked(oldest)
			}
		}
		if e == nil && len(m.entries) < m.capacity {
			buildCtx, cancel := context.WithCancel(tenant.Into(context.Background(), id))
			e = &graphSlot{id: id, permit: p, ctx: buildCtx, cancel: cancel, building: true, ready: make(chan struct{})}
			m.entries[id] = e
			e.stopAuthority = context.AfterFunc(p.Lifetime, func() { m.mu.Lock(); defer m.mu.Unlock(); m.retireLocked(e) })
			go m.construct(e)
		}
		changed := m.changed
		if e != nil && e.building {
			ready := e.ready
			m.mu.Unlock()
			select {
			case <-ctx.Done():
				return nil, ctx.Err()
			case <-e.ctx.Done():
				m.mu.Lock()
				err := e.err
				m.mu.Unlock()
				if err != nil {
					return nil, err
				}
				return nil, errGraphUnavailable
			case <-ready:
				// Publication of err precedes closing ready. All concurrent
				// waiters see the same construction failure, without a retry storm.
				if e.err != nil {
					return nil, e.err
				}
			}
		} else {
			m.mu.Unlock()
			select {
			case <-ctx.Done():
				return nil, ctx.Err()
			case <-changed:
			}
		}
	}
}

func (m *tenantGraphManager) construct(e *graphSlot) {
	g, err := m.factory(e.ctx, e.id)
	if err == nil {
		p, checkErr := m.authority(e.ctx, e.id)
		if checkErr != nil {
			err = checkErr
		} else if g == nil || !validGraphPermit(p) || p.Generation != e.permit.Generation {
			err = errGraphUnavailable
		}
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	if err == nil && (m.closed || e.retiring || e.ctx.Err() != nil || !validGraphPermit(e.permit)) {
		err = errGraphUnavailable
	}
	e.graph, e.err, e.building = g, err, false
	m.clock++
	e.used = m.clock
	if err != nil {
		m.retireLocked(e)
	}
	m.closeIfDrainedLocked(e)
	close(e.ready)
	m.notifyLocked()
}

func (m *tenantGraphManager) retireLocked(e *graphSlot) {
	if e.retiring {
		return
	}
	e.retiring = true
	e.cancel()
	e.stopAuthority()
	m.closeIfDrainedLocked(e)
	m.notifyLocked()
}
func (m *tenantGraphManager) closeIfDrainedLocked(e *graphSlot) {
	if !e.retiring || e.building || e.refs != 0 || e.closing {
		return
	}
	e.closing = true
	go func() {
		if e.graph != nil {
			e.graph.Close()
		}
		m.mu.Lock()
		defer m.mu.Unlock()
		delete(m.entries, e.id)
		m.notifyLocked()
	}()
}

// Invalidate retires only the current resident slot. The lifecycle authority
// must also advance/cancel its permit for durable invalidation (including misses).
// Ordinary eviction can use this directly; it never closes a referenced graph.
func (m *tenantGraphManager) Invalidate(id tenant.ID) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if e := m.entries[id]; e != nil {
		m.retireLocked(e)
	}
}

// Shutdown fences admission, cancels constructors/request contexts, waits for
// constructors and released leases, then joins graph Close. A deadline only
// stops this caller's wait: cleanup continues and Shutdown can be retried. The
// owner MUST NOT close the shared DB until Shutdown returns nil. Stop/join all
// independent workers first; those workers must retain leases while using graphs.
func (m *tenantGraphManager) Shutdown(ctx context.Context) error {
	m.mu.Lock()
	m.closed = true
	m.cancel()
	for _, e := range m.entries {
		m.retireLocked(e)
	}
	for len(m.entries) != 0 || m.admissions != 0 {
		changed := m.changed
		m.mu.Unlock()
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-changed:
		}
		m.mu.Lock()
	}
	m.mu.Unlock()
	return nil
}
