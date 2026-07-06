package runner

import (
	"context"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/huynle/brain-api/internal/types"
)

// TestTaskRunner_Dispatch_UsesWorkerPoolWhenStarted confirms that when
// TaskRunner has been Start()ed (dispatch pool running), incoming
// CommandDispatch messages are processed via the async worker pool
// rather than inline on the caller's goroutine. We prove this by
// intercepting the handler through a test seam and observing that
// handleCommand returns before the handler starts executing.
func TestTaskRunner_Dispatch_UsesWorkerPoolWhenStarted(t *testing.T) {
	client := newMockClient()
	client.readyTasks["proj-a"] = []types.ResolvedTask{*testTask("task1", "proj-a")}
	executor := newMockExecutor()
	processMgr := newMockProcessMgr()
	stateMgr := newMockStateMgr()
	cfg := testRunnerConfig()
	cfg.Capabilities = []string{"dispatch_push"}
	tr := NewTaskRunner(TaskRunnerOptions{
		Projects: []string{"proj-a"},
		Config:   cfg, Mode: ExecutionModeHeadless,
		Client: client, Executor: executor,
		ProcessMgr: processMgr, StateMgr: stateMgr,
	})

	// Swap in a slow handler that blocks until we release it.
	release := make(chan struct{})
	var started atomic.Int32
	handler := func(ctx context.Context, cmd RunnerCommand) {
		started.Add(1)
		<-release
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	tr.startDispatchPoolWithHandler(ctx, handler, 2, 4)
	defer func() { close(release); tr.stopDispatchPool(context.Background()) }()

	// Submit a dispatch through handleCommand. The handler should NOT
	// have started by the time handleCommand returns — proving async.
	start := time.Now()
	tr.handleCommand(ctx, RunnerCommand{
		Type: CommandDispatch, ProjectID: "proj-a", TaskID: "t1", LeaseID: "l1",
	})
	elapsed := time.Since(start)

	if elapsed > 50*time.Millisecond {
		t.Fatalf("handleCommand blocked for %v — dispatch appears to be running inline, not via pool", elapsed)
	}

	// Wait briefly for the worker to pick up the job and enter the handler.
	deadline := time.Now().Add(500 * time.Millisecond)
	for time.Now().Before(deadline) && started.Load() == 0 {
		time.Sleep(5 * time.Millisecond)
	}
	if got := started.Load(); got != 1 {
		t.Fatalf("handler started = %d, want 1 (worker should have picked up the submitted command)", got)
	}
}

// TestTaskRunner_Dispatch_InlineWhenPoolNotStarted confirms that when
// the pool has NOT been started (existing test scenarios that call
// handleCommand directly), dispatch falls back to synchronous inline
// execution so tests observe ack/spawn side effects immediately.
func TestTaskRunner_Dispatch_InlineWhenPoolNotStarted(t *testing.T) {
	client := newMockClient()
	client.readyTasks["proj-a"] = []types.ResolvedTask{*testTask("task1", "proj-a")}
	executor := newMockExecutor()
	processMgr := newMockProcessMgr()
	stateMgr := newMockStateMgr()
	cfg := testRunnerConfig()
	cfg.Capabilities = []string{"dispatch_push"}
	tr := NewTaskRunner(TaskRunnerOptions{
		Projects: []string{"proj-a"},
		Config:   cfg, Mode: ExecutionModeHeadless,
		Client: client, Executor: executor,
		ProcessMgr: processMgr, StateMgr: stateMgr,
	})
	// Note: no startDispatchPool call.

	tr.handleCommand(context.Background(), RunnerCommand{
		Type: CommandDispatch, ProjectID: "proj-a", TaskID: "task1", LeaseID: "lease-1",
	})

	// Immediately after handleCommand returns, ack must have been called
	// (proving inline execution when the pool isn't running).
	if got := len(client.getAckCalls()); got != 1 {
		t.Fatalf("ack calls = %d, want 1 (dispatch should run inline when pool is not started)", got)
	}
}

// TestTaskRunner_Dispatch_BacklogFullRejectsLease confirms that when
// the pool queue AND all workers are saturated, a subsequent
// handleCommand results in the lease being rejected synchronously with
// a dispatch_backlog_full reason — instead of silently dropping like
// the old non-blocking SSE channel send did.
func TestTaskRunner_Dispatch_BacklogFullRejectsLease(t *testing.T) {
	client := newMockClient()
	executor := newMockExecutor()
	processMgr := newMockProcessMgr()
	stateMgr := newMockStateMgr()
	cfg := testRunnerConfig()
	cfg.Capabilities = []string{"dispatch_push"}
	tr := NewTaskRunner(TaskRunnerOptions{
		Projects: []string{"proj-a"},
		Config:   cfg, Mode: ExecutionModeHeadless,
		Client: client, Executor: executor,
		ProcessMgr: processMgr, StateMgr: stateMgr,
	})

	// Blocking handler pins the single worker. Only the first
	// invocation signals firstEntered; subsequent invocations block
	// waiting for release just like the first.
	release := make(chan struct{})
	firstEntered := make(chan struct{})
	var once sync.Once
	handler := func(ctx context.Context, cmd RunnerCommand) {
		once.Do(func() { close(firstEntered) })
		<-release
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	// Tiny pool: 1 worker + 1 queue slot → 3rd submit overflows.
	tr.startDispatchPoolWithHandler(ctx, handler, 1, 1)
	defer func() { close(release); tr.stopDispatchPool(context.Background()) }()

	// First submit: worker picks it up and blocks in handler.
	tr.handleCommand(ctx, RunnerCommand{
		Type: CommandDispatch, ProjectID: "proj-a", TaskID: "t1", LeaseID: "l1",
	})
	// Ensure the worker is actually inside the handler before proceeding.
	select {
	case <-firstEntered:
	case <-time.After(500 * time.Millisecond):
		t.Fatal("first dispatch handler never entered")
	}

	// Second submit: queued (queue depth 1).
	tr.handleCommand(ctx, RunnerCommand{
		Type: CommandDispatch, ProjectID: "proj-a", TaskID: "t2", LeaseID: "l2",
	})

	// Third submit: overflow — expect backlog-full rejection.
	tr.handleCommand(ctx, RunnerCommand{
		Type: CommandDispatch, ProjectID: "proj-a", TaskID: "t3", LeaseID: "l3",
	})

	// Wait for the reject HTTP call to land.
	deadline := time.Now().Add(500 * time.Millisecond)
	var rejects []dispatchRejectCall
	for time.Now().Before(deadline) {
		rejects = client.getRejectCalls()
		if len(rejects) >= 1 {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}

	if len(rejects) != 1 {
		t.Fatalf("reject calls = %d, want 1", len(rejects))
	}
	if rejects[0].Reason.Code != "dispatch_backlog_full" {
		t.Fatalf("reject reason code = %q, want %q", rejects[0].Reason.Code, "dispatch_backlog_full")
	}
	if rejects[0].TaskID != "t3" {
		t.Fatalf("rejected task = %q, want %q (only the overflow should be rejected)", rejects[0].TaskID, "t3")
	}
}
