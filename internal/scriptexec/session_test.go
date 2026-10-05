package scriptexec

import (
	"encoding/json"
	"errors"
	"strings"
	"sync"
	"testing"
)

func sessionFixture(t *testing.T, limits ProtocolLimits) *ProtocolSession {
	t.Helper()
	s, e := NewProtocolSession(limits)
	if e != nil {
		t.Fatal(e)
	}
	return s
}
func callFrame(n uint64) Frame {
	return Frame{1, "call", n, json.RawMessage(`{"operation":"entries.get","arguments":{"id":"one"}}`)}
}
func TestProtocolSessionOrderedExchange(t *testing.T) {
	s := sessionFixture(t, ProtocolLimits{2, 256, 1024})
	f := callFrame(1)
	m, e := s.Accept(f)
	if e != nil || m.Call == nil || m.Call.Operation != "entries.get" || string(m.Call.Arguments) != `{"id":"one"}` || m.Done {
		t.Fatalf("valid call refused: %+v %v", m, e)
	}
	for i := range f.Payload {
		f.Payload[i] = 'x'
	}
	if string(m.Call.Arguments) != `{"id":"one"}` {
		t.Fatal("worker buffer alias retained")
	}
	data := json.RawMessage(`{"value":42}`)
	reply, e := s.Reply(data)
	if e != nil || reply.Kind != "result" || reply.Sequence != 1 {
		t.Fatalf("reply: %+v %v", reply, e)
	}
	data[0] = 'x'
	if string(reply.Payload) != `{"value":42}` {
		t.Fatal("parent buffer alias retained")
	}
	m, e = s.Accept(Frame{1, "result", 2, json.RawMessage(`{"answer":42}`)})
	if e != nil || !m.Done || m.Call != nil || string(m.Result) != `{"answer":42}` {
		t.Fatalf("terminal result: %+v %v", m, e)
	}
	if _, e = s.Accept(callFrame(3)); !errors.Is(e, ErrProtocol) {
		t.Fatal("accepted after result")
	}
}

func TestProtocolSessionRejectsAndRetires(t *testing.T) {
	for _, tc := range []struct {
		name   string
		mutate func(*Frame)
	}{
		{"wrong direction", func(f *Frame) { f.Kind = "start" }},
		{"wrong version", func(f *Frame) { f.Version = 2 }},
		{"wrong sequence", func(f *Frame) { f.Sequence = 2 }},
		{"missing args", func(f *Frame) { f.Payload = json.RawMessage(`{"operation":"entries.get"}`) }},
		{"forged authority", func(f *Frame) {
			f.Payload = json.RawMessage(`{"operation":"entries.get","arguments":{},"principal":"admin"}`)
		}},
		{"duplicate operation", func(f *Frame) {
			f.Payload = json.RawMessage(`{"operation":"entries.get","operation":"entries.delete","arguments":{}}`)
		}},
		{"duplicate nested args", func(f *Frame) {
			f.Payload = json.RawMessage(`{"operation":"entries.get","arguments":{"id":"one","id":"two"}}`)
		}},
		{"nonobject args", func(f *Frame) { f.Payload = json.RawMessage(`{"operation":"entries.get","arguments":null}`) }},
		{"empty operation", func(f *Frame) { f.Payload = json.RawMessage(`{"operation":"","arguments":{}}`) }},
		{"invalid operation", func(f *Frame) { f.Payload = json.RawMessage(`{"operation":"https://example.test","arguments":{}}`) }},
		{"oversized payload", func(f *Frame) { f.Payload = json.RawMessage(`"` + strings.Repeat("x", 257) + `"`) }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			s := sessionFixture(t, ProtocolLimits{2, 256, 1024})
			f := callFrame(1)
			tc.mutate(&f)
			if _, e := s.Accept(f); !errors.Is(e, ErrProtocol) {
				t.Fatalf("accepted invalid frame: %v", e)
			}
			if _, e := s.Accept(callFrame(1)); !errors.Is(e, ErrProtocol) {
				t.Fatal("invalid frame did not retire session")
			}
		})
	}
}

