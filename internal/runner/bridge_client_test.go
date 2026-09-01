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
	"syscall"
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
	tr.setBridgeClient(bc)
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

// execOutcome is everything one runner-shell command fanned out to the
// realtime topic the control API streams from.
type execOutcome struct {
	stdout   string
	stderr   string
	exitCode int
	exitErr  string
}

// collectExec drains an exec topic until the terminating exec_exit arrives.
func collectExec(t *testing.T, ch <-chan realtime.SSEMessage, execID string, timeout time.Duration) execOutcome {
	t.Helper()
	var out execOutcome
	deadline := time.After(timeout)
	for {
		select {
		case msg := <-ch:
			data, ok := msg.Data.(map[string]any)
			if !ok {
				t.Fatalf("exec payload is %T, want map[string]any", msg.Data)
			}
			if id, _ := data["exec_id"].(string); id != execID {
				t.Fatalf("event for exec %q, want %q", id, execID)
			}
			switch msg.Event {
			case "exec_data":
				chunk, _ := data["chunk"].(string)
				if stream, _ := data["stream"].(string); stream == bridge.ExecStreamStderr {
					out.stderr += chunk
				} else {
					out.stdout += chunk
				}
			case "exec_exit":
				out.exitCode, _ = data["exit_code"].(int)
				out.exitErr, _ = data["error"].(string)
				return out
			}
		case <-deadline:
			t.Fatalf("timed out waiting for exec_exit (stdout=%q stderr=%q)", out.stdout, out.stderr)
		}
	}
}

