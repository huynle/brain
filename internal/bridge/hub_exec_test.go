package bridge

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/coder/websocket"
	"github.com/huynle/brain-api/internal/realtime"
	"github.com/huynle/brain-api/internal/types"
)

const testRunnerID = "runner-1"

// fakeRunner is a bridge peer that acks every correlated frame and pushes
// exec frames on demand — enough to drive the hub's exec bookkeeping without
// a real runner process.
type fakeRunner struct {
	t  *testing.T
	ws *websocket.Conn

	writeMu sync.Mutex
	ctx     context.Context

	mu       sync.Mutex
	startErr string // returned to the next exec_start, if set
}

// newExecTestHub wires a real Hub to a fakeRunner over a real websocket.
func newExecTestHub(t *testing.T) (*Hub, *realtime.Hub, *fakeRunner) {
	t.Helper()
	rt := realtime.NewHub()
	hub := NewHub(rt)

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hub.ServeBridge(w, r, testRunnerID)
	}))
	t.Cleanup(srv.Close)

	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)

	ws, _, err := websocket.Dial(ctx, "ws"+strings.TrimPrefix(srv.URL, "http"), nil)
	if err != nil {
		t.Fatalf("dial bridge: %v", err)
	}
	t.Cleanup(func() { _ = ws.Close(websocket.StatusNormalClosure, "test over") })

	fr := &fakeRunner{t: t, ws: ws, ctx: ctx}
	go fr.ackLoop()

	waitFor(t, 2*time.Second, func() bool { return hub.Connected(testRunnerID) }, "bridge connection")
	return hub, rt, fr
}

// ackLoop answers every correlated frame so hub round-trips complete.
func (fr *fakeRunner) ackLoop() {
	for {
		_, data, err := fr.ws.Read(fr.ctx)
		if err != nil {
			return
		}
		var f Frame
		if err := json.Unmarshal(data, &f); err != nil || f.ID == "" {
			continue
		}
		fr.mu.Lock()
		errMsg := fr.startErr
		fr.mu.Unlock()
		fr.send(Frame{Type: FrameRes, ID: f.ID, Status: http.StatusOK, Error: errMsg})
	}
}

func (fr *fakeRunner) send(f Frame) {
	b, err := json.Marshal(f)
	if err != nil {
		return
	}
	fr.writeMu.Lock()
	defer fr.writeMu.Unlock()
	_ = fr.ws.Write(fr.ctx, websocket.MessageText, b)
}

// die drops the connection the way a SIGKILLed runner would: no close
// handshake, no exec_exit for anything still running.
func (fr *fakeRunner) die() {
	fr.writeMu.Lock()
	defer fr.writeMu.Unlock()
	_ = fr.ws.CloseNow()
}

