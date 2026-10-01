package runner

import (
	"context"
	"encoding/base64"
	"net/http"
	"net/http/httptest"
	"os"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/huynle/brain-api/internal/bridge"
	"github.com/huynle/brain-api/internal/types"
)

// v2 bridge: instanceHealthy -> GET /api/info (+auth); tailEvents -> /api/event
// (+auth); the generic proxy /api-prefixes and authenticates forwarded
// /session* paths.

func portOf(t *testing.T, srv *httptest.Server) int {
	t.Helper()
	i := strings.LastIndex(srv.URL, ":")
	p, err := strconv.Atoi(srv.URL[i+1:])
	if err != nil {
		t.Fatalf("port parse: %v", err)
	}
	return p
}

func TestInstanceHealthy_V2_InfoPathAuth(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/info" {
			t.Fatalf("want /api/info, got %s", r.URL.Path)
		}
		want := "Basic " + base64.StdEncoding.EncodeToString([]byte("opencode:pwH"))
		if got := r.Header.Get("Authorization"); got != want {
			// no-auth or wrong-auth must be treated as unhealthy
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"version":"2.0.18","pid":1,"urls":["http://127.0.0.1:1"]}`))
	}))
	defer srv.Close()
	port := portOf(t, srv)

	if !instanceHealthy(port, "pwH") {
		t.Fatal("instanceHealthy = false with correct auth, want true")
	}
	if instanceHealthy(port, "wrongpw") {
		t.Fatal("instanceHealthy = true with wrong auth, want false (401)")
	}
}

// v2 adhoc instances cannot be re-adopted across a runner restart: their
// per-serve OPENCODE_PASSWORD is in-memory only (never persisted), so under
// v2 Basic auth they are undriveable. A still-live orphan must be reaped; a
// dead PID needs no action.
func TestAdhocOrphanNeedsReap(t *testing.T) {
	// A live PID (this test process) is a reapable orphan.
	if !adhocOrphanNeedsReap(types.OpencodeInstance{InstanceID: "a", PID: os.Getpid(), Port: 5252}) {
		t.Error("live-PID adhoc orphan should be reaped")
	}
	// PID 0 / unset: nothing to reap.
	if adhocOrphanNeedsReap(types.OpencodeInstance{InstanceID: "b", PID: 0}) {
		t.Error("PID 0 should not be reaped")
	}
	// A dead PID: nothing to reap (IsPidAlive false). Use an implausibly high PID.
	if adhocOrphanNeedsReap(types.OpencodeInstance{InstanceID: "c", PID: 2147480000}) {
		t.Error("dead PID should not be reaped")
	}
}

func TestPasswordForInstance_AdhocAndTracked(t *testing.T) {
	pm := NewProcessManager(RunnerConfig{APITimeout: 5000})
	task := RunningTask{ID: "t1", InstanceID: "inst_tracked", OpencodePort: 4242, OpencodePassword: "trackedpw", ExecutorType: "opencode"}
	if err := pm.Add(task.ID, task, newMockProcess(os.Getpid())); err != nil {
		t.Fatalf("track: %v", err)
	}
	tr := &TaskRunner{processMgr: pm, config: RunnerConfig{StateDir: t.TempDir()}}
	bc := NewBridgeClient(tr)
	bc.adhoc["inst_adhoc"] = &adhocInstance{
		Instance: types.OpencodeInstance{InstanceID: "inst_adhoc", Port: 5252},
		password: "adhocpw",
	}

	if got := bc.passwordForInstance("inst_adhoc"); got != "adhocpw" {
		t.Fatalf("adhoc password = %q, want adhocpw", got)
	}
	if got := bc.passwordForInstance("inst_tracked"); got != "trackedpw" {
		t.Fatalf("tracked password = %q, want trackedpw", got)
	}
	if got := bc.passwordForPort(5252); got != "adhocpw" {
		t.Fatalf("passwordForPort(adhoc) = %q, want adhocpw", got)
	}
	if got := bc.passwordForPort(4242); got != "trackedpw" {
		t.Fatalf("passwordForPort(tracked) = %q, want trackedpw", got)
	}
}

// The generic proxy must /api-prefix a forwarded bare /session* path and
// attach Basic auth from the instance's password.
func TestProxyRequest_V2_PrefixesApiAndAuths(t *testing.T) {
	var gotPath, gotAuth string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		gotAuth = r.Header.Get("Authorization")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"data":{}}`))
	}))
	defer srv.Close()
	port := portOf(t, srv)

	pm := NewProcessManager(RunnerConfig{APITimeout: 5000})
	task := RunningTask{ID: "t1", InstanceID: "inst_x", OpencodePort: port, OpencodePassword: "proxypw", ExecutorType: "opencode"}
	if err := pm.Add(task.ID, task, newMockProcess(os.Getpid())); err != nil {
		t.Fatalf("track: %v", err)
	}
	tr := &TaskRunner{processMgr: pm, config: RunnerConfig{StateDir: t.TempDir()}}
	bc := NewBridgeClient(tr)

	// A caller-supplied bare v1-style path must be forwarded to /api/...
	status, _, err := bc.proxyRequest(bridge.Frame{
		InstanceID: "inst_x",
		Method:     http.MethodGet,
		Path:       "/session/ses_a/message",
	})
	if err != nil {
		t.Fatalf("proxyRequest: %v", err)
	}
	if status != http.StatusOK {
		t.Fatalf("status = %d, want 200", status)
	}
	if gotPath != "/api/session/ses_a/message" {
		t.Fatalf("forwarded path = %q, want /api/session/ses_a/message", gotPath)
	}
	want := "Basic " + base64.StdEncoding.EncodeToString([]byte("opencode:proxypw"))
	if gotAuth != want {
		t.Fatalf("forwarded auth = %q, want %q", gotAuth, want)
	}
}

func TestTailEvents_V2_ApiEventPathAuth(t *testing.T) {
	gotPath := make(chan string, 1)
	gotAuth := make(chan string, 1)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath <- r.URL.Path
		gotAuth <- r.Header.Get("Authorization")
		w.Header().Set("Content-Type", "text/event-stream")
		fl := w.(http.Flusher)
		_, _ = w.Write([]byte("data: {\"type\":\"server.connected\",\"data\":{}}\n\n"))
		fl.Flush()
		<-r.Context().Done()
	}))
	defer srv.Close()
	port := portOf(t, srv)

	pm := NewProcessManager(RunnerConfig{APITimeout: 5000})
	task := RunningTask{ID: "t1", InstanceID: "inst_ev", OpencodePort: port, OpencodePassword: "evpw", ExecutorType: "opencode"}
	if err := pm.Add(task.ID, task, newMockProcess(os.Getpid())); err != nil {
		t.Fatalf("track: %v", err)
	}
	tr := &TaskRunner{processMgr: pm, config: RunnerConfig{StateDir: t.TempDir()}}
	bc := NewBridgeClient(tr)

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	go func() { _ = bc.tailEvents(ctx, "inst_ev", port) }()

	select {
	case p := <-gotPath:
		if p != "/api/event" {
			t.Fatalf("tailEvents path = %q, want /api/event", p)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("tailEvents never hit the server")
	}
	want := "Basic " + base64.StdEncoding.EncodeToString([]byte("opencode:evpw"))
	if got := <-gotAuth; got != want {
		t.Fatalf("tailEvents auth = %q, want %q", got, want)
	}
}
