package runner

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/huynle/brain-api/internal/bridge"
	"github.com/huynle/brain-api/internal/realtime"
	"github.com/huynle/brain-api/internal/types"
)

// fakeOpencode is a minimal stand-in for an OpenCode HTTP server: serves
// /session, /session/{id}/prompt_async, and a one-shot /event SSE stream.
func fakeOpencode(t *testing.T) (*httptest.Server, int) {
	t.Helper()
	mux := http.NewServeMux()
	mux.HandleFunc("/session", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprint(w, `[{"id":"ses_test","time":{"updated":100}}]`)
	})
	mux.HandleFunc("/session/ses_test/prompt_async", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNoContent)
	})
	mux.HandleFunc("/event", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		flusher := w.(http.Flusher)
		// One control event, one full-stream event.
		fmt.Fprint(w, "data: {\"type\":\"permission.updated\",\"properties\":{\"id\":\"perm_1\"}}\n\n")
		flusher.Flush()
		fmt.Fprint(w, "data: {\"type\":\"message.part.updated\",\"properties\":{\"text\":\"hi\"}}\n\n")
		flusher.Flush()
		<-r.Context().Done()
	})

	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	u, _ := url.Parse(srv.URL)
	port, _ := strconv.Atoi(u.Port())
	return srv, port
}

// newBridgeTestEnv stands up a real bridge Hub behind an httptest API server
// and a real BridgeClient on a minimal TaskRunner, with one tracked task
// instance pointing at the fake opencode port.
func newBridgeTestEnv(t *testing.T, opencodePort int) (*bridge.Hub, *realtime.Hub, *TaskRunner, string, string) {
	t.Helper()

	rt := realtime.NewHub()
	hub := bridge.NewHub(rt)

	router := chi.NewRouter()
	router.Get("/api/v1/runners/{runnerId}/bridge", func(w http.ResponseWriter, r *http.Request) {
		hub.ServeBridge(w, r, chi.URLParam(r, "runnerId"))
	})
	apiSrv := httptest.NewServer(router)
	t.Cleanup(apiSrv.Close)

	runnerID := "runner_bridge_test"
	instanceID := "inst_test01"

	pm := NewProcessManager(RunnerConfig{APITimeout: 5000})
	task := RunningTask{
		ID:           "task-1",
		Path:         "projects/p/task/t1.md",
		Title:        "bridge test task",
		ProjectID:    "p",
		PID:          os.Getpid(),
		StartedAt:    time.Now(),
		ExecutorType: "opencode",
		InstanceID:   instanceID,
		OpencodePort: opencodePort,
	}
	if err := pm.Add(task.ID, task, newMockProcess(os.Getpid())); err != nil {
		t.Fatalf("track task: %v", err)
	}

	tr := &TaskRunner{
		runnerID: runnerID,
		config: RunnerConfig{
			BrainAPIURL: apiSrv.URL,
			StateDir:    t.TempDir(),
			APITimeout:  5000,
		},
		processMgr: pm,
		logger:     log.New(os.Stderr, "", 0),
	}
	return hub, rt, tr, runnerID, instanceID
}

