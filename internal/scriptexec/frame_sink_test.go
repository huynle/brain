package scriptexec

import (
	"bytes"
	"context"
	"errors"
	"os"
	"os/exec"
	"testing"
	"time"
)

func TestFrameSinkPacketizationAndWireBudget(t *testing.T) {
	var wire bytes.Buffer
	if err := WriteFrame(&wire, Frame{1, "result", 1, []byte(`42`)}); err != nil {
		t.Fatal(err)
	}
	for chunk := 1; chunk <= wire.Len(); chunk++ {
		ctx, cancel := context.WithCancelCause(context.Background())
		calls := 0
		sink := &frameSink{remaining: wire.Len(), cancel: cancel, accept: func(f Frame) error {
			calls++
			if f.Kind != "result" || string(f.Payload) != "42" {
				t.Fatalf("frame=%+v", f)
			}
			return nil
		}}
		data := wire.Bytes()
		for len(data) > 0 {
			n := min(chunk, len(data))
			if written, err := sink.Write(data[:n]); written != n || err != nil {
				t.Fatalf("chunk=%d written=%d err=%v", chunk, written, err)
			}
			data = data[n:]
		}
		if calls != 1 || len(sink.buffer) != 0 || ctx.Err() != nil {
			t.Fatalf("chunk=%d calls=%d err=%v", chunk, calls, ctx.Err())
		}
		if _, err := sink.Write([]byte{0}); !errors.Is(err, ErrProtocol) || !errors.Is(context.Cause(ctx), ErrProtocol) {
			t.Fatalf("wire budget overflow=%v", err)
		}
		if _, err := sink.Write(wire.Bytes()); !errors.Is(err, ErrProtocol) || calls != 1 {
			t.Fatal("retired sink accepted more output")
		}
		cancel(nil)
	}
}

func TestFrameSinkHostileHeadersAndEnvelope(t *testing.T) {
	for _, input := range [][]byte{{0, 0, 0, 0}, {0xff, 0xff, 0xff, 0xff}, {0, 0, 0, 1, 'x'}} {
		ctx, cancel := context.WithCancelCause(context.Background())
		sink := &frameSink{remaining: 4096, cancel: cancel, accept: func(Frame) error { t.Fatal("invalid envelope accepted"); return nil }}
		if _, err := sink.Write(input); !errors.Is(err, ErrProtocol) || !errors.Is(context.Cause(ctx), ErrProtocol) || len(sink.buffer) != 0 {
			t.Fatalf("input=%x err=%v retained=%d", input, err, len(sink.buffer))
		}
		cancel(nil)
	}
}

func TestFrameSinkRejectsTruncatedEOF(t *testing.T) {
	for _, partial := range [][]byte{{0}, {0, 0, 0, 4, '{'}} {
		ctx, cancel := context.WithCancelCause(context.Background())
		sink := &frameSink{remaining: 4096, cancel: cancel, accept: func(Frame) error { t.Fatal("partial frame delivered"); return nil }}
		if _, err := sink.Write(partial); err != nil {
			t.Fatal(err)
		}
		if err := sink.finish(); !errors.Is(err, ErrProtocol) || !errors.Is(context.Cause(ctx), ErrProtocol) {
			t.Fatalf("truncated EOF accepted: err=%v cause=%v", err, context.Cause(ctx))
		}
		cancel(nil)
	}
}

func TestFrameSinkCleanEOFRetires(t *testing.T) {
	ctx, cancel := context.WithCancelCause(context.Background())
	defer cancel(nil)
	sink := &frameSink{remaining: 4096, cancel: cancel, accept: func(Frame) error { return nil }}
	var wire bytes.Buffer
	if err := WriteFrame(&wire, Frame{1, "result", 1, []byte(`null`)}); err != nil {
		t.Fatal(err)
	}
	if _, err := sink.Write(wire.Bytes()); err != nil {
		t.Fatal(err)
	}
	if err := sink.finish(); err != nil || ctx.Err() != nil || sink.buffer != nil {
		t.Fatalf("clean EOF=%v retained=%d", err, len(sink.buffer))
	}
	if _, err := sink.Write(wire.Bytes()); !errors.Is(err, ErrProtocol) {
		t.Fatal("write after EOF accepted")
	}
}

func FuzzFrameSink(f *testing.F) {
	var frame bytes.Buffer
	_ = WriteFrame(&frame, Frame{1, "result", 1, []byte(`42`)})
	f.Add(frame.Bytes(), uint8(1))
	f.Add([]byte{0xff, 0xff, 0xff, 0xff}, uint8(3))
	f.Fuzz(func(t *testing.T, data []byte, packet uint8) {
		ctx, cancel := context.WithCancelCause(context.Background())
		defer cancel(nil)
		session, err := NewProtocolSession(ProtocolLimits{2, 1024, 4096})
		if err != nil {
			t.Fatal(err)
		}
		defer session.Retire()
		sink := &frameSink{remaining: 4096, cancel: cancel, accept: func(frame Frame) error { _, err := session.Accept(frame); return err }}
		for len(data) > 0 {
			n := min(int(packet)+1, len(data))
			_, err = sink.Write(data[:n])
			data = data[n:]
			if len(sink.buffer) > 4100 || sink.remaining < 0 {
				t.Fatal("unbounded assembly")
			}
			if err != nil {
				if !errors.Is(context.Cause(ctx), ErrProtocol) {
					t.Fatal("missing cancellation")
				}
				if _, again := sink.Write(frame.Bytes()); !errors.Is(again, ErrProtocol) {
					t.Fatal("sink not retired")
				}
				return
			}
		}
		_ = sink.finish()
		if !sink.closed || sink.buffer != nil {
			t.Fatal("EOF did not retire")
		}
	})
}

func TestWorkerMaliciousFrameFloodCancelledAndReaped(t *testing.T) {
	outer, stop := context.WithTimeout(context.Background(), 3*time.Second)
	defer stop()
	ctx, cancel := context.WithCancelCause(outer)
	defer cancel(nil)
	session, err := NewProtocolSession(ProtocolLimits{1, 1024, 4096})
	if err != nil {
		t.Fatal(err)
	}
	defer session.Retire()
	admitted := 0
	sink := &frameSink{remaining: 4096, cancel: cancel, accept: func(f Frame) error {
		_, err := session.Accept(f)
		if err == nil {
			admitted++
		}
		return err
	}}
	cmd := exec.Command(os.Args[0], "-test.run=^TestProcessFixture$")
	cmd.Env = []string{"BRAIN_PROCESS_FIXTURE=frames"}
	cmd.Stdout = sink
	err = runWorkerProcess(ctx, cmd)
	if !errors.Is(err, ErrProtocol) || admitted != 1 {
		t.Fatalf("malicious flood not retired: err=%v admitted=%d", err, admitted)
	}
	if outer.Err() != nil || cmd.ProcessState == nil || cmd.ProcessState.Success() {
		t.Fatalf("flood not killed and reaped independently of deadline: %v state=%v", outer.Err(), cmd.ProcessState)
	}
}
