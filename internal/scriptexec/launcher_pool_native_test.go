package scriptexec

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

// Real concurrent sealed workers through the production launcher and the local
// fair pool: capacity, queueing, cancellation with kill+Wait, slot recovery and
// join. Runs only inside the opt-in Linux fixture (TestQuickJSLauncherLinux).
func TestNativeLauncherPool(t *testing.T) {
	worker := os.Getenv("BRAIN_NATIVE_WORKER_FIXTURE")
	if worker == "" {
		t.Skip("requires opt-in native Linux worker fixture")
	}
	if runtime.GOOS != "linux" {
		t.Fatal("native Linux fixture only")
	}
	pinned := filepath.Join(t.TempDir(), "brain-script-worker")
	data, err := os.ReadFile(worker)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(pinned, data, 0o555); err != nil {
		t.Fatal(err)
	}
	l, err := newWorkerLauncher(launcherConfig{Enabled: true, WorkerPath: pinned, WorkerSHA256: fileSHA256(t, pinned), WallTimeout: 10 * time.Second})
	if err != nil {
		t.Fatal(err)
	}
	pool, err := newLocalWorkerPool(workerAdmissionLimits{global: 2, tenant: 2, principal: 1, queued: 4})
	if err != nil {
		t.Fatal(err)
	}
	var inCall atomic.Int32

	type outcome struct {
		result json.RawMessage
		report launchReport
		err    error
	}
	// submit runs one execution through pool+launcher. The worker makes one
	// call; the reply is held until release closes or ctx is cancelled.
	submit := func(ctx context.Context, b workerBinding, release <-chan struct{}, called chan<- struct{}) <-chan outcome {
		done := make(chan outcome, 1)
		go func() {
			var o outcome
			o.err = pool.run(ctx, b, func(work context.Context) error {
				session, err := NewProtocolSession(ProtocolLimits{1, 1024, 4096})
				if err != nil {
					return err
				}
				defer session.Retire()
				report, err := l.run(work, `return (await brain.entries.get("x")).value*2;`, func(frame Frame, reply func(Frame) error) error {
					message, e := session.Accept(frame)
					if e != nil {
						return e
					}
					if message.Done {
						o.result = message.Result
						return nil
					}
					inCall.Add(1)
					defer inCall.Add(-1)
					if called != nil {
						called <- struct{}{}
					}
					select {
					case <-release:
					case <-work.Done():
						return context.Cause(work)
					}
					out, e := session.Reply(json.RawMessage(`{"value":21}`))
					if e != nil {
						return e
					}
					return reply(out)
				})
				o.report = report
				return err
			})
			done <- o
		}()
		return done
	}
	waitCall := func(ch <-chan struct{}, name string) {
		select {
		case <-ch:
		case <-time.After(5 * time.Second):
			t.Fatalf("%s never reached its brokered call", name)
		}
	}
	released := make(chan struct{})
	close(released)

	ctxA, cancelA := context.WithCancelCause(context.Background())
	defer cancelA(nil)
	releaseA, releaseB := make(chan struct{}), make(chan struct{})
	calledA, calledB, calledC := make(chan struct{}, 1), make(chan struct{}, 1), make(chan struct{}, 1)
	doneA := submit(ctxA, workerBinding{"t1", "p1"}, releaseA, calledA)
	doneB := submit(context.Background(), workerBinding{"t1", "p2"}, releaseB, calledB)
	waitCall(calledA, "A")
	waitCall(calledB, "B")
	if n := inCall.Load(); n != 2 {
		t.Fatalf("want 2 simultaneous sealed workers in a brokered call, got %d", n)
	}
	doneC := submit(context.Background(), workerBinding{"t2", "p1"}, released, calledC)
	select {
	case <-calledC:
		t.Fatal("third execution started beyond global capacity 2")
	case <-time.After(300 * time.Millisecond):
	}

	reason := errors.New("caller cancelled A")
	start := time.Now()
	cancelA(reason)
	a := <-doneA
	if !errors.Is(a.err, reason) || !a.report.waited || a.result != nil {
		t.Fatalf("A: err=%v report=%+v result=%s", a.err, a.report, a.result)
	}
	c := <-doneC
	if c.err != nil || string(c.result) != "42" || !c.report.attested || !c.report.waited {
		t.Fatalf("C after A's slot recovery: err=%v report=%+v result=%s", c.err, c.report, c.result)
	}
	t.Logf("A cancelled+Waited, queued C ran in recovered slot: result=%s after %v", c.result, time.Since(start))

	close(releaseB)
	b := <-doneB
	if b.err != nil || string(b.result) != "42" || !b.report.waited {
		t.Fatalf("B: err=%v report=%+v result=%s", b.err, b.report, b.result)
	}
	// Same tenant/principal as cancelled A: its principal slot was recovered.
	d := <-submit(context.Background(), workerBinding{"t1", "p1"}, released, nil)
	if d.err != nil || string(d.result) != "42" {
		t.Fatalf("D reusing A's principal: err=%v result=%s", d.err, d.result)
	}
	closeCtx, stop := context.WithTimeout(context.Background(), 2*time.Second)
	defer stop()
	if err := pool.close(closeCtx); err != nil {
		t.Fatalf("pool close/join: %v", err)
	}
	children := ""
	tasks, _ := filepath.Glob("/proc/self/task/*/children")
	for _, task := range tasks {
		if data, err := os.ReadFile(task); err == nil {
			children += strings.TrimSpace(string(data))
		}
	}
	if children != "" {
		t.Fatalf("live child processes remain after join: %q", children)
	}
	t.Logf("pool: 2 concurrent sealed workers, 3rd queued, cancel->kill+Wait->slot recovered, B=42, D=42, close joined, no children")
}
