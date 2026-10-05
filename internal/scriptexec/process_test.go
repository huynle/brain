package scriptexec

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"strings"
	"testing"
	"time"
)

func TestProcessFixture(t *testing.T) {
	switch os.Getenv("BRAIN_PROCESS_FIXTURE") {
	case "cancel":
		fmt.Print("ready")
	case "stderr":
		fmt.Fprint(os.Stderr, strings.Repeat("secret-content", 10000))
	case "success":
		os.Exit(0)
	default:
		return
	}
	// Finite negative control: without cancellation support it exits normally.
	time.Sleep(500 * time.Millisecond)
	os.Exit(0)
}

func TestWorkerProcessLifecycleEdges(t *testing.T) {
	t.Run("pre-cancel preserves reason without starting", func(t *testing.T) {
		ctx, cancel := context.WithCancelCause(context.Background())
		reason := errors.New("retired execution")
		cancel(reason)
		cmd := exec.Command(os.Args[0], "-test.run=^TestProcessFixture$")
		if err := runWorkerProcess(ctx, cmd); !errors.Is(err, reason) || cmd.Process != nil {
			t.Fatalf("pre-cancel: err=%v process=%v", err, cmd.Process)
		}
	})
	t.Run("start failure", func(t *testing.T) {
		cmd := exec.Command(t.TempDir() + "/absent")
		if err := runWorkerProcess(context.Background(), cmd); err == nil || cmd.Process != nil {
			t.Fatalf("start failure: err=%v process=%v", err, cmd.Process)
		}
	})
	t.Run("normal exit", func(t *testing.T) {
		cmd := exec.Command(os.Args[0], "-test.run=^TestProcessFixture$")
		cmd.Env = []string{"BRAIN_PROCESS_FIXTURE=success"}
		if err := runWorkerProcess(context.Background(), cmd); err != nil || !cmd.ProcessState.Success() {
			t.Fatalf("normal exit: err=%v state=%v", err, cmd.ProcessState)
		}
	})
}

func TestDiagnosticSinkExactBudget(t *testing.T) {
	ctx, cancel := context.WithCancelCause(context.Background())
	defer cancel(nil)
	sink := &diagnosticSink{remaining: 4, cancel: cancel}
	for i := 0; i < 2; i++ {
		if n, err := sink.Write([]byte("ab")); n != 2 || err != nil || ctx.Err() != nil {
			t.Fatalf("within budget: n=%d err=%v cause=%v", n, err, context.Cause(ctx))
		}
	}
	if _, err := sink.Write([]byte("x")); !errors.Is(err, errWorkerDiagnosticLimit) || !errors.Is(context.Cause(ctx), errWorkerDiagnosticLimit) {
		t.Fatalf("overflow err=%v cause=%v", err, context.Cause(ctx))
	}
}

func TestWorkerProcessDiagnosticFlood(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	cmd := exec.Command(os.Args[0], "-test.run=^TestProcessFixture$")
	cmd.Env = []string{"BRAIN_PROCESS_FIXTURE=stderr"}
	err := runWorkerProcess(ctx, cmd)
	if err == nil || err.Error() != "worker diagnostic limit exceeded" {
		t.Fatalf("diagnostic flood not stopped with redacted limit error: %v", err)
	}
	if cmd.ProcessState == nil || cmd.ProcessState.Success() {
		t.Fatalf("flooding child not killed and waited: %v", cmd.ProcessState)
	}
	if ctx.Err() != nil {
		t.Fatal("used outer test deadline instead of diagnostic bound")
	}
}

type cancelOnWrite struct{ cancel context.CancelFunc }

func (w cancelOnWrite) Write(p []byte) (int, error) {
	w.cancel()
	return len(p), nil
}

func TestWorkerProcessCancellationReaps(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	cmd := exec.Command(os.Args[0], "-test.run=^TestProcessFixture$")
	cmd.Env = []string{"BRAIN_PROCESS_FIXTURE=cancel"}
	cmd.Stdout = cancelOnWrite{cancel}
	err := runWorkerProcess(ctx, cmd)
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("cancellation ignored: err=%v state=%v", err, cmd.ProcessState)
	}
	if cmd.ProcessState == nil || cmd.ProcessState.Success() {
		t.Fatalf("child was not killed and waited: %v", cmd.ProcessState)
	}
	if err := cmd.Process.Signal(os.Kill); !errors.Is(err, os.ErrProcessDone) {
		t.Fatalf("child still signalable after return: %v", err)
	}
}
