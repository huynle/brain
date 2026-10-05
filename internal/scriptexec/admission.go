package scriptexec

import (
	"context"
	"errors"
	"sync"
	"unicode/utf8"
)

var errWorkerAdmission = errors.New("worker admission unavailable")

// Descriptive keys from a future trusted composition, not identity or authority.
type workerBinding struct{ tenant, principal string }
type workerAdmissionLimits struct{ global, tenant, principal, queued int }
type workerTicket struct {
	binding workerBinding
	ready   chan struct{}
	started bool
	err     error
}

// Inactive local capacity/lifetime primitive. No goroutine, process, scan or DB
// transaction starts during construction. Every slot reserves one FIXED worker
// policy's CPU/memory ceiling until its owning callback has Waited and returned.
// This is NOT durable admission or authoritative multi-server quota. Callers may
// not release a slot merely because cancellation was requested or a lease expired.
type localWorkerPool struct {
	mu              sync.Mutex
	limits          workerAdmissionLimits
	queue           []*workerTicket
	waiting, active int
	tenants         map[string]int
	principals      map[workerBinding]int
	lastTenant      string
	lastPrincipal   map[string]string
	closed, joined  bool
	lifetime        context.Context
	cancel          context.CancelCauseFunc
	idle            chan struct{}
}

func newLocalWorkerPool(l workerAdmissionLimits) (*localWorkerPool, error) {
	if l.global < 1 || l.global > 64 || l.tenant < 1 || l.tenant > l.global || l.principal < 1 || l.principal > l.tenant || l.queued < 1 || l.queued > 1024 {
		return nil, errWorkerAdmission
	}
	ctx, cancel := context.WithCancelCause(context.Background())
	return &localWorkerPool{limits: l, tenants: map[string]int{}, principals: map[workerBinding]int{}, lastPrincipal: map[string]string{}, lifetime: ctx, cancel: cancel, idle: make(chan struct{})}, nil
}

func (p *localWorkerPool) run(ctx context.Context, b workerBinding, runAndWait func(context.Context) error) error {
	if err := ctx.Err(); err != nil {
		return context.Cause(ctx)
	}
	if runAndWait == nil || b.tenant == "" || b.principal == "" || len(b.tenant) > 256 || len(b.principal) > 256 || !utf8.ValidString(b.tenant) || !utf8.ValidString(b.principal) {
		return errWorkerAdmission
	}
	t := &workerTicket{binding: b, ready: make(chan struct{})}
	p.mu.Lock()
	if p.closed || p.waiting >= p.limits.queued {
		p.mu.Unlock()
		return errWorkerAdmission
	}
	p.queue = append(p.queue, t)
	p.waiting++
	p.scheduleLocked()
	p.mu.Unlock()
	select {
	case <-t.ready:
	case <-ctx.Done():
	}
	p.mu.Lock()
	if ctx.Err() != nil || t.err != nil {
		if t.started {
			p.releaseLocked(b)
		} else {
			p.removeLocked(t)
			p.scheduleLocked()
		}
		p.mu.Unlock()
		if ctx.Err() != nil {
			return context.Cause(ctx)
		}
		return t.err
	}
	p.mu.Unlock()
	defer func() { p.mu.Lock(); defer p.mu.Unlock(); p.releaseLocked(b) }()
	work, cancel := context.WithCancelCause(ctx)
	stop := context.AfterFunc(p.lifetime, func() { cancel(errWorkerAdmission) })
	defer stop()
	defer cancel(nil)
	// AfterFunc is asynchronous; observe an already-closed pool synchronously.
	if p.lifetime.Err() != nil {
		cancel(errWorkerAdmission)
	}
	if work.Err() != nil {
		return context.Cause(work)
	}
	err := runAndWait(work)
	if work.Err() != nil {
		return context.Cause(work)
	}
	return err
}

func (p *localWorkerPool) close(ctx context.Context) error {
	p.mu.Lock()
	p.closed = true
	p.cancel(errWorkerAdmission)
	p.scheduleLocked()
	p.mu.Unlock()
	select {
	case <-p.idle:
		return nil
	case <-ctx.Done():
		return context.Cause(ctx)
	}
}

func (p *localWorkerPool) removeLocked(t *workerTicket) {
	for i, candidate := range p.queue {
		if candidate == t {
			copy(p.queue[i:], p.queue[i+1:])
			p.queue[len(p.queue)-1] = nil
			p.queue = p.queue[:len(p.queue)-1]
			p.waiting--
			break
		}
	}
}

func (p *localWorkerPool) releaseLocked(b workerBinding) {
	p.active--
	p.tenants[b.tenant]--
	p.principals[b]--
	if p.tenants[b.tenant] == 0 {
		delete(p.tenants, b.tenant)
	}
	if p.principals[b] == 0 {
		delete(p.principals, b)
	}
	p.scheduleLocked()
}

func (p *localWorkerPool) scheduleLocked() {
	if p.closed {
		for _, t := range p.queue {
			t.err = errWorkerAdmission
			close(t.ready)
		}
		clear(p.queue)
		p.queue = nil
		p.waiting = 0
		if p.active == 0 && !p.joined {
			p.joined = true
			close(p.idle)
		}
	} else {
		for p.active < p.limits.global {
			// FIFO among equally eligible bindings, with tenant rotation first and
			// principal rotation within that tenant. Saturated owners cannot block
			// independent owners; fixed worker weights avoid large-job starvation.
			pick := -1
			for i, t := range p.queue {
				b := t.binding
				if p.tenants[b.tenant] >= p.limits.tenant || p.principals[b] >= p.limits.principal {
					continue
				}
				if pick < 0 {
					pick = i
					continue
				}
				old := p.queue[pick].binding
				if old.tenant == p.lastTenant && b.tenant != p.lastTenant {
					pick = i
					continue
				}
				if old.tenant == b.tenant && old.principal == p.lastPrincipal[b.tenant] && b.principal != p.lastPrincipal[b.tenant] {
					pick = i
				}
			}
			if pick < 0 {
				break
			}
			t := p.queue[pick]
			p.removeLocked(t)
			b := t.binding
			t.started = true
			p.active++
			p.tenants[b.tenant]++
			p.principals[b]++
			p.lastTenant = b.tenant
			p.lastPrincipal[b.tenant] = b.principal
			close(t.ready)
		}
	}
	// Retain fairness history only while an owner has outstanding work. State
	// cardinality stays bounded by active slots plus the bounded waiting queue.
	for tenant := range p.lastPrincipal {
		if p.tenants[tenant] > 0 {
			continue
		}
		pending := false
		for _, t := range p.queue {
			if t.binding.tenant == tenant {
				pending = true
				break
			}
		}
		if !pending {
			delete(p.lastPrincipal, tenant)
		}
	}
}