func waitFor(t *testing.T, timeout time.Duration, cond func() bool, what string) {
	t.Helper()
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		if cond() {
			return
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatalf("timed out waiting for %s", what)
}

// collect reads events off an exec subscription until the terminal one, and
// returns them in order.
func collect(t *testing.T, ch <-chan realtime.SSEMessage, timeout time.Duration) []realtime.SSEMessage {
	t.Helper()
	var got []realtime.SSEMessage
	deadline := time.After(timeout)
	for {
		select {
		case msg := <-ch:
			got = append(got, msg)
			if msg.Event == "exec_exit" {
				return got
			}
		case <-deadline:
			t.Fatalf("timed out after %d events, last = %+v", len(got), got)
			return got
		}
	}
}

func TestHubExec_DeliversOutputAndRecordsOutcome(t *testing.T) {
	hub, rt, fr := newExecTestHub(t)
	const execID = "exec-ok"

	ch, unsub := rt.SubscribeWithCapacity(realtime.ExecTopic(testRunnerID, execID), 1024)
	defer unsub()

	if err := hub.StartExec(context.Background(), testRunnerID, execID, "echo hi", "", 0); err != nil {
		t.Fatalf("StartExec: %v", err)
	}
	for _, chunk := range []string{"one\n", "two\n", "three\n"} {
		fr.send(Frame{Type: FrameExecData, ExecID: execID, Stream: ExecStreamStdout, Chunk: chunk})
	}
	fr.send(Frame{Type: FrameExecExit, ExecID: execID, ExitCode: 7, Error: "boom"})

	got := collect(t, ch, 5*time.Second)
	if len(got) != 4 {
		t.Fatalf("got %d events, want 3 data + 1 exit", len(got))
	}

	outcome, ok := hub.ExecOutcome(testRunnerID, execID)
	if !ok {
		t.Fatal("exec is no longer tracked")
	}
	if !outcome.Done || outcome.ExitCode != 7 || outcome.Error != "boom" {
		t.Errorf("outcome = %+v, want done exit 7 boom", outcome)
	}
	if outcome.DroppedChunks != 0 || outcome.DroppedBytes != 0 {
		t.Errorf("nothing should have been dropped, got %+v", outcome)
	}
}

// Output the fan-out cannot deliver must be counted, not swallowed: the
// counters are what let the streaming handler tell the user its transcript is
// incomplete. The read loop must never block waiting for a slow browser.
func TestHubExec_CountsDroppedOutput(t *testing.T) {
	hub, rt, fr := newExecTestHub(t)
	const execID = "exec-flood"

	// A default-depth subscriber that never reads: everything past its
	// buffer is lost.
	_, unsub := rt.Subscribe(realtime.ExecTopic(testRunnerID, execID))
	defer unsub()

	if err := hub.StartExec(context.Background(), testRunnerID, execID, "cat huge", "", 0); err != nil {
		t.Fatalf("StartExec: %v", err)
	}
	const chunks = realtime.DefaultSubscriberBuffer * 4
	for i := 0; i < chunks; i++ {
		fr.send(Frame{Type: FrameExecData, ExecID: execID, Stream: ExecStreamStdout, Chunk: "0123456789"})
	}
	fr.send(Frame{Type: FrameExecExit, ExecID: execID, ExitCode: 0})

	waitFor(t, 5*time.Second, func() bool {
		outcome, ok := hub.ExecOutcome(testRunnerID, execID)
		return ok && outcome.Done
	}, "exec outcome")

	outcome, _ := hub.ExecOutcome(testRunnerID, execID)
	wantDropped := chunks - realtime.DefaultSubscriberBuffer
	if outcome.DroppedChunks < wantDropped {
		t.Errorf("dropped chunks = %d, want at least %d", outcome.DroppedChunks, wantDropped)
	}
	if outcome.DroppedBytes < wantDropped*10 {
		t.Errorf("dropped bytes = %d, want at least %d", outcome.DroppedBytes, wantDropped*10)
	}
}

// A runner killed mid-command never sends an exec_exit. The hub must end the
// exec itself, or every stream watching it waits forever.
func TestHubExec_RunnerDisconnectEndsExec(t *testing.T) {
	hub, rt, fr := newExecTestHub(t)
	const execID = "exec-orphan"

	ch, unsub := rt.SubscribeWithCapacity(realtime.ExecTopic(testRunnerID, execID), 64)
	defer unsub()

	if err := hub.StartExec(context.Background(), testRunnerID, execID, "sleep 300", "", 0); err != nil {
		t.Fatalf("StartExec: %v", err)
	}
	fr.send(Frame{Type: FrameExecData, ExecID: execID, Stream: ExecStreamStdout, Chunk: "partial\n"})
	fr.die()

	got := collect(t, ch, 5*time.Second)
	last := got[len(got)-1]
	data, _ := last.Data.(map[string]any)
	if msg, _ := data["error"].(string); !strings.Contains(msg, "runner disconnected") {
		t.Errorf("synthetic exit error = %q, want it to explain the disconnect", msg)
	}

	outcome, ok := hub.ExecOutcome(testRunnerID, execID)
	if !ok || !outcome.Done {
		t.Fatalf("outcome = %+v tracked=%v, want a recorded end", outcome, ok)
	}
	if outcome.ExitCode != types.ExecExitUnknown {
		t.Errorf("exit code = %d, want %d (status unknowable)", outcome.ExitCode, types.ExecExitUnknown)
	}
}

// Per-exec state is bounded by live streams, not by commands ever run.
func TestHubExec_StateIsReleased(t *testing.T) {
	hub, _, fr := newExecTestHub(t)

	if err := hub.StartExec(context.Background(), testRunnerID, "exec-1", "true", "", 0); err != nil {
		t.Fatalf("StartExec: %v", err)
	}
	if got := hub.execCount(); got != 1 {
		t.Fatalf("tracked execs = %d, want 1", got)
	}
	hub.ReleaseExec(testRunnerID, "exec-1")
	if got := hub.execCount(); got != 0 {
		t.Errorf("tracked execs = %d after release, want 0", got)
	}
	if _, ok := hub.ExecOutcome(testRunnerID, "exec-1"); ok {
		t.Error("released exec is still tracked")
	}

	// A command the runner refuses leaves nothing behind either.
	fr.mu.Lock()
	fr.startErr = "workdir does not exist"
	fr.mu.Unlock()
	if err := hub.StartExec(context.Background(), testRunnerID, "exec-2", "true", "/nope", 0); err == nil {
		t.Fatal("expected StartExec to fail")
	}
	if got := hub.execCount(); got != 0 {
		t.Errorf("tracked execs = %d after a refused start, want 0", got)
	}
}

// execCount reports how many commands the hub is tracking.
func (h *Hub) execCount() int {
	h.mu.Lock()
	defer h.mu.Unlock()
	return len(h.execs)
}
