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
	"strings"
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
	runManagedFixture(t, false)
}

// The external observer owns the input pipe across the Go parent's death; EOF
// cannot rescue an unprotected orphan. The worker is a direct child of Go.
func TestNativeManagedOrphanParent(t *testing.T) {
	worker := os.Getenv("BRAIN_NATIVE_WORKER_FIXTURE")
	if worker == "" {
		t.Skip("requires opt-in native Linux worker fixture")
	}
	cmd := exec.Command(worker)
	cmd.Env = []string{}
	cmd.Stdin, cmd.Stdout = os.Stdin, os.Stdout
	if err := runWorkerProcess(context.Background(), cmd); err != nil {
		t.Fatal(err)
	}
}

func TestQuickJSManagedParentDeath(t *testing.T) {
	runManagedFixture(t, true)
}

func runManagedFixture(t *testing.T, observeDeath bool) {
	t.Helper()
	_, err := quickJSProgram(t, "", true, func(host, name string) ([]byte, error) {
		binary := filepath.Join(t.TempDir(), "managed.test")
		ctx, cancel := context.WithTimeout(context.Background(), 600*time.Second) // trusted build or cross-build under shared-host load; prefer BRAIN_SCRIPT_WORKER_ARTIFACT
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
		if observeDeath {
			observer := strings.Replace(lifecycleObserver, `char *args[]={"probe","--supervise",NULL};
        _exit(supervised_main(2,args));`, `execl("/tmp/managed.test","managed.test","-test.run=^TestNativeManagedOrphanParent$",NULL);
        _exit(73);`, 1)
			if strings.Contains(observer, "supervised_main") {
				return nil, errors.New("observer launch anchor changed")
			}
			headers := "#include <sys/prctl.h>\n#include <sys/wait.h>\n#include <unistd.h>\n#include <signal.h>\n#include <errno.h>\n#include <stdio.h>\n#include <string.h>\n"
			buildObserver := exec.CommandContext(ctx, "docker", "--host", host, "exec", "-i", name, "/bin/sh", "-c", "cat > /tmp/direct-observer.c && cc -O1 /tmp/direct-observer.c -o /tmp/direct-observer")
			buildObserver.Stdin = strings.NewReader(headers + observer)
			if out, e := buildObserver.CombinedOutput(); e != nil {
				return nil, fmt.Errorf("build direct observer: %w %s", e, out)
			}
			deathCtx, stop := context.WithTimeout(ctx, 5*time.Second)
			defer stop()
			run := exec.CommandContext(deathCtx, "docker", "--host", host, "exec", "--user=65534:65534", name, "/usr/bin/env", "-i", "BRAIN_NATIVE_WORKER_FIXTURE=/tmp/probe", "/tmp/direct-observer")
			out, e := run.CombinedOutput()
			if deathCtx.Err() != nil {
				return nil, errors.New("direct worker survived Go parent death until outer deadline")
			}
			if e != nil {
				return nil, fmt.Errorf("direct observer: %w %s", e, out)
			}
			if !strings.Contains(string(out), `"worker_reaped":true,"signal":9,"no_children":true`) {
				return nil, fmt.Errorf("incomplete orphan evidence: %s", out)
			}
			t.Logf("external native subreaper after direct Go-parent death: %s", out)
			return nil, nil
		}
		run := exec.CommandContext(ctx, "docker", "--host", host, "exec", "--user=65534:65534", name, "/usr/bin/env", "-i", "BRAIN_NATIVE_WORKER_FIXTURE=/tmp/probe", "/tmp/managed.test", "-test.run=^TestNativeManaged(QuickJS|Aggregate|Startup)$", "-test.v", "-test.timeout=20s")
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