func TestProtocolSessionPendingAndBudgets(t *testing.T) {
	for _, mode := range []string{"pending call", "pending final", "operation limit", "reply without call", "total budget", "oversized reply", "retired", "deep JSON"} {
		t.Run(mode, func(t *testing.T) {
			limits := ProtocolLimits{1, 256, 256}
			s := sessionFixture(t, limits)
			if mode == "reply without call" {
				if _, e := s.Reply(json.RawMessage(`{}`)); !errors.Is(e, ErrProtocol) {
					t.Fatal("unsolicited reply")
				}
				return
			}
			if mode == "retired" {
				s.Retire()
				if _, e := s.Accept(callFrame(1)); !errors.Is(e, ErrProtocol) {
					t.Fatal("retired accepted")
				}
				return
			}
			if mode == "deep JSON" {
				p := strings.Repeat("[", 65) + "0" + strings.Repeat("]", 65)
				if _, e := s.Accept(Frame{1, "result", 1, json.RawMessage(p)}); !errors.Is(e, ErrProtocol) {
					t.Fatal("deep payload accepted")
				}
				return
			}
			if _, e := s.Accept(callFrame(1)); e != nil {
				t.Fatalf("first call: %v", e)
			}
			switch mode {
			case "pending call", "pending final":
				f := callFrame(2)
				if mode == "pending final" {
					f.Kind = "result"
				}
				if _, e := s.Accept(f); !errors.Is(e, ErrProtocol) {
					t.Fatal("pending state accepted frame")
				}
			case "operation limit":
				if _, e := s.Reply(json.RawMessage(`{}`)); e != nil {
					t.Fatal(e)
				}
				if _, e := s.Accept(callFrame(2)); !errors.Is(e, ErrProtocol) {
					t.Fatal("excess operation accepted")
				}
			case "total budget":
				if _, e := s.Reply(json.RawMessage(`"` + strings.Repeat("x", 220) + `"`)); !errors.Is(e, ErrProtocol) {
					t.Fatal("total bytes exceeded")
				}
			case "oversized reply":
				if _, e := s.Reply(json.RawMessage(`"` + strings.Repeat("x", 257) + `"`)); !errors.Is(e, ErrProtocol) {
					t.Fatal("reply too large")
				}
			}
			if _, e := s.Reply(json.RawMessage(`{}`)); !errors.Is(e, ErrProtocol) {
				t.Fatal("failure did not permanently retire")
			}
		})
	}
}

func TestProtocolSessionInvalidLimits(t *testing.T) {
	for _, limits := range []ProtocolLimits{{}, {-1, 1, 1}, {1, 0, 1}, {1, 2, 1}, {10001, 1, 1}, {1, MaxFrameBytes + 1, MaxFrameBytes + 1}, {1, 1, 16*MaxFrameBytes + 1}} {
		if _, e := NewProtocolSession(limits); !errors.Is(e, ErrProtocol) {
			t.Errorf("accepted limits %+v", limits)
		}
	}
	var zero ProtocolSession
	if _, e := zero.Accept(callFrame(1)); !errors.Is(e, ErrProtocol) {
		t.Fatal("zero session admitted")
	}
}

func TestProtocolSessionConcurrentAdmissionIsAtMostOnce(t *testing.T) {
	s := sessionFixture(t, ProtocolLimits{10, 256, 1024})
	var wg sync.WaitGroup
	results := make(chan error, 2)
	for i := 0; i < 2; i++ {
		wg.Add(1)
		go func() { defer wg.Done(); _, e := s.Accept(callFrame(1)); results <- e }()
	}
	wg.Wait()
	close(results)
	admitted := 0
	for e := range results {
		if e == nil {
			admitted++
		}
	}
	if admitted != 1 {
		t.Fatalf("admitted=%d", admitted)
	}
	if _, e := s.Reply(json.RawMessage(`{}`)); !errors.Is(e, ErrProtocol) {
		t.Fatal("concurrent protocol violation did not retire")
	}
}

func TestProtocolSessionStrictPayloads(t *testing.T) {
	for _, payload := range []string{`{"operation":"entries.get","arguments":{"id":"a","\u0069d":"b"}}`, `{"operation":"entries.get","arguments":{}} {}`, `{"operation":"entries.get","arguments":{"x":[{"a":1,"a":2}]}}`, "{\"operation\":\"entries.get\",\"arguments\":{\"id\":\"\xff\"}}"} {
		s := sessionFixture(t, ProtocolLimits{1, 256, 1024})
		if _, e := s.Accept(Frame{1, "call", 1, json.RawMessage(payload)}); !errors.Is(e, ErrProtocol) {
			t.Fatalf("invalid payload accepted: %q", payload)
		}
	}
}

func FuzzProtocolSession(f *testing.F) {
	f.Add([]byte(`{"operation":"entries.get","arguments":{"id":"one"}}`), []byte(`{"value":42}`))
	f.Add([]byte(`{"operation":"entries.get","arguments":{"id":"a","id":"b"}}`), []byte(`null`))
	f.Fuzz(func(t *testing.T, payload, reply []byte) {
		if len(payload) > 4096 || len(reply) > 4096 {
			t.Skip()
		}
		s, e := NewProtocolSession(ProtocolLimits{1, 4096, 8192})
		if e != nil {
			t.Fatal(e)
		}
		m, e := s.Accept(Frame{1, "call", 1, payload})
		if e != nil {
			if _, again := s.Accept(callFrame(1)); !errors.Is(again, ErrProtocol) {
				t.Fatal("resurrected")
			}
			return
		}
		if m.Call == nil || m.Done {
			t.Fatal("wrong message kind")
		}
		_, e = s.Reply(reply)
		if e != nil {
			if _, again := s.Reply(json.RawMessage(`{}`)); !errors.Is(again, ErrProtocol) {
				t.Fatal("reply revived")
			}
			return
		}
		if _, e = s.Accept(callFrame(2)); !errors.Is(e, ErrProtocol) {
			t.Fatal("operation limit escaped")
		}
	})
}
