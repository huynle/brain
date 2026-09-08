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
)

// These tests cover Hub.FetchChildren + the FrameChildren protocol, mirroring
// how the exec harness (hub_exec_test.go) stands up a real Hub behind a real
// websocket with a fake runner peer. They assert the outgoing frame carries
// Type==FrameChildren with the right SessionID and passes Recursive/Depth
// through, that the runner's response body is returned to the caller, and that
// the no-runner and error paths behave like the sibling FetchHistory RPC.
//
// Reverting hub.go's FetchChildren (or dropping FrameChildren / the
// Recursive/Depth Frame fields from protocol.go) would fail these.

// childrenRunner is a bridge peer that answers a single correlated
// FrameChildren request: it records what it received and replies with either a
// canned body or an error. It is deliberately separate from the exec harness's
// fakeRunner so it can inspect the request frame.
type childrenRunner struct {
	ws  *websocket.Conn
	ctx context.Context

	writeMu sync.Mutex

	mu       sync.Mutex
	got      Frame  // the last FrameChildren request observed
	sawReq   bool   // whether a FrameChildren request arrived
	respBody []byte // body to echo back on the res frame
	respErr  string // error to return instead of a body, if set
}

func (cr *childrenRunner) loop() {
	for {
		_, data, err := cr.ws.Read(cr.ctx)
		if err != nil {
			return
		}
		var f Frame
		if err := json.Unmarshal(data, &f); err != nil || f.ID == "" {
			continue
		}
		if f.Type != FrameChildren {
			// Answer anything else with a bare ack so unrelated round-trips
			// (none expected here) don't wedge.
			cr.send(Frame{Type: FrameRes, ID: f.ID, Status: http.StatusOK})
			continue
		}
		cr.mu.Lock()
		cr.got = f
		cr.sawReq = true
		body, errMsg := cr.respBody, cr.respErr
		cr.mu.Unlock()

		res := Frame{Type: FrameRes, ID: f.ID}
		if errMsg != "" {
			res.Error = errMsg
		} else {
			res.Status = http.StatusOK
			res.Body = body
		}
		cr.send(res)
	}
}

func (cr *childrenRunner) send(f Frame) {
	b, err := json.Marshal(f)
	if err != nil {
		return
	}
	cr.writeMu.Lock()
	defer cr.writeMu.Unlock()
	_ = cr.ws.Write(cr.ctx, websocket.MessageText, b)
}

func (cr *childrenRunner) request() (Frame, bool) {
	cr.mu.Lock()
	defer cr.mu.Unlock()
	return cr.got, cr.sawReq
}

// newChildrenTestHub wires a real Hub to a childrenRunner over a real
// websocket, mirroring newExecTestHub. respBody/respErr configure the reply.
func newChildrenTestHub(t *testing.T, respBody []byte, respErr string) (*Hub, *childrenRunner) {
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

	cr := &childrenRunner{ws: ws, ctx: ctx, respBody: respBody, respErr: respErr}
	go cr.loop()

	waitFor(t, 2*time.Second, func() bool { return hub.Connected(testRunnerID) }, "bridge connection")
	return hub, cr
}

// TestHubFetchChildren_ForwardsFrameAndReturnsBody asserts the happy path: the
// outgoing frame is a FrameChildren carrying the SessionID and the
// Recursive/Depth args, and the runner's response body is returned verbatim.
func TestHubFetchChildren_ForwardsFrameAndReturnsBody(t *testing.T) {
	want := []byte(`[{"session_id":"ses_child","parent_id":"ses_parent","children":[{"session_id":"ses_grand","parent_id":"ses_child"}]}]`)
	hub, cr := newChildrenTestHub(t, want, "")

	body, err := hub.FetchChildren(context.Background(), testRunnerID, "ses_parent", true, 5)
	if err != nil {
		t.Fatalf("FetchChildren: %v", err)
	}
	if string(body) != string(want) {
		t.Errorf("returned body = %s, want %s", body, want)
	}

	req, ok := cr.request()
	if !ok {
		t.Fatal("runner never saw a FrameChildren request")
	}
	if req.Type != FrameChildren {
		t.Errorf("request Type = %q, want %q", req.Type, FrameChildren)
	}
	if req.SessionID != "ses_parent" {
		t.Errorf("request SessionID = %q, want ses_parent", req.SessionID)
	}
	if !req.Recursive {
		t.Errorf("request Recursive = %v, want true", req.Recursive)
	}
	if req.Depth != 5 {
		t.Errorf("request Depth = %d, want 5", req.Depth)
	}
}

// TestHubFetchChildren_FlatArgsPassThrough asserts the non-recursive args are
// forwarded as-is (Recursive false, Depth 0) — proving FetchChildren doesn't
// silently coerce them.
func TestHubFetchChildren_FlatArgsPassThrough(t *testing.T) {
	hub, cr := newChildrenTestHub(t, []byte(`[]`), "")

	body, err := hub.FetchChildren(context.Background(), testRunnerID, "ses_leaf", false, 0)
	if err != nil {
		t.Fatalf("FetchChildren: %v", err)
	}
	if string(body) != "[]" {
		t.Errorf("returned body = %s, want []", body)
	}

	req, ok := cr.request()
	if !ok {
		t.Fatal("runner never saw a FrameChildren request")
	}
	if req.Recursive {
		t.Errorf("request Recursive = %v, want false", req.Recursive)
	}
	if req.Depth != 0 {
		t.Errorf("request Depth = %d, want 0", req.Depth)
	}
	if req.SessionID != "ses_leaf" {
		t.Errorf("request SessionID = %q, want ses_leaf", req.SessionID)
	}
}

// TestHubFetchChildren_RunnerErrorSurfaces asserts that when the runner
// answers a FrameChildren with an Error, FetchChildren returns that as a
// non-nil error and no body — matching FetchHistory's res.Error handling.
func TestHubFetchChildren_RunnerErrorSurfaces(t *testing.T) {
	hub, _ := newChildrenTestHub(t, nil, "opencode db: no such file")

	body, err := hub.FetchChildren(context.Background(), testRunnerID, "ses_parent", true, 3)
	if err == nil {
		t.Fatal("expected error to surface from the runner's res.Error")
	}
	if !strings.Contains(err.Error(), "opencode db") {
		t.Errorf("error = %v, want it to carry the runner's message", err)
	}
	if body != nil {
		t.Errorf("body = %s, want nil on error", body)
	}
}

// TestHubFetchChildren_NoRunnerReturnsError asserts the no-connection path
// returns ErrRunnerNotConnected, the same guard every other Hub RPC uses.
func TestHubFetchChildren_NoRunnerReturnsError(t *testing.T) {
	rt := realtime.NewHub()
	hub := NewHub(rt)
	// No ServeBridge / dial: the runner is not connected.

	_, err := hub.FetchChildren(context.Background(), "runner-absent", "ses_parent", true, 5)
	if err != ErrRunnerNotConnected {
		t.Errorf("err = %v, want ErrRunnerNotConnected", err)
	}
}