// TestBridgeExec_EndToEnd drives the runner shell through a real bridge Hub
// and a real BridgeClient: output must stream back on the exec topic and
// every command must terminate with exactly one exec_exit.
func TestBridgeExec_EndToEnd(t *testing.T) {
	_, opencodePort := fakeOpencode(t)
	hub, rt, tr, runnerID, _ := newBridgeTestEnv(t, opencodePort)

	// Default workdir comes from runner config when the frame omits one.
	workdir := t.TempDir()
	tr.config.WorkDir = workdir
	if err := os.WriteFile(workdir+"/marker.txt", []byte("in-workdir"), 0o644); err != nil {
		t.Fatal(err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// Subscribe to every exec topic up front and hold the subscriptions for
	// the whole test: realtime.Hub.publish ranges over a topic's subscriber
	// set after dropping its lock, so unsubscribing while frames are still
	// in flight trips a pre-existing race in that package.
	subs := make(map[string]<-chan realtime.SSEMessage)
	for _, execID := range []string{
		"exec-ok", "exec-dir", "exec-fail", "exec-utf8", "exec-sig", "exec-baddir",
	} {
		ch, unsub := rt.Subscribe(realtime.ExecTopic(runnerID, execID))
		defer unsub()
		subs[execID] = ch
	}

	bc := NewBridgeClient(tr)
	tr.setBridgeClient(bc)
	go bc.Start(ctx)
	waitFor(t, 5*time.Second, func() bool { return hub.Connected(runnerID) }, "bridge connection")

	t.Run("stdout stderr and exit zero", func(t *testing.T) {
		ch := subs["exec-ok"]

		if err := hub.StartExec(ctx, runnerID, "exec-ok",
			`cat marker.txt; printf 'oops' >&2`, "", 0); err != nil {
			t.Fatalf("StartExec: %v", err)
		}
		out := collectExec(t, ch, "exec-ok", 10*time.Second)
		if !strings.Contains(out.stdout, "in-workdir") {
			t.Errorf("stdout = %q, want it to contain the workdir marker", out.stdout)
		}
		if !strings.Contains(out.stderr, "oops") {
			t.Errorf("stderr = %q, want it to contain %q", out.stderr, "oops")
		}
		if out.exitCode != 0 || out.exitErr != "" {
			t.Errorf("exit = %d (%q), want 0", out.exitCode, out.exitErr)
		}
	})

	t.Run("explicit workdir", func(t *testing.T) {
		other := t.TempDir()
		if err := os.WriteFile(other+"/other.txt", []byte("elsewhere"), 0o644); err != nil {
			t.Fatal(err)
		}
		ch := subs["exec-dir"]

		if err := hub.StartExec(ctx, runnerID, "exec-dir", "cat other.txt", other, 0); err != nil {
			t.Fatalf("StartExec: %v", err)
		}
		out := collectExec(t, ch, "exec-dir", 10*time.Second)
		if !strings.Contains(out.stdout, "elsewhere") {
			t.Errorf("stdout = %q, want it to contain %q", out.stdout, "elsewhere")
		}
	})

	t.Run("non-zero exit code", func(t *testing.T) {
		ch := subs["exec-fail"]

		if err := hub.StartExec(ctx, runnerID, "exec-fail", "exit 7", "", 0); err != nil {
			t.Fatalf("StartExec: %v", err)
		}
		out := collectExec(t, ch, "exec-fail", 10*time.Second)
		if out.exitCode != 7 {
			t.Errorf("exit code = %d, want 7", out.exitCode)
		}
	})

	t.Run("chunked output stays valid utf8", func(t *testing.T) {
		ch := subs["exec-utf8"]

		// 40 KB of two-byte runes: more than one ExecChunkBytes read, so a
		// rune is guaranteed to straddle a chunk boundary.
		const runes = 20000
		if err := hub.StartExec(ctx, runnerID, "exec-utf8",
			`printf 'é%.0s' $(seq 1 20000)`, "", 0); err != nil {
			t.Fatalf("StartExec: %v", err)
		}
		out := collectExec(t, ch, "exec-utf8", 20*time.Second)
		if out.exitCode != 0 {
			t.Fatalf("exit = %d (%q), want 0", out.exitCode, out.exitErr)
		}
		if got := strings.Count(out.stdout, "é"); got != runes {
			t.Errorf("got %d é runes, want %d", got, runes)
		}
		if strings.Contains(out.stdout, "�") {
			t.Error("output contains a replacement char: a rune was split across chunks")
		}
	})

	t.Run("signal kills the process group", func(t *testing.T) {
		ch := subs["exec-sig"]

		// A child sleep in the same group: signalling must reach it too, or
		// the pipes stay open and exec_exit never lands.
		if err := hub.StartExec(ctx, runnerID, "exec-sig",
			`printf 'up'; sleep 120 & wait`, "", 0); err != nil {
			t.Fatalf("StartExec: %v", err)
		}
		waitFor(t, 5*time.Second, func() bool {
			bc.execMu.Lock()
			defer bc.execMu.Unlock()
			return bc.execs["exec-sig"] != nil
		}, "exec registration")

		// A duplicate exec id must be refused while the first is running.
		if err := hub.StartExec(ctx, runnerID, "exec-sig", "echo dup", "", 0); err == nil {
			t.Error("expected duplicate exec id to be rejected")
		}

		if err := hub.SignalExec(ctx, runnerID, "exec-sig", "kill"); err != nil {
			t.Fatalf("SignalExec: %v", err)
		}
		out := collectExec(t, ch, "exec-sig", 10*time.Second)
		if out.exitCode == 0 {
			t.Errorf("exit code = 0, want non-zero after a kill (err=%q)", out.exitErr)
		}
		if out.exitErr == "" {
			t.Error("expected an error reason on a signalled exit")
		}
		// The registry entry is dropped only when the process actually exits.
		waitFor(t, 5*time.Second, func() bool {
			bc.execMu.Lock()
			defer bc.execMu.Unlock()
			return bc.execs["exec-sig"] == nil
		}, "exec registry cleanup")
	})

	t.Run("bad workdir fails before the stream opens", func(t *testing.T) {
		ch := subs["exec-baddir"]

		err := hub.StartExec(ctx, runnerID, "exec-baddir", "echo hi",
			workdir+"/definitely-missing", 0)
		if err == nil {
			t.Fatal("expected a missing workdir to be rejected")
		}
		// No exec_exit may follow a pre-spawn failure.
		select {
		case msg := <-ch:
			t.Fatalf("unexpected event %q after pre-spawn failure", msg.Event)
		case <-time.After(300 * time.Millisecond):
		}
	})

	t.Run("signal for unknown exec errors", func(t *testing.T) {
		if err := hub.SignalExec(ctx, runnerID, "exec-nope", "int"); err == nil {
			t.Error("expected an error signalling an unknown exec")
		}
	})
}

func TestExecHelpers(t *testing.T) {
	if got := execTimeout(0); got != time.Duration(bridge.ExecDefaultTimeoutMs)*time.Millisecond {
		t.Errorf("execTimeout(0) = %v, want the default", got)
	}
	if got := execTimeout(bridge.ExecMaxTimeoutMs * 10); got != time.Duration(bridge.ExecMaxTimeoutMs)*time.Millisecond {
		t.Errorf("execTimeout(huge) = %v, want the ceiling", got)
	}
	if got := execSignal("term"); got != syscall.SIGTERM {
		t.Errorf(`execSignal("term") = %v, want SIGTERM`, got)
	}
	if got := execSignal("bogus"); got != syscall.SIGINT {
		t.Errorf(`execSignal("bogus") = %v, want SIGINT`, got)
	}

	// A two-byte rune split across a read boundary must be held back.
	full := []byte("aé")
	if got := trailingPartialRune(full); got != len(full) {
		t.Errorf("trailingPartialRune(complete) = %d, want %d", got, len(full))
	}
	partial := full[:len(full)-1]
	if got := trailingPartialRune(partial); got != len(partial)-1 {
		t.Errorf("trailingPartialRune(partial) = %d, want %d", got, len(partial)-1)
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