func waitFor(t *testing.T, timeout time.Duration, cond func() bool, msg string) {
	t.Helper()
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		if cond() {
			return
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatalf("timed out waiting for %s", msg)
}

func TestBridge_EndToEnd(t *testing.T) {
	_, opencodePort := fakeOpencode(t)
	hub, rt, tr, runnerID, instanceID := newBridgeTestEnv(t, opencodePort)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	bc := NewBridgeClient(tr)
	tr.bridgeClient = bc
	go bc.Start(ctx)

	waitFor(t, 5*time.Second, func() bool { return hub.Connected(runnerID) }, "bridge connection")

	// Proxied GET round-trip.
	status, body, err := hub.Do(ctx, runnerID, instanceID, "GET", "/session", nil)
	if err != nil {
		t.Fatalf("Do(GET /session) failed: %v", err)
	}
	if status != http.StatusOK || !strings.Contains(string(body), "ses_test") {
		t.Errorf("unexpected response: %d %s", status, body)
	}

	// Proxied POST round-trip (204, empty body).
	status, _, err = hub.Do(ctx, runnerID, instanceID, "POST", "/session/ses_test/prompt_async",
		[]byte(`{"parts":[{"type":"text","text":"hello"}]}`))
	if err != nil {
		t.Fatalf("Do(POST prompt_async) failed: %v", err)
	}
	if status != http.StatusNoContent {
		t.Errorf("expected 204, got %d", status)
	}

	// Allowlist enforced API-side.
	if _, _, err := hub.Do(ctx, runnerID, instanceID, "GET", "/file/content", nil); err != bridge.ErrPathNotAllowed {
		t.Errorf("expected ErrPathNotAllowed, got %v", err)
	}

	// Unknown instance surfaces a runner-side error.
	if _, _, err := hub.Do(ctx, runnerID, "inst_nope", "GET", "/session", nil); err == nil {
		t.Error("expected error for unknown instance")
	}

	// Event streaming: subscribe to the realtime topic, open the stream, and
	// expect both the control event and (once the stream is open) the full
	// firehose event to fan out.
	ch, unsub := rt.Subscribe(realtime.InstanceTopic(runnerID, instanceID))
	defer unsub()

	release, err := hub.AcquireStream(runnerID, instanceID)
	if err != nil {
		t.Fatalf("AcquireStream failed: %v", err)
	}
	defer release()

	gotControl, gotFull := false, false
	deadline := time.After(5 * time.Second)
	for !(gotControl && gotFull) {
		select {
		case msg := <-ch:
			raw, _ := msg.Data.(json.RawMessage)
			s := string(raw)
			if strings.Contains(s, "permission.updated") {
				gotControl = true
			}
			if strings.Contains(s, "message.part.updated") {
				gotFull = true
			}
		case <-deadline:
			t.Fatalf("timed out waiting for events (control=%v full=%v)", gotControl, gotFull)
		}
	}

	// The control event populated the pending-permission cache.
	waitFor(t, 2*time.Second, func() bool {
		return len(hub.PendingPermissions(runnerID, instanceID)) == 1
	}, "pending permission cache")

	// DecorateInstances merges live state.
	insts := []types.OpencodeInstance{{InstanceID: instanceID, RunnerID: runnerID, Status: "busy"}}
	hub.DecorateInstances(insts)
	if !insts[0].BridgeConnected {
		t.Error("expected BridgeConnected after decoration")
	}
	if insts[0].PendingPermissions != 1 {
		t.Errorf("expected 1 pending permission, got %d", insts[0].PendingPermissions)
	}
}

func TestBridgeClientAbortTask_KillsProcessAndResetsPending(t *testing.T) {
	client := newMockClient()
	processMgr := newMockProcessMgr()
	tr := NewTaskRunner(TaskRunnerOptions{
		Projects:   []string{"proj-a"},
		Config:     testRunnerConfig(),
		Mode:       ExecutionModeHeadless,
		Client:     client,
		Executors:  map[string]TaskExecutor{"opencode": newMockExecutor()},
		ProcessMgr: processMgr,
		StateMgr:   newMockStateMgr(),
	})
	bc := NewBridgeClient(tr)
	bc.ctx = context.Background()

	proc := newMockProcess(100)
	task := testRunningTask("task1")
	task.InstanceID = "inst-task1"
	if err := processMgr.Add("task1", task, proc); err != nil {
		t.Fatalf("add running task: %v", err)
	}
	if err := bc.abortTask("task1"); err != nil {
		t.Fatalf("abortTask returned error: %v", err)
	}

	processMgr.mu.Lock()
	killCalls := append([]string(nil), processMgr.killCalls...)
	processMgr.mu.Unlock()
	if len(killCalls) != 1 || killCalls[0] != "task1" {
		t.Fatalf("kill calls = %v, want [task1]", killCalls)
	}
	if processMgr.Get("task1") != nil {
		t.Fatal("task should be removed from process manager")
	}
	updates := client.getUpdateStatusCalls()
	if len(updates) != 1 || updates[0].Status != "pending" {
		t.Fatalf("status updates = %+v, want one pending update", updates)
	}
}

func TestBridge_DoWithoutConnection(t *testing.T) {
	hub := bridge.NewHub(realtime.NewHub())
	_, _, err := hub.Do(context.Background(), "runner_offline", "inst_x", "GET", "/session", nil)
	if err != bridge.ErrRunnerNotConnected {
		t.Errorf("expected ErrRunnerNotConnected, got %v", err)
	}
}

func TestBridgeClient_ValidateSpawnWorkdir(t *testing.T) {
	root := t.TempDir()
	nested := root + "/proj"
	if err := os.Mkdir(nested, 0o755); err != nil {
		t.Fatal(err)
	}

	tr := &TaskRunner{
		config: RunnerConfig{
			Control: ControlConfig{AllowedWorkdirRoots: []string{root}},
		},
		logger: log.New(os.Stderr, "", 0),
	}
	bc := NewBridgeClient(tr)

	if err := bc.validateSpawnWorkdir(nested); err != nil {
		t.Errorf("expected nested dir under root to validate: %v", err)
	}
	if err := bc.validateSpawnWorkdir(root); err != nil {
		t.Errorf("expected root itself to validate: %v", err)
	}
	if err := bc.validateSpawnWorkdir("relative/path"); err == nil {
		t.Error("expected relative path to be rejected")
	}
	if err := bc.validateSpawnWorkdir("/etc"); err == nil {
		t.Error("expected path outside roots to be rejected")
	}
	if err := bc.validateSpawnWorkdir(root + "/missing"); err == nil {
		t.Error("expected missing dir to be rejected")
	}
	// Prefix trickery: /tmp/rootEvil must not match root /tmp/root.
	evil := root + "evil"
	if err := os.Mkdir(evil, 0o755); err == nil {
		if err := bc.validateSpawnWorkdir(evil); err == nil {
			t.Error("expected sibling dir sharing the root prefix to be rejected")
		}
	}
}
