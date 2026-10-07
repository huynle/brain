// Package scriptexec contains private, not-yet-enabled script execution building
// blocks. A protocol codec is neither an authorization boundary nor a sandbox.
package scriptexec

import (
	"bytes"
	"encoding/binary"
	"encoding/json"
	"errors"
	"io"
)

const MaxFrameBytes = 1 << 20

var ErrProtocol = errors.New("invalid script worker protocol")

type Frame struct {
	Version  int             `json:"version"`
	Kind     string          `json:"kind"`
	Sequence uint64          `json:"sequence"`
	Payload  json.RawMessage `json:"payload"`
}

// ReadFrame consumes exactly one length-prefixed envelope. It checks the fixed
// ceiling before allocation or payload reads. The caller owns deadlines and
// process cancellation; this codec must not be used without those controls.
func ReadFrame(r io.Reader) (Frame, error) {
	var header [4]byte
	n, err := io.ReadFull(r, header[:])
	if n == 0 && errors.Is(err, io.EOF) {
		return Frame{}, io.EOF
	}
	if err != nil {
		return Frame{}, ErrProtocol
	}
	size := binary.BigEndian.Uint32(header[:])
	if size == 0 || size > MaxFrameBytes {
		return Frame{}, ErrProtocol
	}
	body := make([]byte, int(size))
	if _, err := io.ReadFull(r, body); err != nil {
		return Frame{}, ErrProtocol
	}
	dec := json.NewDecoder(bytes.NewReader(body))
	token, err := dec.Token()
	if err != nil || token != json.Delim('{') {
		return Frame{}, ErrProtocol
	}
	seen := map[string]bool{}
	var frame Frame
	for dec.More() {
		token, err := dec.Token()
		if err != nil {
			return Frame{}, ErrProtocol
		}
		key, ok := token.(string)
		if !ok || seen[key] {
			return Frame{}, ErrProtocol
		}
		seen[key] = true
		switch key {
		case "version":
			err = dec.Decode(&frame.Version)
		case "kind":
			err = dec.Decode(&frame.Kind)
		case "sequence":
			err = dec.Decode(&frame.Sequence)
		case "payload":
			err = dec.Decode(&frame.Payload)
		default:
			return Frame{}, ErrProtocol
		}
		if err != nil {
			return Frame{}, ErrProtocol
		}
	}
	if token, err := dec.Token(); err != nil || token != json.Delim('}') {
		return Frame{}, ErrProtocol
	}
	if _, err := dec.Token(); !errors.Is(err, io.EOF) {
		return Frame{}, ErrProtocol
	}
	if len(seen) != 4 || !validFrame(frame) {
		return Frame{}, ErrProtocol
	}
	return frame, nil
}

func validFrame(f Frame) bool {
	return f.Version == 1 && (f.Kind == "call" || f.Kind == "result" || f.Kind == "error") && f.Sequence > 0 && f.Sequence <= 1<<53-1 && len(f.Payload) > 0 && json.Valid(f.Payload)
}

// WriteFrame validates and buffers one envelope before emitting anything. The
// writer must be serialized by its owner. An error retires the IPC stream; never
// retry a partial frame on the same stream.
func WriteFrame(w io.Writer, f Frame) error {
	if len(f.Payload) > MaxFrameBytes || !validFrame(f) {
		return ErrProtocol
	}
	body, err := json.Marshal(f)
	if err != nil || len(body) > MaxFrameBytes {
		return ErrProtocol
	}
	buf := make([]byte, 4+len(body))
	binary.BigEndian.PutUint32(buf, uint32(len(body)))
	copy(buf[4:], body)
	n, err := w.Write(buf)
	if err != nil {
		return err
	}
	if n != len(buf) {
		return io.ErrShortWrite
	}
	return nil
}
