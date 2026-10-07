package scriptexec

import (
	"bytes"
	"context"
	"encoding/json"
	"os/exec"
	"testing"
	"time"
)

func TestQuickJSWorkerCompletionSemantics(t *testing.T) {
	_, err := quickJSProgram(t, "", true, func(host, name string) ([]byte, error) {
		for _, tc := range []struct {
			name, source, want string
			calls              int
			refused            bool
		}{
			{"final await", `const a=40; a+await Promise.resolve(2);`, "42", 0, false},
			{"final object", "const x=\"semi;colon\"; ({x, template:`line\n;${6*7}`}); // tail", `{"x":"semi;colon","template":"line\n;42"}`, 0, false},
			{"nested return", `function f(){return 42;} f();`, "42", 0, false},
			{"block completion", `if(true){42;}else{0;}`, "42", 0, false},
			{"explicit return", `const a=await Promise.resolve(40);return a+2;`, "42", 0, false},
			{"explicit object", `return {value:42};`, `{"value":42}`, 0, false},
			{"final promise", `Promise.resolve(42);`, "42", 0, false},
			{"explicit promise", `return Promise.resolve(42);`, "42", 0, false},
			{"final thenable", `({then(resolve){resolve(42);}});`, "42", 0, false},
			{"rejected final promise", `Promise.reject(new Error("hidden"));`, "", 0, true},
			{"throwing then getter", `({get then(){throw new Error("hidden");}});`, "", 0, true},
			{"unresolved final promise", `new Promise(()=>{});`, "", 0, true},
			{"source return string", `const s="return false;";s.length;`, "13", 0, false},
			{"one operation", `(await brain.entries.get("one")).value;`, "42", 1, false},
			{"runtime failure never reruns", `await brain.entries.get("once");throw new Error("stop");`, "", 1, true},
			{"return ASI is undefined", "return\n42;", "", 0, true},
			{"invalid syntax never runs", `await brain.entries.get("never"); const = ;`, "", 0, true},
		} {
			t.Run(tc.name, func(t *testing.T) {
				ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
				defer cancel()
				cmd := exec.CommandContext(ctx, "docker", "--host", host, "exec", "-i", "--user=65534:65534", name, "/usr/bin/env", "-i", "/tmp/probe")
				cmd.WaitDelay = time.Second
				var input bytes.Buffer
				source, _ := json.Marshal(tc.source)
				if err := WriteFrame(&input, Frame{1, "call", 1, source}); err != nil {
					t.Fatal(err)
				}
				if tc.calls > 0 {
					if err := WriteFrame(&input, Frame{1, "result", 1, json.RawMessage(`{"value":42}`)}); err != nil {
						t.Fatal(err)
					}
				}
				cmd.Stdin = &input
				out, runErr := cmd.Output()
				if ctx.Err() != nil {
					t.Fatal("outer deadline reached")
				}
				reader := bytes.NewReader(out)
				for i := 0; i < tc.calls; i++ {
					frame, err := ReadFrame(reader)
					if err != nil || frame.Kind != "call" || frame.Sequence != uint64(i+1) {
						t.Fatalf("expected one fixture call: %+v %v", frame, err)
					}
				}
				if tc.refused {
					if runErr == nil {
						t.Fatalf("refused source retried or released output: %v, remaining=%q", runErr, out[len(out)-reader.Len():])
					}
					assertBoundedWorkerFailure(t, reader, uint64(tc.calls+1))
					return
				}
				result, err := ReadFrame(reader)
				if runErr != nil || err != nil || result.Kind != "result" || string(result.Payload) != tc.want || reader.Len() != 0 {
					t.Fatalf("completion=%s want=%s run=%v frame=%v trailing=%d", result.Payload, tc.want, runErr, err, reader.Len())
				}
			})
		}
		return nil, nil
	})
	if err != nil {
		t.Fatal(err)
	}
}
