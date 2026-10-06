package scriptexec

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"testing"
	"time"
)

// deepArray builds a script value whose innermost empty array sits at the
// given JSON depth (top-level value depth 0), the parent's uniqueJSON measure.
func deepArray(depth int) string {
	return `(()=>{const a=[];let c=a;for(let i=0;i<` + strconv.Itoa(depth) + `;i++){const n=[];c.push(n);c=n;}return a})()`
}

// Every execution must end with EXACTLY ONE terminal outcome that the real
// parent ProtocolSession accepts, with nothing after it: success, or a fixed
// bounded failure code. A limit or depth violation must never look like a
// protocol-violating worker (a frame the parent rejects, or silent exit).
// Hard kernel kills (CPU/AS) are the only no-terminal outcomes and are covered
// by the launcher/CPU tests. Runs inside the opt-in Linux wrapper.
func TestNativeLauncherFinality(t *testing.T) {
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
	l, err := newWorkerLauncher(launcherConfig{Enabled: true, WorkerPath: pinned, WorkerSHA256: fileSHA256(t, pinned), WallTimeout: 5 * time.Second})
	if err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		name, source string
		calls        int    // brokered calls the parent must accept before the terminal
		result, code string // exactly one of these
	}{
		{"result depth 64 accepted", deepArray(64), 0, deepJSON(64), ""},
		{"result depth 65 result_invalid", deepArray(65), 0, "", "result_invalid"},
		{"result depth 70 result_invalid", deepArray(70), 0, "", "result_invalid"},
		// Console payload nests values at depth 3: a value may add 61 levels.
		{"console depth at limit", `console.log(` + deepArray(61) + `);42`, 1, "42", ""},
		{"console depth over limit", `console.log(` + deepArray(62) + `);42`, 0, "", "limit_exceeded"},
		{"console depth 70", `console.log(` + deepArray(70) + `);42`, 0, "", "limit_exceeded"},
		{"console count 33", `for(let i=0;i<33;i++)console.log(i);42`, 32, "", "limit_exceeded"},
		{"console record bytes", `console.log("x".repeat(8170));42`, 0, "", "limit_exceeded"},
		{"console total bytes", `for(let i=0;i<3;i++)console.log("x".repeat(6000));42`, 2, "", "limit_exceeded"},
		{"console cycle", `const x={};x.x=x;console.log(x);42`, 0, "", "script_failed"},
		{"100 calls accepted", `let s=0;for(let i=0;i<100;i++)s+=(await brain.entries.get("x")).value;s`, 100, "2100", ""},
		{"101st call limit_exceeded", `for(let i=0;i<101;i++)await brain.entries.get("x");1`, 100, "", "limit_exceeded"},
		{"unawaited unsupported call script_failed", `brain.tasks.resume("p","t");42`, 0, "", "script_failed"},
		{"handled unsupported call ok", `brain.tasks.resume("p","t").catch(()=>{});42`, 0, "42", ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			session, err := NewProtocolSession(ProtocolLimits{100, 65536, 1 << 20})
			if err != nil {
				t.Fatal(err)
			}
			defer session.Retire()
			calls, terminals := 0, 0
			var protocolErr error
			var result json.RawMessage
			var failure *WorkerFailure
			report, runErr := l.run(context.Background(), tc.source, func(frame Frame, reply func(Frame) error) error {
				if terminals > 0 {
					protocolErr = errors.New("frame after terminal outcome")
					return protocolErr
				}
				message, e := session.Accept(frame)
				if e != nil {
					protocolErr = e
					return e
				}
				if message.Done {
					terminals++
					result, failure = message.Result, message.Failure
					return nil
				}
				calls++
				answer := json.RawMessage(`null`)
				if message.Call.Operation == "entries.get" {
					answer = json.RawMessage(`{"value":21}`)
				}
				out, e := session.Reply(answer)
				if e != nil {
					return e
				}
				return reply(out)
			})
			if protocolErr != nil {
				t.Fatalf("parent rejected worker output (looks like a misbehaving worker): %v", protocolErr)
			}
			if !report.waited || terminals != 1 || calls != tc.calls {
				t.Fatalf("want exactly one terminal after %d calls; terminals=%d calls=%d run=%v report=%+v", tc.calls, terminals, calls, runErr, report)
			}
			switch {
			case tc.code != "":
				if failure == nil || failure.Code != tc.code || result != nil || runErr == nil {
					t.Fatalf("want failure %q, got failure=%+v result=%s run=%v", tc.code, failure, result, runErr)
				}
			default:
				if failure != nil || string(result) != tc.result || runErr != nil {
					t.Fatalf("want result %s, got failure=%+v result=%.80s run=%v", tc.result, failure, result, runErr)
				}
			}
			t.Logf("calls=%d terminal=%s run=%v", calls, func() string {
				if failure != nil {
					return failure.Code
				}
				return "result"
			}(), runErr)
		})
	}
}

// deepJSON is the exact serialization of deepArray(depth).
func deepJSON(depth int) string {
	s := "[]"
	for i := 0; i < depth; i++ {
		s = "[" + s + "]"
	}
	return s
}
