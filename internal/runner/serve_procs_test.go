package runner

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"syscall"
	"testing"
	"time"
)

// =============================================================================
// opencode serve lifecycle
//
// Each attachable task spawns an `opencode serve` beside the `opencode run`
// driver the ProcessManager tracks. Until 2026-09-03 the server was torn down
// only on normal completion and by a goroutine tied to the driver's exit, so a
// runner shutdown or crash with work in flight orphaned one server per task.
// Five of them, up to 31 hours old, had a 36GB machine deep into swap.
// =============================================================================

func newServeTestExecutor(t *testing.T) *OpenCodeExecutor {
	t.Helper()
	cfg := testRunnerConfig()
	cfg.StateDir = t.TempDir()
	return NewExecutor(cfg)
}

func readServeRecord(t *testing.T, e *OpenCodeExecutor) map[string]serveProcRecord {
	t.Helper()
	b, err := os.ReadFile(e.serveProcsPath())
	if os.IsNotExist(err) {
		return nil
	}
	if err != nil {
		t.Fatalf("read serve record: %v", err)
	}
	var state serveProcsState
	if err := json.Unmarshal(b, &state); err != nil {
		t.Fatalf("decode serve record: %v", err)
	}
	return state.Procs
}

func TestKillAllServeProcs_KillsEveryTrackedServer(t *testing.T) {
	shrinkServeGraces(t)
	e := newServeTestExecutor(t)
	a := &politeProcess{pid: 1001}
	b := &politeProcess{pid: 1002}
	e.trackServeProcFor("task-a", "proj", a)
	e.trackServeProcFor("task-b", "proj", b)

	e.KillAllServeProcs()

	for _, p := range []*politeProcess{a, b} {
		if got := p.log.get(); len(got) != 1 || got[0] != syscall.SIGTERM {
			t.Fatalf("pid %d: signals=%v, want [SIGTERM] — shutdown must not leave a server behind", p.pid, got)
		}
	}

	e.serveMu.Lock()
	remaining := len(e.serveProcs)
	e.serveMu.Unlock()
	if remaining != 0 {
		t.Fatalf("%d serve procs still tracked after KillAllServeProcs", remaining)
	}
	if rec := readServeRecord(t, e); rec != nil {
		t.Fatalf("serve record still on disk after shutdown: %v", rec)
	}
}

func TestKillAllServeProcs_SkipsAlreadyExited(t *testing.T) {
	e := newServeTestExecutor(t)
	p := newMockProcess(1003)
	p.simulateExit(0)
	e.trackServeProcFor("task-a", "proj", p)

	e.KillAllServeProcs()

	p.mu.Lock()
	defer p.mu.Unlock()
	if p.killed {
		t.Fatal("signalled a process that had already exited")
	}
}

func TestServeProcs_RecordFollowsTrackAndKill(t *testing.T) {
	shrinkServeGraces(t)
	e := newServeTestExecutor(t)

	e.trackServeProcFor("task-a", "proj-x", newMockProcess(2001))
	rec := readServeRecord(t, e)
	got, ok := rec["task-a"]
	if !ok {
		t.Fatalf("record missing task-a after track: %v", rec)
	}
	if got.PID != 2001 || got.ProjectID != "proj-x" {
		t.Fatalf("record = %+v, want pid 2001 project proj-x", got)
	}

	// A second task lands beside the first, not over it.
	e.trackServeProcFor("task-b", "proj-x", newMockProcess(2002))
	if rec := readServeRecord(t, e); len(rec) != 2 {
		t.Fatalf("record has %d entries after second track, want 2: %v", len(rec), rec)
	}

	// Killing one leaves the other; killing the last removes the file, so a
	// successor never reads a record with nothing in it.
	e.killServeProc("task-a")
	if rec := readServeRecord(t, e); len(rec) != 1 || rec["task-b"].PID != 2002 {
		t.Fatalf("record after killing task-a = %v, want only task-b", rec)
	}
	e.killServeProc("task-b")
	if rec := readServeRecord(t, e); rec != nil {
		t.Fatalf("record still present with nothing tracked: %v", rec)
	}
}

