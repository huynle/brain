package scriptexec

import (
	"bytes"
	"context"
	"encoding/binary"
)

// frameSink is an inactive, single-writer framing adapter for os/exec's stdout
// copy goroutine. It bounds TOTAL wire bytes (including JSON padding), assembles
// at most one capped envelope, and cancels on protocol/admission errors. accept
// is trusted parsing/quarantine only: it must be bounded and must not release
// protected output or dispatch an operation without the future authority broker.
// Join the writer, then call finish; separately require a terminal protocol result.
// This adapter does not authorize methods, make callbacks cancelable, or prove
// the child is confined. There are no production call sites.
type frameSink struct {
	remaining int
	cancel    context.CancelCauseFunc
	accept    func(Frame) error
	buffer    []byte
	closed    bool
}

func (s *frameSink) finish() error {
	bad := s.closed || len(s.buffer) != 0
	s.closed = true
	s.buffer = nil
	if bad {
		s.cancel(ErrProtocol)
		return ErrProtocol
	}
	return nil
}

func (s *frameSink) Write(p []byte) (int, error) {
	consumed := 0
	bad := func() (int, error) {
		s.closed = true
		s.buffer = nil
		s.cancel(ErrProtocol)
		return consumed, ErrProtocol
	}
	if s.closed {
		return consumed, ErrProtocol
	}
	for len(p) > 0 {
		target := 4
		if len(s.buffer) >= 4 {
			size := binary.BigEndian.Uint32(s.buffer[:4])
			if size == 0 || size > MaxFrameBytes {
				return bad()
			}
			target += int(size)
		}
		n := min(len(p), target-len(s.buffer))
		if n > s.remaining {
			return bad()
		}
		s.buffer = append(s.buffer, p[:n]...)
		s.remaining -= n
		consumed += n
		p = p[n:]
		if len(s.buffer) < 4 {
			continue
		}
		size := binary.BigEndian.Uint32(s.buffer[:4])
		if size == 0 || size > MaxFrameBytes {
			return bad()
		}
		if len(s.buffer) == int(size)+4 {
			f, err := ReadFrame(bytes.NewReader(s.buffer))
			if err != nil {
				return bad()
			}
			if err = s.accept(f); err != nil {
				return bad()
			}
			s.buffer = s.buffer[:0]
		}
	}
	return consumed, nil
}
