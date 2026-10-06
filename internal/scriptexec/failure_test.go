package scriptexec

import (
	"bytes"
	"encoding/json"
	"testing"
)

func TestProtocolTerminalFailure(t *testing.T) {
	for _, code := range []string{"compile_failed", "script_failed", "result_invalid"} {
		t.Run(code, func(t *testing.T) {
			s, err := NewProtocolSession(ProtocolLimits{100, 65536, 1 << 20})
			if err != nil {
				t.Fatal(err)
			}
			call := Frame{1, "call", 1, json.RawMessage(`{"operation":"entries.get","arguments":{"id":"one"}}`)}
			if _, err = s.Accept(call); err != nil {
				t.Fatal(err)
			}
			if _, err = s.Reply(json.RawMessage(`42`)); err != nil {
				t.Fatal(err)
			}
			f := Frame{1, "error", 2, json.RawMessage(`{"code":"` + code + `","location":{"line":2,"column":4}}`)}
			var wire bytes.Buffer
			if err = WriteFrame(&wire, f); err != nil {
				t.Fatalf("terminal error cannot be encoded: %v", err)
			}
			f, err = ReadFrame(&wire)
			if err != nil {
				t.Fatal(err)
			}
			m, err := s.Accept(f)
			if err != nil || !m.Done || m.Call != nil || m.Result != nil {
				t.Fatalf("failure not distinct from result: %+v err=%v", m, err)
			}
			if m.Failure == nil || m.Failure.Code != code || m.Failure.Location == nil || m.Failure.Location.Line != 2 || m.Failure.Location.Column != 4 {
				t.Fatalf("missing bounded error metadata: %+v", m.Failure)
			}
			if _, err = s.Accept(f); err == nil {
				t.Fatal("duplicate terminal error accepted")
			}
			if _, err = s.Accept(Frame{1, "result", 2, json.RawMessage(`42`)}); err == nil {
				t.Fatal("success after error accepted")
			}
		})
	}
}

func TestProtocolFailureRejectsContentAndInvalidLocations(t *testing.T) {
	for _, payload := range []string{
		`{"code":"secret"}`, `{"code":"script_failed","message":"secret"}`, `{"code":"script_failed","stack":"secret"}`,
		`{"code":"script_failed","code":"script_failed"}`, `{"code":"script_failed","location":null}`,
		`{"code":"script_failed","location":{"line":0,"column":1}}`,
		`{"code":"script_failed","location":{"line":1,"column":32769}}`,
		`{"code":"script_failed","location":{"line":1,"column":1,"file":"secret"}}`,
		`{"code":"script_failed","location":{"line":1.5,"column":1}}`,
		`{"code":"script_failed","location":{"line":1}}`,
	} {
		s, _ := NewProtocolSession(ProtocolLimits{100, 65536, 1 << 20})
		if _, err := s.Accept(Frame{1, "error", 1, json.RawMessage(payload)}); err == nil {
			t.Fatalf("accepted unbounded failure %s", payload)
		}
		if _, err := s.Accept(Frame{1, "result", 1, json.RawMessage(`42`)}); err == nil {
			t.Fatal("invalid error did not retire")
		}
	}
}
