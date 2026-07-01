package runner

import (
	"context"
	"errors"
	"sync"
	"sync/atomic"
)

// dispatchPool is a bounded worker pool that decouples SSE command
// consumption from the slow HTTP + spawn work of processing a dispatch
// command.
//
// Design constraints (see fix/dispatch-worker-pool):
//
//   - Fixed number of worker goroutines. Bounded to keep runtime cost
//     predictable regardless of dispatch burst size.
//   - Bounded queue in front of workers. When full, Submit returns
//     errDispatchPoolFull so the caller can synchronously reject the
//     lease with a meaningful reason (surfacing backpressure to the
//     scheduler) instead of silently dropping like the old non-blocking
//     select on the SSE command channel.
//   - Stop drains in-flight work. Handlers already running when Stop is
//     called finish; queued commands not yet picked up are discarded
//     (Submit after Stop returns errDispatchPoolStopped).
//
// This type is intentionally small and independent from TaskRunner so it
// can be unit-tested without needing a full runner harness.
type dispatchPool struct {
	handler func(ctx context.Context, cmd RunnerCommand)
	jobs    chan RunnerCommand
	workers int

	backlogFull atomic.Int64 // total count of Submit calls that returned errDispatchPoolFull

	stopOnce sync.Once
	stopped  chan struct{} // closed when Stop begins
	done     chan struct{} // closed when all workers exit

	ctx    context.Context
	cancel context.CancelFunc
}

type dispatchPoolConfig struct {
	Workers    int
	QueueDepth int
	Handler    func(ctx context.Context, cmd RunnerCommand)
}

var (
	errDispatchPoolFull    = errors.New("dispatch pool queue full")
	errDispatchPoolStopped = errors.New("dispatch pool stopped")
)

func newDispatchPool(cfg dispatchPoolConfig) *dispatchPool {
	if cfg.Workers < 1 {
		cfg.Workers = 1
	}
	if cfg.QueueDepth < 0 {
		cfg.QueueDepth = 0
	}
	return &dispatchPool{
		handler: cfg.Handler,
		jobs:    make(chan RunnerCommand, cfg.QueueDepth),
		workers: cfg.Workers,
		stopped: make(chan struct{}),
		done:    make(chan struct{}),
	}
}

// Start spawns worker goroutines. The provided ctx is used as the parent
// context for handler invocations; when it is cancelled, workers exit
// after finishing their current job.
func (p *dispatchPool) Start(ctx context.Context) {
	p.ctx, p.cancel = context.WithCancel(ctx)
	var wg sync.WaitGroup
	for i := 0; i < p.workers; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for {
				select {
				case <-p.ctx.Done():
					return
				case cmd, ok := <-p.jobs:
					if !ok {
						return
					}
					p.handler(p.ctx, cmd)
				}
			}
		}()
	}
	go func() {
		wg.Wait()
		close(p.done)
	}()
}

// Submit enqueues a command for asynchronous processing. Returns
// errDispatchPoolFull if the queue is at capacity and all workers are
// busy, or errDispatchPoolStopped if Stop has been called.
func (p *dispatchPool) Submit(cmd RunnerCommand) error {
	// Check stopped first so Submit-after-Stop is deterministic.
	select {
	case <-p.stopped:
		return errDispatchPoolStopped
	default:
	}
	select {
	case <-p.stopped:
		return errDispatchPoolStopped
	case p.jobs <- cmd:
		return nil
	default:
		p.backlogFull.Add(1)
		return errDispatchPoolFull
	}
}

// Stop stops accepting new work and blocks until all in-flight handlers
// have returned. Queued-but-not-yet-picked-up commands are discarded.
// Safe to call multiple times.
func (p *dispatchPool) Stop(ctx context.Context) {
	p.stopOnce.Do(func() {
		close(p.stopped)
		// Close jobs so workers exit after draining their current job.
		close(p.jobs)
	})
	select {
	case <-p.done:
	case <-ctx.Done():
	}
}

// dispatchPoolStats captures a point-in-time snapshot of the pool for
// observability (surfaced via heartbeat Stats and brain_runner_status).
type dispatchPoolStats struct {
	Workers          int   `json:"workers"`
	QueueDepth       int   `json:"queueDepth"`
	QueueLen         int   `json:"queueLen"`
	QueueCapacity    int   `json:"queueCapacity"`
	BacklogFullCount int64 `json:"backlogFullCount"`
}

// Stats returns a snapshot of pool state for observability.
func (p *dispatchPool) Stats() dispatchPoolStats {
	return dispatchPoolStats{
		Workers:          p.workers,
		QueueDepth:       cap(p.jobs),
		QueueLen:         len(p.jobs),
		QueueCapacity:    cap(p.jobs),
		BacklogFullCount: p.backlogFull.Load(),
	}
}

// -----------------------------------------------------------------------------
// TaskRunner integration
// -----------------------------------------------------------------------------

// dispatchPoolSize computes the worker pool size and queue depth for a
// given MaxParallel setting. Workers scale with capacity plus a small
// slack so fast-reject dispatches (paused, expired, task_not_found)
// don't stall behind slow spawns; queue absorbs bursts without pushing
// backpressure into the SSE channel.
func dispatchPoolSize(maxParallel int) (workers, queueDepth int) {
	workers = maxParallel + 2
	if workers < 4 {
		workers = 4
	}
	queueDepth = 32
	return workers, queueDepth
}

// startDispatchPool starts the runner's dispatch pool with the
// production handler (handleDispatchCommand). Idempotent — a second
// call while a pool is already running is a no-op.
func (tr *TaskRunner) startDispatchPool(ctx context.Context) {
	workers, queueDepth := dispatchPoolSize(tr.getMaxParallel())
	tr.startDispatchPoolWithHandler(ctx, tr.handleDispatchCommand, workers, queueDepth)
}

// startDispatchPoolWithHandler installs a custom handler pool. Used by
// startDispatchPool with the production handler, and by tests with a
// synthetic handler to observe async submission without depending on
// full HTTP + spawn plumbing.
func (tr *TaskRunner) startDispatchPoolWithHandler(ctx context.Context, handler func(context.Context, RunnerCommand), workers, queueDepth int) {
	tr.dispatchPoolMu.Lock()
	if tr.dispatchPool != nil {
		tr.dispatchPoolMu.Unlock()
		return
	}
	pool := newDispatchPool(dispatchPoolConfig{
		Workers:    workers,
		QueueDepth: queueDepth,
		Handler:    handler,
	})
	pool.Start(ctx)
	tr.dispatchPool = pool
	tr.dispatchPoolMu.Unlock()
}

// stopDispatchPool stops the runner's dispatch pool if one is running,
// blocking until in-flight handlers complete or ctx is cancelled. Safe
// to call when no pool was ever started.
func (tr *TaskRunner) stopDispatchPool(ctx context.Context) {
	tr.dispatchPoolMu.Lock()
	pool := tr.dispatchPool
	tr.dispatchPool = nil
	tr.dispatchPoolMu.Unlock()
	if pool != nil {
		pool.Stop(ctx)
	}
}

// getDispatchPool returns the current pool (may be nil) under the RW
// lock. Callers must not retain the pointer beyond a single operation
// — Stop may swap it out concurrently.
func (tr *TaskRunner) getDispatchPool() *dispatchPool {
	tr.dispatchPoolMu.RLock()
	defer tr.dispatchPoolMu.RUnlock()
	return tr.dispatchPool
}
