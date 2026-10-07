package scriptexec

import (
	"bytes"
	"context"
	"encoding/json"
	"os/exec"
	"testing"
	"time"
)

func TestQuickJSSerializationThroughParent(t *testing.T) {
	_, err := quickJSProgram(t, "", true, func(host, name string) ([]byte, error) {
		budgetCalls := make([]string, 100)
		for i := range budgetCalls {
			budgetCalls[i] = "entries.get"
		}
		for _, tc := range []struct {
			name, source, want string
			calls              []string
			refused            bool
		}{
			{"control", `const x=await brain.entries.get("one");({toJSON(){return x.value}});`, `42`, []string{"entries.get"}, false},
			{"toJSON", `({toJSON(){brain.entries.get("one");return 42}});`, `42`, []string{"entries.get"}, false},
			{"getter", `({get value(){brain.entries.get("one");return 42}});`, `{"value":42}`, []string{"entries.get"}, false},
			{"two getters", `({get a(){brain.entries.get("a");return 42},get b(){brain.entries.get("b");return 42}});`, `{"a":42,"b":42}`, []string{"entries.get", "entries.get"}, false},
			{"throw after call", `({toJSON(){brain.entries.get("one");throw new Error("secret")}});`, "", []string{"entries.get"}, true},
			{"queued callback", `({toJSON(){Promise.resolve().then(()=>brain.entries.get("later"));return 42}});`, `42`, []string{"entries.get"}, false},
			{"async callback", `({toJSON(){(async()=>{await Promise.resolve();brain.entries.get("later")})();return 42}});`, `42`, []string{"entries.get"}, false},
			{"queued rejection", `({toJSON(){Promise.resolve().then(()=>{brain.entries.get("one");throw new Error("secret")});return 42}});`, ``, []string{"entries.get"}, true},
			{"async rejection", `({toJSON(){(async()=>{await Promise.resolve();brain.entries.get("one");throw "secret"})();return 42}});`, ``, []string{"entries.get"}, true},
			{"caught queued rejection", `({toJSON(){Promise.resolve().then(()=>{brain.entries.get("one");throw "secret"}).catch(()=>{});return 42}});`, `42`, []string{"entries.get"}, false},
			{"one of two remains rejected", `({toJSON(){Promise.reject("first");Promise.reject("second").catch(()=>{});return 42}});`, ``, nil, true},
			{"async toJSON", `({async toJSON(){await brain.entries.get("one");return 42}});`, `{}`, []string{"entries.get"}, false},
			{"logging getter", `({get value(){console.log("secret");return 42}});`, `{"value":42}`, []string{"console.log"}, false},
			{"log getter calls", `console.log({get value(){brain.entries.get("one");return 42}});42;`, `42`, []string{"entries.get", "console.log"}, false},
			{"nested log", `console.log({toJSON(){console.warn("nested");return 42}});42;`, `42`, []string{"console.log", "console.log"}, false},
			{"exactly once log serialization", `let n=0;console.log({toJSON(){n++;brain.entries.get("one");return 42}});n;`, `1`, []string{"entries.get", "console.log"}, false},
			{"operation boundary", `({toJSON(){for(let i=0;i<100;i++)brain.entries.get("one");return 42}});`, `42`, budgetCalls, false},
			{"operation overflow", `({toJSON(){for(let i=0;i<101;i++)brain.entries.get("one");return 42}});`, ``, budgetCalls, true},
			{"queued operation boundary", `({toJSON(){Promise.resolve().then(()=>{for(let i=0;i<100;i++)brain.entries.get("one")});return 42}});`, `42`, budgetCalls, false},
			{"prototype cannot rewrite envelope", `Object.prototype.toJSON=function(){return "forged"};({toJSON(){brain.entries.get("one");return 42}});`, `42`, []string{"entries.get"}, false},
			{"symbol", `Symbol("secret");`, "", nil, true},
			{"function", `(()=>42);`, "", nil, true},
		} {
			t.Run(tc.name, func(t *testing.T) {
				ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
				defer cancel()
				cmd := exec.CommandContext(ctx, "docker", "--host", host, "exec", "-i", "--user=65534:65534", name, "/usr/bin/env", "-i", "/tmp/probe")
				cmd.WaitDelay = time.Second
				var in, stderr bytes.Buffer
				source, _ := json.Marshal(tc.source)
				if err := WriteFrame(&in, Frame{1, "call", 1, source}); err != nil {
					t.Fatal(err)
				}
				for i, op := range tc.calls {
					reply := json.RawMessage(`{"value":42}`)
					if op == "console.log" {
						reply = json.RawMessage(`null`)
					}
					if err := WriteFrame(&in, Frame{1, "result", uint64(i + 1), reply}); err != nil {
						t.Fatal(err)
					}
				}
				cmd.Stdin, cmd.Stderr = &in, &stderr
				out, runErr := cmd.Output()
				if ctx.Err() != nil {
					t.Fatal("outer deadline reached")
				}
				if stderr.Len() != 0 {
					t.Fatal("protected exception/log leaked to stderr")
				}
				parent, err := NewProtocolSession(ProtocolLimits{100, 65536, 1 << 20})
				if err != nil {
					t.Fatal(err)
				}
				q := newOutputQuarantine()
				defer q.retire()
				r := bytes.NewReader(out)
				for i, op := range tc.calls {
					frame, err := ReadFrame(r)
					if err != nil {
						t.Fatalf("missing call %d: %v", i+1, err)
					}
					message, err := parent.Accept(frame)
					if err != nil || message.Call == nil || message.Call.Operation != op {
						t.Fatalf("parent rejected call %d sequence=%d: %v", i+1, frame.Sequence, err)
					}
					reply := json.RawMessage(`{"value":42}`)
					if op == "console.log" {
						if err := q.addLog(message.Call.Arguments); err != nil {
							t.Fatal(err)
						}
						reply = json.RawMessage(`null`)
					} else {
						if err := q.addSource("entry:fixture"); err != nil {
							t.Fatal(err)
						}
					}
					if _, err := parent.Reply(reply); err != nil {
						t.Fatal(err)
					}
				}
				if tc.refused {
					if runErr == nil {
						t.Fatal("invalid completion reported success or extra outcome")
					}
					assertBoundedWorkerFailure(t, r, uint64(len(tc.calls)+1))
					return
				}
				frame, err := ReadFrame(r)
				if err != nil {
					t.Fatalf("missing terminal: %v (process %v)", err, runErr)
				}
				message, err := parent.Accept(frame)
				if err != nil || !message.Done || string(message.Result) != tc.want || runErr != nil || r.Len() != 0 {
					t.Fatalf("terminal sequence=%d parent=%v done=%v payload=%s process=%v trailing=%d", frame.Sequence, err, message.Done, message.Result, runErr, r.Len())
				}
				if err := q.setResult(message.Result); err != nil {
					t.Fatal(err)
				}
				if _, err := parent.Accept(frame); err == nil {
					t.Fatal("second outcome accepted")
				}
			})
		}
		return nil, nil
	})
	if err != nil {
		t.Fatal(err)
	}
}