func TestServeProcs_NoStateDirMeansNoRecord(t *testing.T) {
	cfg := testRunnerConfig()
	cfg.StateDir = ""
	e := NewExecutor(cfg)
	e.trackServeProcFor("task-a", "proj", newMockProcess(3001))
	// Nothing to assert on disk; the point is that it did not panic or write
	// to the working directory.
	if _, err := os.Stat("serve-procs.json"); err == nil {
		t.Fatal("wrote serve-procs.json into the working directory")
	}
}

// startSleeper spawns a real child to stand in for a leftover server, and
// makes sure it dies with the test.
func startSleeper(t *testing.T) *exec.Cmd {
	t.Helper()
	cmd := exec.Command("sleep", "60")
	if err := cmd.Start(); err != nil {
		t.Fatalf("start sleeper: %v", err)
	}
	t.Cleanup(func() {
		_ = cmd.Process.Kill()
		_ = cmd.Wait()
	})
	return cmd
}

// waitExit reports whether the child exits within d.
func waitExit(cmd *exec.Cmd, d time.Duration) bool {
	done := make(chan struct{})
	go func() { _ = cmd.Wait(); close(done) }()
	select {
	case <-done:
		return true
	case <-time.After(d):
		return false
	}
}

func writeServeRecord(t *testing.T, e *OpenCodeExecutor, procs map[string]serveProcRecord) {
	t.Helper()
	b, err := json.MarshalIndent(serveProcsState{Procs: procs}, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(e.serveProcsPath(), b, 0o644); err != nil {
		t.Fatal(err)
	}
}

// psAnswering makes the executor's `ps` lookups return the given command line.
func psAnswering(e *OpenCodeExecutor, line string) {
	real := e.CommandFactory
	e.CommandFactory = func(name string, args ...string) *exec.Cmd {
		if name == "ps" {
			return exec.Command("echo", line)
		}
		return real(name, args...)
	}
}

func TestReapLeftoverServeProcs_KillsLiveServerFromPreviousRunner(t *testing.T) {
	e := newServeTestExecutor(t)
	child := startSleeper(t)
	writeServeRecord(t, e, map[string]serveProcRecord{
		"task-a": {PID: child.Process.Pid, ProjectID: "proj", StartedAt: "2026-09-02T17:41:38Z"},
	})
	psAnswering(e, "opencode serve --port 0")

	e.ReapLeftoverServeProcs()

	if !waitExit(child, 5*time.Second) {
		t.Fatal("leftover server was not killed at startup")
	}
	if _, err := os.Stat(e.serveProcsPath()); !os.IsNotExist(err) {
		t.Fatalf("serve record not discarded after reap (err=%v)", err)
	}
}

func TestReapLeftoverServeProcs_SkipsReusedPid(t *testing.T) {
	e := newServeTestExecutor(t)
	child := startSleeper(t)
	writeServeRecord(t, e, map[string]serveProcRecord{
		"task-a": {PID: child.Process.Pid, ProjectID: "proj"},
	})
	// The PID is alive, but it is no longer an opencode serve: whatever
	// inherited the number must not be signalled.
	psAnswering(e, "python3 -m http.server 8000")

	e.ReapLeftoverServeProcs()

	if waitExit(child, 500*time.Millisecond) {
		t.Fatal("killed an unrelated process that reused a recorded pid")
	}
	// The stale record is still discarded — it describes a runner that is gone.
	if _, err := os.Stat(e.serveProcsPath()); !os.IsNotExist(err) {
		t.Fatalf("stale serve record kept (err=%v)", err)
	}
}

func TestReapLeftoverServeProcs_ToleratesDeadPidAndNoRecord(t *testing.T) {
	e := newServeTestExecutor(t)

	// No record at all: a no-op.
	e.ReapLeftoverServeProcs()

	// A record naming a pid that is long gone: a no-op that still clears.
	writeServeRecord(t, e, map[string]serveProcRecord{
		"task-a": {PID: 2147483000, ProjectID: "proj"},
	})
	e.ReapLeftoverServeProcs()
	if _, err := os.Stat(e.serveProcsPath()); !os.IsNotExist(err) {
		t.Fatalf("record with only dead pids not discarded (err=%v)", err)
	}
}

func TestReapLeftoverServeProcs_UnreadableRecordIsNotActedOn(t *testing.T) {
	e := newServeTestExecutor(t)
	if err := os.WriteFile(e.serveProcsPath(), []byte("{not json"), 0o644); err != nil {
		t.Fatal(err)
	}
	e.ReapLeftoverServeProcs() // must not panic
	if _, err := os.Stat(e.serveProcsPath()); !os.IsNotExist(err) {
		t.Fatalf("unreadable record kept (err=%v)", err)
	}
}

// =============================================================================
// Runner shutdown reaches the executor's servers
// =============================================================================

func TestStop_KillsServeProcsOfRunningTasks(t *testing.T) {
	shrinkServeGraces(t)
	cfg := testRunnerConfig()
	cfg.StateDir = t.TempDir()
	exec := NewExecutor(cfg)
	serve := &politeProcess{pid: 4001}
	exec.trackServeProcFor("task-a", "proj-a", serve)

	client := newMockClient()
	tr := NewTaskRunner(TaskRunnerOptions{
		Projects:   []string{"proj-a"},
		Config:     cfg,
		Mode:       ExecutionModeHeadless,
		Client:     client,
		Executors:  map[string]TaskExecutor{"opencode": exec},
		ProcessMgr: newMockProcessMgr(),
		StateMgr:   newMockStateMgr(),
	})

	ctx, cancel := context.WithCancel(context.Background())
	go func() { tr.Start(ctx) }()
	time.Sleep(100 * time.Millisecond)
	cancel()
	if err := tr.Stop(); err != nil {
		t.Fatalf("Stop returned error: %v", err)
	}

	if got := serve.log.get(); len(got) == 0 || got[0] != syscall.SIGTERM {
		t.Fatalf("serve signals=%v after Stop, want SIGTERM first: KillAll only covers the run driver", got)
	}
	if _, err := os.Stat(filepath.Join(cfg.StateDir, serveProcsFileName)); !os.IsNotExist(err) {
		t.Fatalf("serve record left behind after a clean Stop (err=%v)", err)
	}
}

// =============================================================================
// Escalation: SIGTERM, then SIGKILL for a server that ignores it
// =============================================================================

// signalLog records every signal a process receives, in order.
type signalLog struct {
	mu   sync.Mutex
	sigs []os.Signal
}

func (l *signalLog) add(s os.Signal) { l.mu.Lock(); l.sigs = append(l.sigs, s); l.mu.Unlock() }
func (l *signalLog) get() []os.Signal {
	l.mu.Lock()
	defer l.mu.Unlock()
	return append([]os.Signal(nil), l.sigs...)
}

// stubbornProcess ignores SIGTERM and only exits on SIGKILL — a server
// mid-request or hung in a handler.
type stubbornProcess struct {
	pid    int
	log    signalLog
	mu     sync.Mutex
	exited bool
}

func (p *stubbornProcess) Pid() int { return p.pid }
func (p *stubbornProcess) Exited() bool {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.exited
}
func (p *stubbornProcess) ExitCode() int { return -1 }
func (p *stubbornProcess) Kill(sig os.Signal) error {
	p.log.add(sig)
	if sig == syscall.SIGKILL {
		p.mu.Lock()
		p.exited = true
		p.mu.Unlock()
	}
	return nil
}

// politeProcess exits on the first SIGTERM.
type politeProcess struct {
	pid    int
	log    signalLog
	mu     sync.Mutex
	exited bool
}

func (p *politeProcess) Pid() int { return p.pid }
func (p *politeProcess) Exited() bool {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.exited
}
func (p *politeProcess) ExitCode() int { return 0 }
func (p *politeProcess) Kill(sig os.Signal) error {
	p.log.add(sig)
	p.mu.Lock()
	p.exited = true
	p.mu.Unlock()
	return nil
}

// shrinkServeGraces makes escalation fast for tests and restores it after.
func shrinkServeGraces(t *testing.T) {
	t.Helper()
	term, kill := serveTermGrace, serveKillGrace
	serveTermGrace, serveKillGrace = 150*time.Millisecond, 100*time.Millisecond
	t.Cleanup(func() { serveTermGrace, serveKillGrace = term, kill })
}

func TestTerminateServe_EscalatesToSIGKILL(t *testing.T) {
	shrinkServeGraces(t)
	p := &stubbornProcess{pid: 5001}

	terminateServe("task-a", p)

	got := p.log.get()
	if len(got) != 2 || got[0] != syscall.SIGTERM || got[1] != syscall.SIGKILL {
		t.Fatalf("signals = %v, want [SIGTERM SIGKILL]: a single SIGTERM is how servers survived teardown", got)
	}
	if !p.Exited() {
		t.Fatal("server still alive after escalation")
	}
}

func TestTerminateServe_StopsAtSIGTERMWhenHonoured(t *testing.T) {
	shrinkServeGraces(t)
	p := &politeProcess{pid: 5002}

	terminateServe("task-a", p)

	if got := p.log.get(); len(got) != 1 || got[0] != syscall.SIGTERM {
		t.Fatalf("signals = %v, want [SIGTERM] only: no SIGKILL for a server that exits politely", got)
	}
}

func TestKillAllServeProcs_ConcurrentTeardownIsBounded(t *testing.T) {
	shrinkServeGraces(t)
	e := newServeTestExecutor(t)
	procs := []*stubbornProcess{{pid: 5101}, {pid: 5102}, {pid: 5103}, {pid: 5104}}
	for i, p := range procs {
		e.trackServeProcFor("task-"+string(rune('a'+i)), "proj", p)
	}

	start := time.Now()
	e.KillAllServeProcs()
	elapsed := time.Since(start)

	for _, p := range procs {
		if !p.Exited() {
			t.Fatalf("pid %d survived shutdown", p.pid)
		}
	}
	// Serial escalation would be 4 × (term + kill) ≈ 1s; concurrent is ≈ one
	// escalation. Allow generous slack for a loaded CI box.
	oneEscalation := serveTermGrace + serveKillGrace
	if elapsed > 3*oneEscalation {
		t.Fatalf("KillAllServeProcs took %v for 4 servers; want ≈ one escalation (%v), teardown is serial", elapsed, oneEscalation)
	}
}

// =============================================================================
// One OpenCodeExecutor owns the servers, whatever name it is registered under
// =============================================================================

func TestNewExecutorRegistry_ScriptSharesOpencodeInstance(t *testing.T) {
	cfg := testExecutorConfig()
	cfg.Script.Enabled = true
	reg := NewExecutorRegistry(cfg)

	oc, _ := reg.Get("opencode")
	sc, _ := reg.Get("script")
	if oc == nil || sc == nil {
		t.Fatal("both executors should be registered")
	}
	if oc != sc {
		t.Fatal("script and opencode must be the SAME OpenCodeExecutor: two instances would each own half the serve processes and overwrite each other's on-disk record")
	}
}

// =============================================================================
// Every path that kills a driver also reaps its server
// =============================================================================

// serveOwningRunner builds a runner whose "opencode" executor is real (so it
// owns a serve process for task1) while everything else is mocked.
func serveOwningRunner(t *testing.T, client *mockClient, processMgr *mockProcessMgr) (*TaskRunner, *mockProcess) {
	t.Helper()
	cfg := testRunnerConfig()
	cfg.StateDir = t.TempDir()
	exec := NewExecutor(cfg)
	serve := newMockProcess(6001)
	exec.trackServeProcFor("task1", "proj-a", serve)

	tr := NewTaskRunner(TaskRunnerOptions{
		Projects:   []string{"proj-a"},
		Config:     cfg,
		Mode:       ExecutionModeHeadless,
		Client:     client,
		Executors:  map[string]TaskExecutor{"opencode": exec},
		ProcessMgr: processMgr,
		StateMgr:   newMockStateMgr(),
	})
	return tr, serve
}

// awaitKilled waits for the async killServeProc goroutine to signal the mock.
func awaitKilled(t *testing.T, p *mockProcess, what string) {
	t.Helper()
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		p.mu.Lock()
		killed := p.killed
		p.mu.Unlock()
		if killed {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("%s: serve process was never signalled — this path kills the driver and leaks the server", what)
}

func TestAbortTask_ReapsServeProcess(t *testing.T) {
	shrinkServeGraces(t)
	client := newMockClient()
	processMgr := newMockProcessMgr()
	tr, serve := serveOwningRunner(t, client, processMgr)

	bc := NewBridgeClient(tr)
	bc.ctx = context.Background()

	task := testRunningTask("task1")
	task.ExecutorType = "opencode"
	task.InstanceID = "inst-task1"
	if err := processMgr.Add("task1", task, newMockProcess(100)); err != nil {
		t.Fatalf("add running task: %v", err)
	}

	if err := bc.abortTask("task1"); err != nil {
		t.Fatalf("abortTask returned error: %v", err)
	}
	awaitKilled(t, serve, "abortTask")
}

func TestRenewClaimsFailure_ReapsServeProcess(t *testing.T) {
	shrinkServeGraces(t)
	client := newMockClient()
	processMgr := newMockProcessMgr()
	tr, serve := serveOwningRunner(t, client, processMgr)

	task := testRunningTask("task1")
	task.ExecutorType = "opencode"
	if err := processMgr.Add("task1", task, newMockProcess(100)); err != nil {
		t.Fatalf("add running task: %v", err)
	}
	client.renewErr = fmt.Errorf("claim not found")

	tr.renewClaims(context.Background())
	awaitKilled(t, serve, "renewClaims failure")
}

func TestClaimAndSpawn_AddFailureKillsDriverAndCleansUp(t *testing.T) {
	client := newMockClient()
	executor := newMockExecutor()
	processMgr := newMockProcessMgr()
	tr := newTestRunner(client, executor, processMgr, newMockStateMgr())

	driver := newMockProcess(7001)
	executor.spawnResult = &SpawnResult{PID: 7001, Proc: driver}

	// Make Add fail: the task is already tracked with a live process, which
	// is the duplicate-dispatch condition ProcessManager rejects.
	already := testRunningTask("task1")
	if err := processMgr.Add("task1", already, newMockProcess(1)); err != nil {
		t.Fatal(err)
	}

	err := tr.claimAndSpawn(context.Background(), testTask("task1", "proj-a"), "proj-a")
	if err == nil || !strings.Contains(err.Error(), "track process") {
		t.Fatalf("err = %v, want the track-process failure", err)
	}

	driver.mu.Lock()
	killed := driver.killed
	driver.mu.Unlock()
	if !killed {
		t.Fatal("driver left running after Add failed: nothing tracked it, so nothing would ever kill it")
	}
	found := false
	for _, c := range executor.getCleanupCalls() {
		if c.TaskID == "task1" {
			found = true
		}
	}
	if !found {
		t.Fatalf("executor Cleanup not called after Add failed; calls = %v", executor.getCleanupCalls())
	}
}
