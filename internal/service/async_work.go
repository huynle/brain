package service

import (
	"context"
	"sync"
)

// asyncWork owns detached work, not the repositories it uses. The zero value is
// ready to use. Closing fences admission before cancellation and joining, so a
// concurrent submit cannot race Wait with a new positive Add.
type asyncWork struct {
	mu     sync.Mutex
	ctx    context.Context
	cancel context.CancelFunc
	closed bool
	wg     sync.WaitGroup
}

func (w *asyncWork) goRun(fn func(context.Context)) {
	w.mu.Lock()
	if w.closed {
		w.mu.Unlock()
		return
	}
	if w.ctx == nil {
		w.ctx, w.cancel = context.WithCancel(context.Background())
	}
	ctx := w.ctx
	w.wg.Add(1)
	w.mu.Unlock()
	go func() { defer w.wg.Done(); fn(ctx) }()
}

func (w *asyncWork) close() {
	w.mu.Lock()
	w.closed = true
	if w.cancel != nil {
		w.cancel()
	}
	w.mu.Unlock()
	w.wg.Wait()
}

// wait is a nonterminal drain; callers must stop submitting while waiting.
func (w *asyncWork) wait() { w.wg.Wait() }
