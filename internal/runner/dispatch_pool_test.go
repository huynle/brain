package runner

import (
	"context"
	"errors"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

// TestDispatchPool_ProcessesSubmittedCommand confirms that a submitted
// command reaches the handler in a worker goroutine. This is the core
// contract: submit returns quickly, work happens asynchronously.
func TestDispatchPool_ProcessesSubmittedCommand(t *testing.T) {
	var handled atomic.Int32
	pool := newDispatchPool(dispatchPoolConfig{
		Workers:    2,
		QueueDepth: 4,
		Handler: func(ctx context.Context, cmd RunnerCommand) {
			handled.Add(1)
		},
	})
	pool.Start(context.Background())
	defer pool.Stop(context.Background())

	if err := pool.Submit(RunnerCommand{Type: CommandDispatch, TaskID: "t1"}); err != nil {
		t.Fatalf("Submit returned error: %v", err)
	}

	// Wait up to 1s for the worker to process
	deadline := time.Now().Add(time.Second)
	for time.Now().Before(deadline) && handled.Load() == 0 {
		time.Sleep(5 * time.Millisecond)
	}

	if got := handled.Load(); got != 1 {
		t.Fatalf("handled = %d, want 1", got)
	}
}

// TestDispatchPool_SubmitReturnsBacklogFullWhenQueueSaturated confirms
// that when both workers AND queue are full, submit returns
// errDispatchPoolFull immediately rather than blocking. This is the
// core backpressure contract that lets the caller synchronously reject
// the lease with a real reason instead of silently queueing.
func TestDispatchPool_SubmitReturnsBacklogFullWhenQueueSaturated(t *testing.T) {
	// Handler blocks until we release it, so workers are pinned.
	release := make(chan struct{})
	var mu sync.Mutex
	started := 0
	handler := func(ctx context.Context, cmd RunnerCommand) {
		mu.Lock()
		started++
		mu.Unlock()
		<-release
	}

	pool := newDispatchPool(dispatchPoolConfig{
		Workers:    1,
		QueueDepth: 2,
		Handler:    handler,
	})
	pool.Start(context.Background())
	defer func() {
		close(release)
		pool.Stop(context.Background())
	}()

	// Pin the single worker with the first submit.
	if err := pool.Submit(RunnerCommand{Type: CommandDispatch, TaskID: "worker"}); err != nil {
		t.Fatalf("first submit: %v", err)
	}

	// Wait for worker to actually pick it up so subsequent submits queue.
	deadline := time.Now().Add(time.Second)
	for time.Now().Before(deadline) {
		mu.Lock()
		s := started
		mu.Unlock()
		if s == 1 {
			break
		}
		time.Sleep(5 * time.Millisecond)
	}

	// Fill the queue: QueueDepth=2 → both submits should succeed.
	for i := 0; i < 2; i++ {
		if err := pool.Submit(RunnerCommand{Type: CommandDispatch, TaskID: "queued"}); err != nil {
			t.Fatalf("queue submit %d: %v", i, err)
		}
	}

	// Next submit should be rejected with backlog-full.
	err := pool.Submit(RunnerCommand{Type: CommandDispatch, TaskID: "overflow"})
	if !errors.Is(err, errDispatchPoolFull) {
		t.Fatalf("Submit overflow err = %v, want errDispatchPoolFull", err)
	}
}

// TestDispatchPool_StopDrainsInFlightWork confirms Stop blocks until
// in-flight handlers finish so no work is lost during shutdown.
func TestDispatchPool_StopDrainsInFlightWork(t *testing.T) {
	var completed atomic.Int32
	release := make(chan struct{})
	handler := func(ctx context.Context, cmd RunnerCommand) {
		<-release
		completed.Add(1)
	}

	pool := newDispatchPool(dispatchPoolConfig{
		Workers:    2,
		QueueDepth: 4,
		Handler:    handler,
	})
	pool.Start(context.Background())

	if err := pool.Submit(RunnerCommand{Type: CommandDispatch, TaskID: "t1"}); err != nil {
		t.Fatalf("submit t1: %v", err)
	}
	if err := pool.Submit(RunnerCommand{Type: CommandDispatch, TaskID: "t2"}); err != nil {
		t.Fatalf("submit t2: %v", err)
	}

	// Let workers pick up work.
	time.Sleep(20 * time.Millisecond)

	// Release handlers, then Stop — expect Stop to wait for completion.
	stopDone := make(chan struct{})
	go func() {
		pool.Stop(context.Background())
		close(stopDone)
	}()

	// Verify Stop hasn't returned yet (workers still blocked on release).
	select {
	case <-stopDone:
		t.Fatal("Stop returned before in-flight work completed")
	case <-time.After(30 * time.Millisecond):
	}

	close(release)

	select {
	case <-stopDone:
	case <-time.After(time.Second):
		t.Fatal("Stop did not return after workers finished")
	}

	if got := completed.Load(); got != 2 {
		t.Fatalf("completed = %d, want 2 (in-flight work must complete before Stop returns)", got)
	}
}

// TestDispatchPool_SubmitAfterStopReturnsError confirms Submit rejects
// commands cleanly after Stop rather than blocking forever or panicking
// on a closed channel.
func TestDispatchPool_SubmitAfterStopReturnsError(t *testing.T) {
	pool := newDispatchPool(dispatchPoolConfig{
		Workers:    1,
		QueueDepth: 2,
		Handler:    func(context.Context, RunnerCommand) {},
	})
	pool.Start(context.Background())
	pool.Stop(context.Background())

	err := pool.Submit(RunnerCommand{Type: CommandDispatch, TaskID: "late"})
	if !errors.Is(err, errDispatchPoolStopped) {
		t.Fatalf("Submit after Stop err = %v, want errDispatchPoolStopped", err)
	}
}
