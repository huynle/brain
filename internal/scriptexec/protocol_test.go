package scriptexec

import (
	"bytes"
	"encoding/binary"
	"encoding/json"
	"errors"
	"io"
	"testing"
)

func wire(body string) []byte {
	b := make([]byte, 4)
	binary.BigEndian.PutUint32(b, uint32(len(body)))
	return append(b, body...)
}

func TestFramesRoundTripWithoutConsumingFollowingFrame(t *testing.T) {
	var b bytes.Buffer
	want := Frame{Version: 1, Kind: "call", Sequence: 1, Payload: json.RawMessage(`{"method":"entries.get","arguments":{"id":"a"}}`)}
	if err := WriteFrame(&b, want); err != nil {
		t.Fatal(err)
	}
	if err := WriteFrame(&b, Frame{Version: 1, Kind: "result", Sequence: 2, Payload: json.RawMessage(`null`)}); err != nil {
		t.Fatal(err)
	}
	got, err := ReadFrame(&b)
	if err != nil {
		t.Fatal(err)
	}
	if got.Kind != want.Kind || got.Sequence != 1 || !bytes.Equal(got.Payload, want.Payload) {
		t.Fatalf("%+v", got)
	}
	next, err := ReadFrame(&b)
	if err != nil || next.Kind != "result" {
		t.Fatalf("%+v %v", next, err)
	}
	if _, err := ReadFrame(&b); !errors.Is(err, io.EOF) {
		t.Fatalf("%v", err)
	}
}

type headerOnly struct {
	header    *bytes.Reader
	bodyReads int
}

func (r *headerOnly) Read(p []byte) (int, error) {
	if r.header.Len() == 0 {
		r.bodyReads++
		return 0, errors.New("must not read payload")
	}
	return r.header.Read(p)
}
func TestOversizedFrameRejectedBeforePayloadRead(t *testing.T) {
	header := make([]byte, 4)
	binary.BigEndian.PutUint32(header, MaxFrameBytes+1)
	r := &headerOnly{header: bytes.NewReader(header)}
	_, err := ReadFrame(r)
	if !errors.Is(err, ErrProtocol) || r.bodyReads != 0 {
		t.Fatalf("error=%v reads=%d", err, r.bodyReads)
	}
}

func TestRejectAmbiguousOrMalformedEnvelopes(t *testing.T) {
	for _, body := range []string{
		`{}`, `null`, `[]`,
		`{"version":1,"kind":"call","sequence":1,"payload":null,"principal":"forged"}`,
		`{"version":1,"kind":"call","sequence":1,"sequence":2,"payload":null}`,
		`{"version":1,"kind":"call","sequence":1,"payload":null,"payload":{}}`,
		`{"version":2,"kind":"call","sequence":1,"payload":null}`,
		`{"version":1,"kind":"authority","sequence":1,"payload":null}`,
		`{"version":1,"kind":"call","sequence":0,"payload":null}`,
		`{"version":1,"kind":"call","sequence":1.5,"payload":null}`,
		`{"version":1,"kind":"call","sequence":1,"payload":null} {}`,
	} {
		if _, err := ReadFrame(bytes.NewReader(wire(body))); !errors.Is(err, ErrProtocol) {
			t.Errorf("accepted %s: %v", body, err)
		}
	}
	for _, b := range [][]byte{{0}, {0, 0, 0, 0}, {0, 0, 0, 10, '{'}} {
		if _, err := ReadFrame(bytes.NewReader(b)); !errors.Is(err, ErrProtocol) {
			t.Errorf("accepted %v: %v", b, err)
		}
	}
}

type shortWriter struct{}

func (shortWriter) Write(p []byte) (int, error) { return len(p) - 1, nil }
func TestWriteRejectsInvalidFrameBeforeAnyBytes(t *testing.T) {
	for _, f := range []Frame{{Version: 1, Kind: "call", Sequence: 1, Payload: json.RawMessage(`{`)}, {Version: 1, Kind: "unknown", Sequence: 1, Payload: json.RawMessage(`null`)}, {Version: 1, Kind: "result", Sequence: 1, Payload: json.RawMessage(`"` + string(bytes.Repeat([]byte("x"), MaxFrameBytes)) + `"`)}} {
		var b bytes.Buffer
		if err := WriteFrame(&b, f); !errors.Is(err, ErrProtocol) || b.Len() != 0 {
			t.Fatalf("err=%v bytes=%d", err, b.Len())
		}
	}
	if err := WriteFrame(shortWriter{}, Frame{Version: 1, Kind: "result", Sequence: 1, Payload: json.RawMessage(`null`)}); !errors.Is(err, io.ErrShortWrite) {
		t.Fatalf("%v", err)
	}
}

func FuzzReadFrame(f *testing.F) {
	f.Add(wire(`{"version":1,"kind":"call","sequence":1,"payload":{}}`))
	f.Add([]byte{255, 255, 255, 255})
	f.Add(wire(`{"version":1,"kind":"result","sequence":1,"payload":null}`))
	f.Fuzz(func(t *testing.T, data []byte) {
		if len(data) > MaxFrameBytes+4 {
			return
		}
		frame, err := ReadFrame(bytes.NewReader(data))
		if err != nil {
			return
		}
		var output bytes.Buffer
		if err := WriteFrame(&output, frame); err != nil {
			t.Fatalf("accepted frame cannot encode: %v", err)
		}
		next, err := ReadFrame(&output)
		if err != nil || next.Version != frame.Version || next.Kind != frame.Kind || next.Sequence != frame.Sequence {
			t.Fatalf("roundtrip changed frame: %+v %v", next, err)
		}
	})
}
