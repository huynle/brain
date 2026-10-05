package scriptexec

import (
	"bytes"
	"encoding/json"
	"io"
	"regexp"
	"sync"
	"unicode/utf8"
)

// ProtocolLimits are parent-owned message budgets, not authorization or quota.
type ProtocolLimits struct{ MaxOperations, MaxPayloadBytes, MaxTotalBytes int }
type OperationCall struct {
	Operation string
	Arguments json.RawMessage
}
type WorkerMessage struct {
	Call   *OperationCall
	Result json.RawMessage
	Done   bool
}

// ProtocolSession is only a parent-side IPC state machine. A parsed call is NOT
// an allowed operation. The future broker must resolve an explicit registry,
// decode operation-specific arguments, authorize/preflight and fence all effects
// and outputs. There are no callbacks, services, credentials or network handles
// here. Retire on any external pipe error; never retry a partially written reply.
// Create one fresh instance per child; do not copy it or reuse across executions.
type ProtocolSession struct {
	mu                sync.Mutex
	limits            ProtocolLimits
	next              uint64
	operations, bytes int
	pending, closed   bool
}

var operationName = regexp.MustCompile(`^[a-z][a-zA-Z0-9]{0,63}\.[a-z][a-zA-Z0-9]{0,63}$`)

func NewProtocolSession(limits ProtocolLimits) (*ProtocolSession, error) {
	if limits.MaxOperations < 1 || limits.MaxOperations > 10000 || limits.MaxPayloadBytes < 1 || limits.MaxPayloadBytes > MaxFrameBytes || limits.MaxTotalBytes < limits.MaxPayloadBytes || limits.MaxTotalBytes > 16*MaxFrameBytes {
		return nil, ErrProtocol
	}
	return &ProtocolSession{limits: limits, next: 1}, nil
}
func (s *ProtocolSession) Accept(frame Frame) (WorkerMessage, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	bad := func() (WorkerMessage, error) { s.closed = true; return WorkerMessage{}, ErrProtocol }
	if s.closed || s.next == 0 || s.pending || len(frame.Payload) > s.limits.MaxPayloadBytes || !validFrame(frame) || frame.Sequence != s.next || !s.consume(frame.Payload) {
		return bad()
	}
	if frame.Kind == "result" {
		s.closed = true
		return WorkerMessage{Done: true, Result: bytes.Clone(frame.Payload)}, nil
	}
	if s.operations >= s.limits.MaxOperations {
		return bad()
	}
	var fields map[string]json.RawMessage
	if json.Unmarshal(frame.Payload, &fields) != nil || len(fields) != 2 || fields["operation"] == nil || fields["arguments"] == nil {
		return bad()
	}
	var name string
	if json.Unmarshal(fields["operation"], &name) != nil || !operationName.MatchString(name) {
		return bad()
	}
	args := bytes.TrimSpace(fields["arguments"])
	if len(args) == 0 || args[0] != '{' {
		return bad()
	}
	s.operations++
	s.pending = true
	return WorkerMessage{Call: &OperationCall{Operation: name, Arguments: bytes.Clone(args)}}, nil
}

func (s *ProtocolSession) Reply(payload json.RawMessage) (Frame, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed || !s.pending || !s.consume(payload) {
		s.closed = true
		return Frame{}, ErrProtocol
	}
	frame := Frame{1, "result", s.next, bytes.Clone(payload)}
	s.next++
	s.pending = false
	return frame, nil
}

func (s *ProtocolSession) Retire() { s.mu.Lock(); defer s.mu.Unlock(); s.closed = true }

func (s *ProtocolSession) consume(payload []byte) bool {
	if len(payload) == 0 || len(payload) > s.limits.MaxPayloadBytes || len(payload) > s.limits.MaxTotalBytes-s.bytes || !utf8.Valid(payload) {
		return false
	}
	dec := json.NewDecoder(bytes.NewReader(payload))
	dec.UseNumber()
	if !uniqueJSON(dec, 0) {
		return false
	}
	if _, err := dec.Token(); err != io.EOF {
		return false
	}
	s.bytes += len(payload)
	return true
}

// Reject duplicate decoded keys at every nesting level and bound recursion before
// handing opaque arguments to a future operation-specific decoder. Escaped aliases
// such as id and \u0069d are the same key. No input text enters protocol errors.
func uniqueJSON(dec *json.Decoder, depth int) bool {
	if depth > 64 {
		return false
	}
	token, err := dec.Token()
	if err != nil {
		return false
	}
	switch token {
	case json.Delim('{'):
		seen := map[string]bool{}
		for dec.More() {
			k, err := dec.Token()
			key, ok := k.(string)
			if err != nil || !ok || seen[key] {
				return false
			}
			seen[key] = true
			if !uniqueJSON(dec, depth+1) {
				return false
			}
		}
		end, err := dec.Token()
		return err == nil && end == json.Delim('}')
	case json.Delim('['):
		for dec.More() {
			if !uniqueJSON(dec, depth+1) {
				return false
			}
		}
		end, err := dec.Token()
		return err == nil && end == json.Delim(']')
	case json.Delim('}'), json.Delim(']'):
		return false
	default:
		return true
	}
}
