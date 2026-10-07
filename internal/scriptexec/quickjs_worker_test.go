package scriptexec

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os/exec"
	"strings"
	"testing"
	"time"
)

// Actual JS child + actual framed pipes. Parent answers fixture data only:
// this is neither an authorized broker nor a service/publication integration.
func TestQuickJSWorkerFramedAsyncCalls(t *testing.T) {
	_, err := quickJSProgram(t, "", true, func(host, name string) ([]byte, error) {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		cmd := exec.CommandContext(ctx, "docker", "--host", host, "exec", "-i", "--user=65534:65534", name, "/usr/bin/env", "-i", "/tmp/probe")
		cmd.WaitDelay = time.Second
		input, e := cmd.StdinPipe()
		if e != nil {
			return nil, e
		}
		output, e := cmd.StdoutPipe()
		if e != nil {
			return nil, e
		}
		var stderr bytes.Buffer
		cmd.Stderr = &stderr
		if e = cmd.Start(); e != nil {
			return nil, e
		}
		waited := false
		defer func() {
			_ = input.Close()
			if !waited {
				_ = cmd.Process.Kill()
				_ = cmd.Wait()
			}
		}()
		source, _ := json.Marshal(`const a = await brain.entries.get("first"); const b = await brain.entries.get(a.next); return {value:a.value+b.value, globals:[typeof fetch,typeof process,typeof require,typeof WebSocket]};`)
		if e = WriteFrame(input, Frame{1, "call", 1, source}); e != nil {
			return nil, e
		}
		protocol, e := NewProtocolSession(ProtocolLimits{2, 65536, 200000})
		if e != nil {
			return nil, e
		}
		defer protocol.Retire()
		for i, id := range []string{"first", "second"} {
			f, e := ReadFrame(output)
			if e != nil {
				return nil, fmt.Errorf("call %d absent: %w", i, e)
			}
			var call struct {
				Operation string `json:"operation"`
				Arguments struct {
					ID string `json:"id"`
				} `json:"arguments"`
			}
			if e = json.Unmarshal(f.Payload, &call); e != nil {
				return nil, e
			}
			admitted, e := protocol.Accept(f)
			if e != nil || admitted.Call == nil || admitted.Call.Operation != "entries.get" {
				return nil, fmt.Errorf("parent protocol refused fixture call: %v", e)
			}
			if f.Kind != "call" || f.Sequence != uint64(i+1) || call.Operation != "entries.get" || call.Arguments.ID != id {
				return nil, fmt.Errorf("unexpected worker call: %+v", f)
			}
			response := json.RawMessage(`{"next":"second","value":20}`)
			if i == 1 {
				response = json.RawMessage(`{"value":22}`)
			}
			reply, e := protocol.Reply(response)
			if e != nil {
				return nil, e
			}
			if e = WriteFrame(input, reply); e != nil {
				return nil, e
			}
		}
		f, e := ReadFrame(output)
		if e != nil {
			return nil, fmt.Errorf("final result: %w", e)
		}
		if f.Kind != "result" || f.Sequence != 3 || string(f.Payload) != `{"value":42,"globals":["undefined","undefined","undefined","undefined"]}` {
			return nil, fmt.Errorf("wrong final frame: %+v", f)
		}
		final, e := protocol.Accept(f)
		if e != nil || !final.Done {
			return nil, fmt.Errorf("parent final state: %v", e)
		}
		_ = input.Close()
		if _, e = ReadFrame(output); e != io.EOF {
			return nil, fmt.Errorf("worker emitted trailing output: %v", e)
		}
		e = cmd.Wait()
		waited = true
		if e != nil {
			return nil, fmt.Errorf("worker exit: %w stderr=%s", e, stderr.String())
		}
		t.Log("actual sealed child: two framed calls, async result 42, no ambient JS APIs, EOF and Wait observed")
		return nil, nil
	})
	if err != nil {
		t.Fatalf("framed worker behavior absent: %v", err)
	}
}

