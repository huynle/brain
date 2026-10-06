package scriptexec

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"runtime"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"
)

// Test-only composition, no runtime/profile selection or production quota claim.
func TestNativeManagedAggregate(t *testing.T) {
	worker := os.Getenv("BRAIN_NATIVE_WORKER_FIXTURE")
	if worker == "" {
		t.Skip("requires opt-in native Linux worker fixture")
	}
	if runtime.GOOS != "linux" {
		t.Fatal("native Linux fixture only")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 8*time.Second)
	defer cancel()
	pool, err := newLocalWorkerPool(workerAdmissionLimits{3, 2, 1, 4})
	if err != nil {
		t.Fatal(err)
	}
	defer func() { cancel(); _ = pool.close(context.Background()) }()
	type readyChild struct {
		pid int
	}
	type outcome struct {
		mode  int
		err   error
		state *os.ProcessState
		value string
		code  string
	}
	ready := make(chan readyChild, 2)
	release := make(chan struct{})
	done := make(chan outcome, 2)
	for i, pressure := range []string{`while(true){}`, `const a=[];while(true)a.push(new Uint8Array(1024*1024));`} {
		go func(i int, pressure string) {
			var state *os.ProcessState
			var value string
			var code string
			err := pool.run(ctx, workerBinding{"pressure", strconv.Itoa(i)}, func(work context.Context) error {
				cmd := exec.Command(worker)
				cmd.Env = []string{}
				input, e := cmd.StdinPipe()
				if e != nil {
					return e
				}
				defer input.Close()
				source, _ := json.Marshal(`const held=new Uint8Array(2*1024*1024);held.fill(1);await brain.entries.get("ready");` + pressure)
				if e = WriteFrame(input, Frame{1, "call", 1, source}); e != nil {
					return e
				}
				session, _ := NewProtocolSession(ProtocolLimits{1, 1024, 4096})
				defer session.Retire()
				childCtx, stop := context.WithCancelCause(work)
				defer stop(nil)
				sink := &frameSink{remaining: 8192, cancel: stop, accept: func(f Frame) error {
					m, e := session.Accept(f)
					if e != nil {
						return e
					}
					if m.Call == nil {
						value = string(m.Result)
						if m.Failure != nil {
							code = m.Failure.Code
						}
						return nil
					}
					if m.Call.Operation != "entries.get" || string(m.Call.Arguments) != `{"id":"ready"}` {
						return ErrProtocol
					}
					ready <- readyChild{cmd.Process.Pid}
					select {
					case <-release:
					case <-work.Done():
						return context.Cause(work)
					}
					reply, e := session.Reply(json.RawMessage(`{}`))
					if e != nil {
						return e
					}
					return WriteFrame(input, reply)
				}}
				cmd.Stdout = sink
				e = runWorkerProcess(childCtx, cmd)
				state = cmd.ProcessState
				return e
			})
			done <- outcome{i, err, state, value, code}
		}(i, pressure)
	}
	var totalVM, totalRSS uint64
	for range 2 {
		select {
		case child := <-ready:
			vm, rss := nativeMemoryKB(t, child.pid)
			if vm > 64*1024 || rss > vm || rss < 2*1024 {
				t.Fatalf("unexpected native memory vm=%d rss=%d KiB", vm, rss)
			}
			totalVM += vm
			totalRSS += rss
		case <-ctx.Done():
			t.Fatal("two sealed children never became simultaneously ready")
		}
	}
	pool.mu.Lock()
	active := pool.active
	pool.mu.Unlock()
	if active != 2 || totalVM > 128*1024 {
		t.Fatalf("simultaneous occupancy=%d vm=%d KiB", active, totalVM)
	}
	// Saturated pressure tenant must not stall an independent tenant's real child.
	err = pool.run(ctx, workerBinding{"independent", "reader"}, func(work context.Context) error {
		cmd := exec.Command(worker)
		cmd.Env = []string{}
		var in strings.Builder
		if e := WriteFrame(&in, Frame{1, "call", 1, json.RawMessage(`"42"`)}); e != nil {
			return e
		}
		cmd.Stdin = strings.NewReader(in.String())
		session, _ := NewProtocolSession(ProtocolLimits{1, 1024, 4096})
		var result string
		sink := &frameSink{remaining: 8192, cancel: func(error) {}, accept: func(f Frame) error {
			m, e := session.Accept(f)
			result = string(m.Result)
			return e
		}}
		cmd.Stdout = sink
		if e := runWorkerProcess(work, cmd); e != nil {
			return e
		}
		if result != "42" || cmd.ProcessState == nil || !cmd.ProcessState.Success() {
			return errors.New("independent native child failed")
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	close(release)
	for range 2 {
		out := <-done
		if out.err == nil || out.state == nil || out.state.Success() || out.value != "" {
			t.Fatalf("pressure must fail and Wait without successful result: %+v", out)
		}
		usage := out.state.SysUsage().(*syscall.Rusage)
		if usage.Maxrss > 64*1024 {
			t.Fatalf("native maxrss exceeds per-worker AS ceiling: %d", usage.Maxrss)
		}
		if out.mode == 0 {
			status := out.state.Sys().(syscall.WaitStatus)
			if !status.Signaled() || status.Signal() != syscall.SIGKILL || out.state.UserTime() < 500*time.Millisecond {
				t.Fatalf("CPU limit was not observed: %v cpu=%s", out.state, out.state.UserTime())
			}
		} else if out.code != "script_failed" {
			t.Fatalf("heap pressure must report bounded script failure: %v code=%s", out.state, out.code)
		}
		t.Logf("pressure mode%d: %v cpu=%s maxrss=%d KiB code=%s", out.mode, out.state, out.state.UserTime(), usage.Maxrss, out.code)
	}
	if ctx.Err() != nil {
		t.Fatal("outer deadline used instead of worker resource limits")
	}
	if err := pool.close(ctx); err != nil {
		t.Fatal(err)
	}
	t.Logf("two simultaneously sealed children vm=%d rss=%d KiB; independent child42; pressure children Waited; pool joined", totalVM, totalRSS)
}

func nativeMemoryKB(t *testing.T, pid int) (vm, rss uint64) {
	t.Helper()
	data, err := os.ReadFile(fmt.Sprintf("/proc/%d/status", pid))
	if err != nil {
		t.Fatal(err)
	}
	for _, line := range strings.Split(string(data), "\n") {
		fields := strings.Fields(line)
		if len(fields) == 3 && (fields[0] == "VmSize:" || fields[0] == "VmRSS:") {
			value, e := strconv.ParseUint(fields[1], 10, 64)
			if e != nil || fields[2] != "kB" {
				t.Fatal("invalid proc memory measure")
			}
			if fields[0] == "VmSize:" {
				vm = value
			} else {
				rss = value
			}
		}
	}
	return vm, rss
}
