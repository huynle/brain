package scriptexec

import (
	"bytes"
	"encoding/json"
	"sync"
	"testing"
)

func TestProtocolTerminalFailure(t *testing.T) {
	for _, code := range []string{"compile_failed", "script_failed", "result_invalid", "limit_exceeded"} {
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

func TestProtocolCompetingTerminalOutcomes(t *testing.T) {
	s, _ := NewProtocolSession(ProtocolLimits{100, 65536, 1 << 20})
	var wg sync.WaitGroup
	start := make(chan struct{})
	accepted := make(chan WorkerMessage, 2)
	for _, f := range []Frame{{1, "result", 1, json.RawMessage(`42`)}, {1, "error", 1, json.RawMessage(`{"code":"script_failed"}`)}} {
		wg.Add(1)
		go func(f Frame) {
			defer wg.Done()
			<-start
			if m, err := s.Accept(f); err == nil {
				accepted <- m
			}
		}(f)
	}
	close(start)
	wg.Wait()
	close(accepted)
	count := 0
	for m := range accepted {
		count++
		if !m.Done || (m.Failure == nil) == (m.Result == nil) {
			t.Fatal("ambiguous outcome")
		}
	}
	if count != 1 {
		t.Fatalf("terminal winner count%d", count)
	}
}

func TestProtocolPendingCallRefusesTerminalError(t *testing.T) {
	s, _ := NewProtocolSession(ProtocolLimits{100, 65536, 1 << 20})
	if _, err := s.Accept(Frame{1, "call", 1, json.RawMessage(`{"operation":"entries.get","arguments":{"id":"one"}}`)}); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Accept(Frame{1, "error", 1, json.RawMessage(`{"code":"script_failed"}`)}); err == nil {
		t.Fatal("error bypassed pending call")
	}
	if _, err := s.Reply(json.RawMessage(`42`)); err == nil {
		t.Fatal("pending error did not retire")
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
