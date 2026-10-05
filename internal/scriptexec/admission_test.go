package scriptexec

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"runtime"
	"testing"
	"time"
)

func poolReceive[T any](t *testing.T, ch <-chan T) T {
	t.Helper()
	select {
	case v := <-ch:
		return v
	case <-time.After(3 * time.Second):
		t.Fatal("pool state transition timed out")
		var zero T
		return zero
	}
}
func poolWaitQueued(t *testing.T, p *localWorkerPool, n int) {
	t.Helper()
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		p.mu.Lock()
		got := p.waiting
		p.mu.Unlock()
		if got == n {
			return
		}
		runtime.Gosched()
	}
	t.Fatalf("queue never reached %d", n)
}

func TestLocalWorkerAdmissionFairnessAndBounds(t *testing.T) {
	p, err := newLocalWorkerPool(workerAdmissionLimits{1, 1, 1, 4})
	if err != nil {
		t.Fatal(err)
	}
	defer func() { ctx, c := context.WithTimeout(context.Background(), time.Second); defer c(); _ = p.close(ctx) }()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	started := make(chan string, 8)
	release := make(chan struct{}, 8)
	results := make(chan error, 8)
	launch := func(name, tenant, principal string) {
		go func() {
			results <- p.run(ctx, workerBinding{tenant, principal}, func(ctx context.Context) error {
				started <- name
				select {
				case <-release:
					return nil
				case <-ctx.Done():
					return context.Cause(ctx)
				}
			})
		}()
	}
	launch("A1", "A", "one")
	if got := poolReceive(t, started); got != "A1" {
		t.Fatal(got)
	}
	launch("A2", "A", "one")
	poolWaitQueued(t, p, 1)
	launch("A3", "A", "two")
	poolWaitQueued(t, p, 2)
	launch("B1", "B", "one")
	poolWaitQueued(t, p, 3)
	launch("B2", "B", "one")
	poolWaitQueued(t, p, 4)
	if err := p.run(ctx, workerBinding{"C", "one"}, func(context.Context) error { t.Error("overflow ran"); return nil }); !errors.Is(err, errWorkerAdmission) {
		t.Fatalf("queue overflow: %v", err)
	}
	for _, want := range []string{"B1", "A3", "B2", "A2"} {
		release <- struct{}{}
		if err := poolReceive(t, results); err != nil {
			t.Fatal(err)
		}
		if got := poolReceive(t, started); got != want {
			t.Fatalf("fair order got %s want %s", got, want)
		}
	}
	release <- struct{}{}
	if err := poolReceive(t, results); err != nil {
		t.Fatal(err)
	}
}

func TestLocalWorkerAdmissionCancellationAndShutdownJoin(t *testing.T) {
	p, err := newLocalWorkerPool(workerAdmissionLimits{2, 1, 1, 4})
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancelCause(context.Background())
	defer cancel(nil)
	started := make(chan struct{})
	cancelSeen := make(chan struct{})
	waited := make(chan struct{})
	result := make(chan error, 1)
	go func() {
		result <- p.run(ctx, workerBinding{"A", "one"}, func(ctx context.Context) error {
			close(started)
			<-ctx.Done()
			close(cancelSeen)
			<-waited
			return context.Cause(ctx)
		})
	}()
	poolReceive(t, started)
	queuedCtx, queuedCancel := context.WithCancelCause(context.Background())
	defer queuedCancel(nil)
	queuedResult := make(chan error, 1)
	go func() {
		queuedResult <- p.run(queuedCtx, workerBinding{"A", "two"}, func(context.Context) error { t.Error("tenant limit or queue cancellation violated"); return nil })
	}()
	poolWaitQueued(t, p, 1)
	reason := errors.New("caller cancelled")
	queuedCancel(reason)
	if err := poolReceive(t, queuedResult); !errors.Is(err, reason) {
		t.Fatalf("lost cause: %v", err)
	}
	poolWaitQueued(t, p, 0)
	closedCtx, closedCancel := context.WithCancel(context.Background())
	closedCancel()
	if err := p.close(closedCtx); !errors.Is(err, context.Canceled) {
		t.Fatalf("shutdown released before Wait: %v", err)
	}
	poolReceive(t, cancelSeen)
	if err := p.run(context.Background(), workerBinding{"B", "one"}, func(context.Context) error { t.Error("post-close worker ran"); return nil }); !errors.Is(err, errWorkerAdmission) {
		t.Fatal(err)
	}
	close(waited)
	if err := poolReceive(t, result); !errors.Is(err, errWorkerAdmission) {
		t.Fatalf("shutdown cause: %v", err)
	}
	if err := p.close(context.Background()); err != nil {
		t.Fatal(err)
	}
}