func TestQuickJSWorkerBoundedFailures(t *testing.T) {
	_, err := quickJSProgram(t, "", true, func(host, name string) ([]byte, error) {
		cases := []struct {
			name, source string
			exit         int
		}{
			{"CPU infinite loop", "for (;;) {}", 137},
			{"oversized source", strings.Repeat(" ", 32769), 133},
			{"oversized result", `return "x".repeat(100000);`, 137},
			{"cyclic result", `const x={};x.self=x;return x;`, 137},
			{"syntax error", `return (`, 135},
			{"heap exhaustion", `const x=[];while(true)x.push(new Array(10000).fill(42));`, 136},
			{"dynamic import", `return await import("node:fs");`, 136},
		}
		for _, tc := range cases {
			t.Run(tc.name, func(t *testing.T) {
				ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
				defer cancel()
				cmd := exec.CommandContext(ctx, "docker", "--host", host, "exec", "-i", "--user=65534:65534", name, "/usr/bin/env", "-i", "/tmp/probe")
				cmd.WaitDelay = time.Second
				var input bytes.Buffer
				source, _ := json.Marshal(tc.source)
				if e := WriteFrame(&input, Frame{1, "call", 1, source}); e != nil {
					t.Fatal(e)
				}
				cmd.Stdin = &input
				out, e := cmd.CombinedOutput()
				if ctx.Err() != nil {
					t.Fatalf("worker required outer wall timeout instead of own bound: %v", ctx.Err())
				}
				exit, ok := e.(*exec.ExitError)
				if !ok || exit.ExitCode() != tc.exit {
					t.Fatalf("exit=%v want=%d output bytes=%d", e, tc.exit, len(out))
				}
				assertBoundedWorkerFailure(t, bytes.NewReader(out), 1)
				t.Logf("actual worker waited/reaped, exit=%d, no result bytes", exit.ExitCode())
			})
		}
		return nil, nil
	})
	if err != nil {
		t.Fatal(err)
	}
}

func TestQuickJSWorkerWallDeadlineKillsAndReapsBlockedIPC(t *testing.T) {
	_, err := quickJSProgram(t, "", true, func(host, name string) ([]byte, error) {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		cmd := exec.CommandContext(ctx, "docker", "--host", host, "exec", "-i", "--user=65534:65534", name, "/usr/bin/env", "-i", "/tmp/probe", "--supervise")
		cmd.WaitDelay = time.Second
		input, e := cmd.StdinPipe()
		if e != nil {
			return nil, e
		}
		output, e := cmd.StdoutPipe()
		if e != nil {
			return nil, e
		}
		var stderr bytes.Buffer
		cmd.Stderr = &stderr
		if e = cmd.Start(); e != nil {
			return nil, e
		}
		waited := false
		defer func() {
			_ = input.Close()
			if !waited {
				_ = cmd.Process.Kill()
				_ = cmd.Wait()
			}
		}()
		source, _ := json.Marshal(`const x=await brain.entries.get("blocked");return x;`)
		if e = WriteFrame(input, Frame{1, "call", 1, source}); e != nil {
			return nil, e
		}
		first, e := ReadFrame(output)
		if e != nil || first.Kind != "call" {
			return nil, fmt.Errorf("child did not reach IPC wait: %v", e)
		}
		// Keep parent stdin open but intentionally do not reply. A CPU timer alone
		// cannot terminate this child; the trusted supervisor must enforce wall time.
		if _, e = ReadFrame(output); e != io.EOF {
			return nil, fmt.Errorf("expected closed worker pipe: %v", e)
		}
		e = cmd.Wait()
		waited = true
		if ctx.Err() != nil {
			return nil, fmt.Errorf("worker required outer timeout: %w", ctx.Err())
		}
		exit, ok := e.(*exec.ExitError)
		if !ok || exit.ExitCode() != 124 {
			return nil, fmt.Errorf("supervisor status=%v diagnostics=%s", e, stderr.String())
		}
		var status struct {
			TimedOut bool `json:"timed_out"`
			Reaped   bool `json:"reaped"`
			Signal   int  `json:"signal"`
		}
		if e = json.Unmarshal(stderr.Bytes(), &status); e != nil {
			return nil, e
		}
		if !status.TimedOut || !status.Reaped || status.Signal != 9 {
			return nil, fmt.Errorf("no killed-and-reaped evidence: %s", stderr.String())
		}
		t.Logf("actual supervisor waitpid: %s", stderr.String())
		return nil, nil
	})
	if err != nil {
		t.Fatal(err)
	}
}
