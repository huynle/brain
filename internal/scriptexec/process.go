package scriptexec

import (
	"context"
	"errors"
	"os/exec"
)

var errWorkerDiagnosticLimit = errors.New("worker diagnostic limit exceeded")

// Diagnostics are untrusted protected content. Count and discard; do not retain
// or forward them to an ambient logger. Structured authorized logs are separate.
type diagnosticSink struct {
	remaining int
	cancel    context.CancelCauseFunc
}

func (s *diagnosticSink) Write(p []byte) (int, error) {
	if len(p) > s.remaining {
		s.cancel(errWorkerDiagnosticLimit)
		return 0, errWorkerDiagnosticLimit
	}
	s.remaining -= len(p)
	return len(p), nil
}

// runWorkerProcess is an inactive lifecycle primitive, NOT a sandbox launcher.
// The trusted caller must establish confinement (including no child creation),
// sanitized descriptors/environment and bounded, nonblocking I/O before use.
// It owns Start/Wait and joins its cancellation observer before returning. Go's
// os.Process synchronizes Kill against Wait, avoiding a recycled-PID signal.
// No production call site is permitted until launch policy is independently proven.
func runWorkerProcess(ctx context.Context, cmd *exec.Cmd) error {
	return runWorkerProcessWithStart(ctx, cmd, nil)
}

// runWorkerProcessWithStart additionally runs afterStart once the child exists
// and the cancellation observer is armed, before Wait. A non-nil error cancels
// with that cause, so the child is killed and Waited exactly like cancellation.
// afterStart may block on child I/O: cancellation kills the child, unblocking it.
func runWorkerProcessWithStart(ctx context.Context, cmd *exec.Cmd, afterStart func(pid int) error) error {
	ctx, cancel := context.WithCancelCause(ctx)
	defer cancel(nil)
	if err := ctx.Err(); err != nil {
		return context.Cause(ctx)
	}
	releaseThread := prepareWorkerParentDeath(cmd)
	defer releaseThread()
	cmd.Stderr = &diagnosticSink{remaining: 64 << 10, cancel: cancel}
	if err := cmd.Start(); err != nil {
		return err
	}
	done := make(chan struct{})
	joined := make(chan struct{})
	go func() {
		defer close(joined)
		select {
		case <-ctx.Done():
			_ = cmd.Process.Kill()
		case <-done:
		}
	}()
	if afterStart != nil {
		if err := afterStart(cmd.Process.Pid); err != nil {
			cancel(err)
		}
	}
	err := cmd.Wait()
	close(done)
	<-joined
	if ctx.Err() != nil {
		return context.Cause(ctx)
	}
	return err
}
