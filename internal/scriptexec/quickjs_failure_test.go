package scriptexec

import (
	"bytes"
	"context"
	"encoding/json"
	"os/exec"
	"testing"
	"time"
)

// Hard kills/limit exhaustion may have no terminal. If the worker emits a
// failure it must be one exact bounded error, never content or success bytes.
func assertBoundedWorkerFailure(t *testing.T, r *bytes.Reader, sequence uint64) {
	t.Helper()
	if r.Len() == 0 {
		return
	}
	f, err := ReadFrame(r)
	if err != nil || f.Kind != "error" || f.Sequence != sequence || r.Len() != 0 {
		t.Fatalf("invalid refusal framing: kind=%s sequence=%d err=%v trailing=%d", f.Kind, f.Sequence, err, r.Len())
	}
	s, _ := NewProtocolSession(ProtocolLimits{100, 65536, 1 << 20})
	// Prior calls are independently checked by the invoking fixture.
	s.next = sequence
	m, err := s.Accept(f)
	if err != nil || !m.Done || m.Failure == nil || m.Result != nil {
		t.Fatalf("invalid failure: %+v err=%v", m, err)
	}
	if _, err = s.Accept(f); err == nil {
		t.Fatal("second outcome accepted")
	}
}

func TestQuickJSFixedTerminalErrors(t *testing.T) {
	_, err := quickJSProgram(t, "", true, func(host, container string) ([]byte, error) {
		for _, tc := range []struct{ name, source, code string }{
			{"compile", `return (`, "compile_failed"},
			{"throw string", `throw "private text";`, "script_failed"},
			{"throw proxy", `throw new Proxy({}, {get(){brain.entries.get("must-not-run");return "private text";}});`, "script_failed"},
			{"throw error accessor", `const e=new Error("private text");Object.defineProperty(e,"stack",{get(){brain.entries.get("must-not-run");return "private text"}});throw e;`, "script_failed"},
			{"promise reject", `await Promise.reject({toJSON(){brain.entries.get("must-not-run");return "private text"}});`, "script_failed"},
			{"serializer reject", `({toJSON(){throw new Proxy({},{get(){brain.entries.get("must-not-run");}})}});`, "result_invalid"},
		} {
			t.Run(tc.name, func(t *testing.T) {
				ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
				defer cancel()
				cmd := exec.CommandContext(ctx, "docker", "--host", host, "exec", "-i", "--user=65534:65534", container, "/usr/bin/env", "-i", "/tmp/probe")
				cmd.WaitDelay = time.Second
				var in, stderr bytes.Buffer
				source, _ := json.Marshal(tc.source)
				if err := WriteFrame(&in, Frame{1, "call", 1, source}); err != nil {
					t.Fatal(err)
				}
				cmd.Stdin, cmd.Stderr = &in, &stderr
				out, runErr := cmd.Output()
				if ctx.Err() != nil || runErr == nil || stderr.Len() != 0 {
					t.Fatalf("unsafe exception outcome: process=%v diagnostics=%d", runErr, stderr.Len())
				}
				r := bytes.NewReader(out)
				f, err := ReadFrame(r)
				if err != nil {
					t.Fatalf("missing structured failure: %v", err)
				}
				if f.Kind != "error" || f.Sequence != 1 || string(f.Payload) != `{"code":"`+tc.code+`"}` || r.Len() != 0 {
					t.Fatalf("unexpected error output: kind=%s sequence=%d bytes=%d", f.Kind, f.Sequence, len(out))
				}
				assertBoundedWorkerFailure(t, bytes.NewReader(out), 1)
			})
		}
		return nil, nil
	})
	if err != nil {
		t.Fatal(err)
	}
}