func TestLocalWorkerAdmissionDistinctBindingsAndPrecancel(t *testing.T) {
	p, err := newLocalWorkerPool(workerAdmissionLimits{3, 2, 1, 4})
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	started := make(chan workerBinding, 3)
	result := make(chan error, 4)
	run := func(b workerBinding) {
		result <- p.run(ctx, b, func(ctx context.Context) error { started <- b; <-ctx.Done(); return context.Cause(ctx) })
	}
	for _, b := range []workerBinding{{"A", "one"}, {"A", "two"}, {"B", "one"}} {
		go run(b)
		poolReceive(t, started)
	}
	go run(workerBinding{"B", "two"})
	poolWaitQueued(t, p, 1)
	if err := p.close(context.Background()); err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 4; i++ {
		if err := poolReceive(t, result); !errors.Is(err, errWorkerAdmission) {
			t.Fatal(err)
		}
	}
	p2, err := newLocalWorkerPool(workerAdmissionLimits{1, 1, 1, 1})
	if err != nil {
		t.Fatal(err)
	}
	cancel()
	if err := p2.run(ctx, workerBinding{"A", "one"}, func(context.Context) error { t.Error("pre-cancel launched"); return nil }); !errors.Is(err, context.Canceled) {
		t.Fatal(err)
	}
	if err := p2.close(context.Background()); err != nil {
		t.Fatal(err)
	}
}

func TestLocalWorkerAdmissionRejectsInvalidLimits(t *testing.T) {
	for _, limits := range []workerAdmissionLimits{{}, {0, 1, 1, 1}, {65, 1, 1, 1}, {1, 2, 1, 1}, {2, 1, 2, 1}, {1, 1, 1, 1025}, {1, 1, 1, -1}} {
		if _, err := newLocalWorkerPool(limits); err == nil {
			t.Errorf("accepted invalid limits %+v", limits)
		}
	}
}

func TestLocalWorkerAdmissionShutdownActuallyWaitsProcess(t *testing.T) {
	p, err := newLocalWorkerPool(workerAdmissionLimits{1, 1, 1, 1})
	if err != nil {
		t.Fatal(err)
	}
	ready, readyCancel := context.WithCancel(context.Background())
	defer readyCancel()
	cmd := exec.Command(os.Args[0], "-test.run=^TestProcessFixture$")
	cmd.Env = []string{"BRAIN_PROCESS_FIXTURE=cancel"}
	cmd.Stdout = cancelOnWrite{readyCancel}
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	done := make(chan error, 1)
	go func() {
		done <- p.run(ctx, workerBinding{"A", "one"}, func(ctx context.Context) error { return runWorkerProcess(ctx, cmd) })
	}()
	poolReceive(t, ready.Done())
	if err := p.close(ctx); err != nil {
		t.Fatal(err)
	}
	if err := poolReceive(t, done); !errors.Is(err, errWorkerAdmission) {
		t.Fatal(err)
	}
	if cmd.ProcessState == nil || cmd.ProcessState.Success() {
		t.Fatal("shutdown did not kill and Wait")
	}
	if err := cmd.Process.Kill(); !errors.Is(err, os.ErrProcessDone) {
		t.Fatalf("still signalable: %v", err)
	}
	if ctx.Err() != nil {
		t.Fatal("outer deadline used instead of shutdown")
	}
}
