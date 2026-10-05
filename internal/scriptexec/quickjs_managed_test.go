package scriptexec

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"testing"
	"time"
)

// Runs only inside the opt-in Linux fixture. This directly parents the sealed
// native child: no Docker CLI or shell is mistaken for the worker PID.
func TestNativeManagedQuickJS(t *testing.T) {
	worker := os.Getenv("BRAIN_NATIVE_WORKER_FIXTURE")
	if worker == "" {
		t.Skip("requires opt-in native Linux worker fixture")
	}
	if runtime.GOOS != "linux" {
		t.Fatal("native Linux fixture only")
	}
	for _, mode := range []string{"exchange", "cancel"} {
		t.Run(mode, func(t *testing.T) {
			deadline, stop := context.WithTimeout(context.Background(), 3*time.Second)
			defer stop()
			ctx, cancel := context.WithCancelCause(deadline)
			defer cancel(nil)
			reason := errors.New("fixture execution retired")
			cmd := exec.Command(worker)
			cmd.Env = []string{}
			input, err := cmd.StdinPipe()
			if err != nil {
				t.Fatal(err)
			}
			defer input.Close()
			source, _ := json.Marshal(`const a=await brain.entries.get("first");const b=await brain.entries.get("second");return a.value+b.value;`)
			// Small bounded source fits the pipe before Start; no feeder goroutine.
			if err := WriteFrame(input, Frame{1, "call", 1, source}); err != nil {
				t.Fatal(err)
			}
			session, err := NewProtocolSession(ProtocolLimits{2, 1024, 4096})
			if err != nil {
				t.Fatal(err)
			}
			defer session.Retire()
			calls := 0
			var quarantined json.RawMessage
			sink := &frameSink{remaining: 8192, cancel: cancel, accept: func(frame Frame) error {
				message, e := session.Accept(frame)
				if e != nil {
					return e
				}
				if message.Done {
					quarantined = message.Result
					return nil
				}
				if message.Call == nil || message.Call.Operation != "entries.get" {
					return ErrProtocol
				}
				calls++
				if mode == "cancel" {
					cancel(reason)
					return nil
				}
				reply, e := session.Reply(json.RawMessage(`{"value":21}`))
				if e != nil {
					return e
				}
				return WriteFrame(input, reply)
			}}
			cmd.Stdout = sink
			err = runWorkerProcess(ctx, cmd)
			if cmd.ProcessState == nil {
				t.Fatal("native child not waited")
			}
			if deadline.Err() != nil {
				t.Fatal("outer deadline used")
			}
			if mode == "cancel" {
				if !errors.Is(err, reason) || cmd.ProcessState.Success() || calls != 1 || quarantined != nil {
					t.Fatalf("cancel err=%v state=%v calls=%d result=%s", err, cmd.ProcessState, calls, quarantined)
				}
			} else {
				if err != nil || sink.finish() != nil || !cmd.ProcessState.Success() || calls != 2 || string(quarantined) != "42" {
					t.Fatalf("exchange err=%v state=%v calls=%d result=%s", err, cmd.ProcessState, calls, quarantined)
				}
			}
			if err := cmd.Process.Kill(); !errors.Is(err, os.ErrProcessDone) {
				t.Fatalf("child still live after Wait: %v", err)
			}
			t.Logf("direct native child %s: calls=%d waited=true", mode, calls)
		})
	}
}

func TestQuickJSManagedParentIntegration(t *testing.T) {
	_, err := quickJSProgram(t, "", true, func(host, name string) ([]byte, error) {
		binary := filepath.Join(t.TempDir(), "managed.test")
		ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
		defer cancel()
		build := exec.CommandContext(ctx, "go", "test", "-c", "-o", binary, ".")
		build.Env = append(os.Environ(), "GOOS=linux", "GOARCH=arm64", "CGO_ENABLED=0")
		if out, e := build.CombinedOutput(); e != nil {
			return nil, fmt.Errorf("cross-build native parent: %w %s", e, out)
		}
		data, e := os.ReadFile(binary)
		if e != nil {
			return nil, e
		}
		copyCmd := exec.CommandContext(ctx, "docker", "--host", host, "exec", "-i", name, "/bin/sh", "-c", "cat > /tmp/managed.test && chmod 755 /tmp/managed.test")
		copyCmd.Stdin = bytes.NewReader(data)
		if out, e := copyCmd.CombinedOutput(); e != nil {
			return nil, fmt.Errorf("copy native parent: %w %s", e, out)
		}
		run := exec.CommandContext(ctx, "docker", "--host", host, "exec", "--user=65534:65534", name, "/usr/bin/env", "-i", "BRAIN_NATIVE_WORKER_FIXTURE=/tmp/probe", "/tmp/managed.test", "-test.run=^TestNativeManagedQuickJS$", "-test.v", "-test.timeout=10s")
		out, e := run.CombinedOutput()
		if e != nil {
			return nil, fmt.Errorf("native parent: %w %s", e, out)
		}
		t.Logf("real Linux parent/child fixture (non-race cross-build):\n%s", out)
		return nil, nil
	})
	if err != nil {
		t.Fatal(err)
	}
}
