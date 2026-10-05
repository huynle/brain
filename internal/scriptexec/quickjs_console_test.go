package scriptexec

import (
	"bytes"
	"context"
	"encoding/json"
	"os/exec"
	"testing"
	"time"
)

func TestQuickJSBoundedConsole(t *testing.T) {
	_, err := quickJSProgram(t, "", true, func(host, name string) ([]byte, error) {
		for _, tc := range []struct {
			name, source string
			logs         int
			refused      bool
		}{
			{"structured", `console.log("secret",{value:42});console.warn("warning");42;`, 2, false},
			{"levels", `for(const k of ["debug","info","warn","error","log"])console[k](k);42;`, 5, false},
			{"count", `for(let i=0;i<33;i++)console.log(i);42;`, 32, true},
			{"nested count", `console.log({toJSON(){for(let i=0;i<32;i++)console.info(i);return 42}});42;`, 32, true},
			{"single bytes", `console.log("x".repeat(8192));42;`, 0, true},
			{"total bytes", `for(let i=0;i<3;i++)console.log("x".repeat(6000));42;`, 2, true},
			{"cycle", `const x={};x.x=x;console.log(x);42;`, 0, true},
			{"getter loop", `console.log({get x(){while(true){}}});42;`, 0, true},
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
				for i := 1; i <= tc.logs; i++ {
					if err := WriteFrame(&in, Frame{1, "result", uint64(i), json.RawMessage(`null`)}); err != nil {
						t.Fatal(err)
					}
				}
				cmd.Stdin, cmd.Stderr = &in, &stderr
				out, runErr := cmd.Output()
				if ctx.Err() != nil {
					t.Fatal("outer deadline reached")
				}
				if stderr.Len() != 0 {
					t.Fatal("console leaked to diagnostics")
				}
				r := bytes.NewReader(out)
				for i := 1; i <= tc.logs; i++ {
					f, err := ReadFrame(r)
					if err != nil || f.Kind != "call" || f.Sequence != uint64(i) {
						t.Fatalf("missing bounded console frame %d: %v", i, err)
					}
					var call struct {
						Operation string
						Arguments struct {
							Level  string
							Values []any
						}
					}
					if err := json.Unmarshal(f.Payload, &call); err != nil || call.Operation != "console.log" || call.Arguments.Level == "" || len(call.Arguments.Values) == 0 {
						t.Fatalf("invalid console payload: %v", err)
					}
					if tc.name == "structured" && i == 1 && string(f.Payload) != `{"operation":"console.log","arguments":{"level":"log","values":["secret",{"value":42}]}}` {
						t.Fatal("lost structured log values")
					}
				}
				if tc.refused {
					if runErr == nil || r.Len() != 0 {
						t.Fatal("log bound did not retire worker")
					}
					return
				}
				f, err := ReadFrame(r)
				if runErr != nil || err != nil || f.Kind != "result" || string(f.Payload) != "42" || r.Len() != 0 {
					t.Fatalf("console/result failed: %v %v", runErr, err)
				}
			})
		}
		return nil, nil
	})
	if err != nil {
		t.Fatal(err)
	}
}
