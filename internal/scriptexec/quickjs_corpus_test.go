package scriptexec

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os/exec"
	"strings"
	"testing"
	"time"
)

func TestQuickJSWorkerAdversarialCompilationAndFreshness(t *testing.T) {
	_, err := quickJSProgram(t, "", true, func(host, name string) ([]byte, error) {
		cases := []struct {
			name, source string
			want         string
			refused      bool
		}{
			{"deep parser nesting", "return " + strings.Repeat("(", 12000) + "0" + strings.Repeat(")", 12000) + ";", "", true},
			{"runtime compiler nesting", `return new Function("return "+"(".repeat(12000)+"0"+")".repeat(12000))();`, "", true},
			{"recursive accessor serialization", `const x={get value(){return x.value;}};return x;`, "", true},
			{"toJSON CPU loop", `return {toJSON(){while(true){}}};`, "", true},
			{"endless promise jobs", `function spin(){Promise.resolve().then(spin)}spin();return 42;`, "", true},
			{"near source bound", "/*" + strings.Repeat("x", 32000) + "*/ return 42;", "42", false},
			{"fresh state first", `const prior=globalThis.marker;globalThis.marker=42;return {fresh:prior===undefined,ambient:[typeof process,typeof require,typeof fetch,typeof std,typeof os],dynamic:Function("return typeof process")()};`, `{"fresh":true,"ambient":["undefined","undefined","undefined","undefined","undefined"],"dynamic":"undefined"}`, false},
			{"fresh state second", `return typeof globalThis.marker;`, `"undefined"`, false},
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
					t.Fatalf("outer deadline instead of worker bound: %v", ctx.Err())
				}
				if tc.refused {
					exit, ok := e.(*exec.ExitError)
					if !ok || (exit.ExitCode() != 135 && exit.ExitCode() != 136 && exit.ExitCode() != 137) || len(out) != 0 {
						t.Fatalf("unbounded/crashing/non-refusing compiler or serializer: err=%v output bytes=%d", e, len(out))
					}
					t.Logf("bounded refusal with no output; exit=%d", exit.ExitCode())
					return
				}
				if e != nil {
					t.Fatalf("valid program failed: %v %s", e, out)
				}
				reader := bytes.NewReader(out)
				result, e := ReadFrame(reader)
				if e != nil || result.Kind != "result" || string(result.Payload) != tc.want || reader.Len() != 0 {
					t.Fatalf("result=%+v err=%v trailing=%d want=%s", result, e, reader.Len(), tc.want)
				}
			})
		}
		return nil, nil
	})
	if err != nil {
		t.Fatal(fmt.Errorf("compiler corpus: %w", err))
	}
}
