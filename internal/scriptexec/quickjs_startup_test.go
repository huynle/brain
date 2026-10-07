package scriptexec

import (
	"context"
	"errors"
	"io"
	"os"
	"os/exec"
	"runtime"
	"testing"
	"time"
)

// exec starts its stdin-copy goroutine only after starting the process. Refuse
// input and cancel at that exact boundary, before any submitted source is sent.
type cancelStartupReader struct{ cancel context.CancelCauseFunc }

func (r cancelStartupReader) Read([]byte) (int, error) {
	r.cancel(errStartupFixture)
	return 0, io.EOF
}

var errStartupFixture = errors.New("fixture startup cancellation")

func TestNativeManagedStartup(t *testing.T) {
	worker := os.Getenv("BRAIN_NATIVE_WORKER_FIXTURE")
	if worker == "" {
		t.Skip("requires opt-in native Linux worker fixture")
	}
	if runtime.GOOS != "linux" {
		t.Fatal("native Linux fixture only")
	}
	for _, before := range []bool{true, false} {
		for i := range 20 {
			deadline, stop := context.WithTimeout(context.Background(), 2*time.Second)
			ctx, cancel := context.WithCancelCause(deadline)
			cmd := exec.Command(worker)
			cmd.Env = []string{}
			cmd.Stdin = cancelStartupReader{cancel}
			if before {
				cancel(errStartupFixture)
			}
			err := runWorkerProcess(ctx, cmd)
			cancel(nil)
			stop()
			if !errors.Is(err, errStartupFixture) {
				t.Fatalf("before=%v iteration%d: lost cancellation: %v", before, i, err)
			}
			if before {
				if cmd.Process != nil || cmd.ProcessState != nil {
					t.Fatal("pre-cancelled native child started")
				}
			} else {
				if cmd.Process == nil || cmd.ProcessState == nil || cmd.ProcessState.Success() {
					t.Fatal("native startup cancellation not joined")
				}
				if err := cmd.Process.Kill(); !errors.Is(err, os.ErrProcessDone) {
					t.Fatalf("native child still alive: %v", err)
				}
			}
		}
	}
	t.Log("20 pre-start refusals; 20 post-Start/pre-source cancellations; all started children Waited")
}
