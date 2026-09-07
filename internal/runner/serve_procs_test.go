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
	e := NewExecutor(cfg)
	e.serveTermGrace, e.serveKillGrace = 150*time.Millisecond, 100*time.Millisecond
	t.Cleanup(e.KillAllServeProcs)
	return e
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

// blockedServe makes the pending teardown observable without sleep-based ordering.
type blockedServe struct {
	stubbornProcess
	term        chan struct{}
	release     chan struct{}
	once        sync.Once
	releaseOnce sync.Once
}

func (p *blockedServe) unblock() { p.releaseOnce.Do(func() { close(p.release) }) }

func (p *blockedServe) Kill(sig os.Signal) error {
	if sig == syscall.SIGTERM {
		p.once.Do(func() { close(p.term) })
		<-p.release
	}
	return p.stubbornProcess.Kill(sig)
}

func TestKillAllServeProcs_DrainsPendingTeardown(t *testing.T) {
	e := newServeTestExecutor(t)
	p := &blockedServe{stubbornProcess: stubbornProcess{pid: 8101}, term: make(chan struct{}), release: make(chan struct{})}
	t.Cleanup(func() { p.unblock(); e.KillAllServeProcs() })
	e.trackServeProcFor("task", "proj", p)
	teardown := e.killServeProc("task")
	<-p.term // cleanup owns a worker, stopped inside TERM
	if rec := readServeRecord(t, e); rec["task"].PID != p.Pid() {
		t.Error("pending teardown lost its recovery PID record")
	}
	done := make(chan struct{})
	go func() { e.KillAllServeProcs(); close(done) }()
	select {
	case <-done:
		t.Error("shutdown returned before pending teardown completed")
	case <-time.After(100 * time.Millisecond):
	}
	p.unblock()
	<-done
	<-teardown // join even if shutdown regresses
	if !p.Exited() {
		t.Fatal("server survived escalation")
	}
	if rec := readServeRecord(t, e); len(rec) != 0 {
		t.Fatalf("confirmed exit left recovery records: %v", rec)
	}
}

func TestServeProcs_ReplacementRetainsOldGeneration(t *testing.T) {
	e := newServeTestExecutor(t)
	old := &blockedServe{stubbornProcess: stubbornProcess{pid: 8102}, term: make(chan struct{}), release: make(chan struct{})}
	t.Cleanup(func() { old.unblock(); e.KillAllServeProcs() })
	next := &politeProcess{pid: 8103}
	e.trackServeProcFor("task", "proj", old)
	e.trackServeProcFor("task", "proj", next)
	<-old.term
	rec := readServeRecord(t, e)
	if len(rec) != 2 || rec["task"].PID != next.Pid() || rec["task#8102"].PID != old.Pid() {
		t.Errorf("replacement lost overlapping recovery records: %v", rec)
	}
	old.unblock()
	e.KillAllServeProcs()
	if !old.Exited() || !next.Exited() {
		t.Fatal("replacement discarded an owned server generation")
	}
}

// An unkillable process models signal errors or an unconfirmed kernel exit.
type unkillableServe struct{ politeProcess }

func (p *unkillableServe) Kill(sig os.Signal) error { p.log.add(sig); return syscall.EPERM }

func TestKillAllServeProcs_RetainsUnconfirmedExitForRetry(t *testing.T) {
	e := newServeTestExecutor(t)
	p := &unkillableServe{politeProcess: politeProcess{pid: 8104}}
	e.trackServeProcFor("task", "proj", p)
	e.KillAllServeProcs()
	if rec := readServeRecord(t, e); rec["task"].PID != p.Pid() {
		t.Error("unconfirmed exit lost its recoverable PID")
	}
	p.mu.Lock()
	p.exited = true
	p.mu.Unlock()
	e.KillAllServeProcs()
	if rec := readServeRecord(t, e); len(rec) != 0 {
		t.Fatalf("confirmed exit not cleared on retry: %v", rec)
	}
}

func TestServeProcs_ShutdownRejectsHeadlessSpawn(t *testing.T) {
	e := newServeTestExecutor(t)
	e.KillAllServeProcs()
	calls := 0
	e.CommandFactory = func(name string, args ...string) *exec.Cmd {
		calls++
		return exec.Command("false")
	}
	_, err := e.spawnHeadless(context.Background(), testTask("task", "proj"), "proj", t.TempDir(), "missing", SpawnOptions{})
	if err == nil || !strings.Contains(err.Error(), "shut") || calls != 0 {
		t.Fatalf("spawn after shutdown: err=%v commands=%d; want admission rejection before commands", err, calls)
	}
}

func TestServeProcs_LateRegistrationIsDrained(t *testing.T) {
	e := newServeTestExecutor(t)
	e.KillAllServeProcs()
	p := &politeProcess{pid: 8105}
	e.trackServeProcFor("late", "proj", p)
	if !p.Exited() || len(readServeRecord(t, e)) != 0 {
		t.Fatal("late registration escaped shutdown")
	}
}

func TestServeProcs_OldWatcherCannotKillReplacement(t *testing.T) {
	e := newServeTestExecutor(t)
	old := &politeProcess{pid: 8110}
	next := &politeProcess{pid: 8111}
	e.trackServeProcFor("task", "proj", old)
	<-e.killServeProc("task")
	e.trackServeProcFor("task", "proj", next)
	t.Cleanup(e.KillAllServeProcs)
	e.killServeGeneration("task", old)
	if e.ServePID("task") != next.Pid() || len(next.log.get()) != 0 {
		t.Fatal("old driver watcher touched the replacement generation")
	}
	if rec := readServeRecord(t, e); rec["task"].PID != next.Pid() {
		t.Fatalf("old watcher removed replacement record: %v", rec)
	}
}

func TestServeProcs_ConcurrentRegistrationAndShutdown(t *testing.T) {
	e := newServeTestExecutor(t)
	const count = 20
	var wg sync.WaitGroup
	start := make(chan struct{})
	procs := make([]*politeProcess, count)
	for i := range procs {
		procs[i] = &politeProcess{pid: 8200 + i}
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			<-start
			e.trackServeProcFor(fmt.Sprintf("task-%d", i), "proj", procs[i])
		}(i)
	}
	wg.Add(1)
	go func() { defer wg.Done(); <-start; e.KillAllServeProcs() }()
	close(start)
	wg.Wait()
	for _, p := range procs {
		if !p.Exited() {
			t.Errorf("registration escaped drain: pid=%d", p.Pid())
		}
	}
	if rec := readServeRecord(t, e); len(rec) != 0 {
		t.Fatalf("stale snapshot overwrote final durable state: %v", rec)
	}
}

func TestKillAllServeProcs_WaitsForAdmittedHeadlessSpawn(t *testing.T) {
	e := newServeTestExecutor(t)
	e.config.Control.Disabled = true
	prompt := filepath.Join(t.TempDir(), "prompt")
	if err := os.WriteFile(prompt, []byte("test"), 0o600); err != nil {
		t.Fatal(err)
	}
	entered, release := make(chan struct{}), make(chan struct{})
	p := &politeProcess{pid: 8300}
	e.CommandFactory = func(string, ...string) *exec.Cmd {
		close(entered)
		<-release
		// Model registration after shutdown has been requested but while the
		// admitted spawn is still in flight.
		e.trackServeProcFor("task", "proj", p)
		return exec.Command("true")
	}
	spawned := make(chan error, 1)
	go func() {
		res, err := e.spawnHeadless(context.Background(), testTask("task", "proj"), "proj", t.TempDir(), prompt, SpawnOptions{})
		if err == nil {
			<-res.Proc.(*OsProcess).Done()
		}
		spawned <- err
	}()
	<-entered
	if e.serveAdmission.TryLock() {
		e.serveAdmission.Unlock()
		t.Error("headless spawn did not retain admission through process creation")
	}
	stopped := make(chan struct{})
	go func() { e.KillAllServeProcs(); close(stopped) }()
	close(release)
	if err := <-spawned; err != nil {
		t.Errorf("spawn: %v", err)
	}
	<-stopped
	if !p.Exited() || len(readServeRecord(t, e)) != 0 {
		t.Fatal("shutdown missed registration from an admitted spawn")
	}
}

func TestReapLeftoverServeProcs_RevalidatesEverySignal(t *testing.T) {
	child, done := startSleeper(t)
	checks := 0
	p := &recoveredServeProcess{PidProcess: NewPidProcess(child.Process.Pid), matches: func(int) bool {
		checks++
		return false
	}}
	for _, sig := range []os.Signal{syscall.SIGTERM, syscall.SIGKILL} {
		if err := p.Kill(sig); err == nil {
			t.Errorf("sent %v to a mismatched PID", sig)
		}
	}
	if checks != 2 {
		t.Fatalf("identity checks=%d, want one per signal", checks)
	}
	select {
	case <-done:
		t.Fatal("signalled mismatched recovered PID")
	default:
	}
}

func TestReapLeftoverServeProcs_RetainsUnconfirmedPID(t *testing.T) {
	e := newServeTestExecutor(t)
	// This fixture deliberately retains a zombie until recovery has inspected
	// it. Unlike startSleeper, its one Wait owner runs only when requested.
	child := exec.Command("sleep", "60")
	if err := child.Start(); err != nil {
		t.Fatal(err)
	}
	var waitOnce sync.Once
	reap := func() { waitOnce.Do(func() { _ = child.Wait() }) }
	t.Cleanup(func() { _ = child.Process.Kill(); reap() })
	writeServeRecord(t, e, map[string]serveProcRecord{
		"task": {PID: child.Process.Pid, ProjectID: "proj", StartedAt: "original"},
	})
	psAnswering(e, "opencode serve --port 0")
	// Do not Wait yet: kill(0) still sees the unreaped child. Recovery must
	// preserve ownership rather than assume successful signalling means exit.
	e.ReapLeftoverServeProcs()
	if rec := readServeRecord(t, e); rec["task"].PID != child.Process.Pid || rec["task"].StartedAt != "original" {
		t.Errorf("unconfirmed recovery discarded PID identity: %v", rec)
	}
	reap()
	e.ReapLeftoverServeProcs()
	if rec := readServeRecord(t, e); len(rec) != 0 {
		t.Fatalf("confirmed recovered exit left record: %v", rec)
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
	e := newServeTestExecutor(t)

	e.trackServeProcFor("task-a", "proj-x", &politeProcess{pid: 2001})
	t.Cleanup(e.KillAllServeProcs)
	rec := readServeRecord(t, e)
	got, ok := rec["task-a"]
	if !ok {
		t.Fatalf("record missing task-a after track: %v", rec)
	}
	if got.PID != 2001 || got.ProjectID != "proj-x" {
		t.Fatalf("record = %+v, want pid 2001 project proj-x", got)
	}

	// A second task lands beside the first, not over it.
	e.trackServeProcFor("task-b", "proj-x", &politeProcess{pid: 2002})
	if rec := readServeRecord(t, e); len(rec) != 2 {
		t.Fatalf("record has %d entries after second track, want 2: %v", len(rec), rec)
	}

	// Killing one leaves the other; killing the last removes the file, so a
	// successor never reads a record with nothing in it.
	<-e.killServeProc("task-a")
	if rec := readServeRecord(t, e); len(rec) != 1 || rec["task-b"].PID != 2002 {
		t.Fatalf("record after killing task-a = %v, want only task-b", rec)
	}
	<-e.killServeProc("task-b")
	if rec := readServeRecord(t, e); rec != nil {
		t.Fatalf("record still present with nothing tracked: %v", rec)
	}
}

func TestServeProcs_NoStateDirMeansNoRecord(t *testing.T) {
	cfg := testRunnerConfig()
	cfg.StateDir = ""
	e := NewExecutor(cfg)
	e.trackServeProcFor("task-a", "proj", &politeProcess{pid: 3001})
	t.Cleanup(e.KillAllServeProcs)
	// Nothing to assert on disk; the point is that it did not panic or write
	// to the working directory.
	if _, err := os.Stat("serve-procs.json"); err == nil {
		t.Fatal("wrote serve-procs.json into the working directory")
	}
}

// startSleeper spawns a real child to stand in for a leftover server, and
// makes sure it dies with the test.
func startSleeper(t *testing.T) (*exec.Cmd, <-chan struct{}) {
	t.Helper()
	cmd := exec.Command("sleep", "60")
	if err := cmd.Start(); err != nil {
		t.Fatalf("start sleeper: %v", err)
	}
	done := make(chan struct{})
	go func() { _ = cmd.Wait(); close(done) }()
	t.Cleanup(func() {
		_ = cmd.Process.Kill()
		<-done
	})
	return cmd, done
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
	child, done := startSleeper(t)
	writeServeRecord(t, e, map[string]serveProcRecord{
		"task-a": {PID: child.Process.Pid, ProjectID: "proj", StartedAt: "2026-09-02T17:41:38Z"},
	})
	psAnswering(e, "opencode serve --port 0")

	e.ReapLeftoverServeProcs()
	<-done
	if _, err := os.Stat(e.serveProcsPath()); !os.IsNotExist(err) {
		t.Fatalf("serve record not discarded after reap (err=%v)", err)
	}
}

func TestReapLeftoverServeProcs_SkipsReusedPid(t *testing.T) {
	e := newServeTestExecutor(t)
	child, done := startSleeper(t)
	writeServeRecord(t, e, map[string]serveProcRecord{
		"task-a": {PID: child.Process.Pid, ProjectID: "proj"},
	})
	// The PID is alive, but it is no longer an opencode serve: whatever
	// inherited the number must not be signalled.
	psAnswering(e, "python3 -m http.server 8000")

	e.ReapLeftoverServeProcs()

	select {
	case <-done:
		t.Fatal("killed an unrelated process that reused a recorded pid")
	case <-time.After(500 * time.Millisecond):
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
	started := make(chan error, 1)
	go func() { started <- tr.Start(ctx) }()
	cancel()
	// Join Start before Stop reads its initialized lifecycle fields.
	if err := <-started; err != nil {
		t.Errorf("Start returned error: %v", err)
	}
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

func TestTerminateServe_EscalatesToSIGKILL(t *testing.T) {
	e := newServeTestExecutor(t)
	p := &stubbornProcess{pid: 5001}

	e.terminateServe("task-a", p)

	got := p.log.get()
	if len(got) != 2 || got[0] != syscall.SIGTERM || got[1] != syscall.SIGKILL {
		t.Fatalf("signals = %v, want [SIGTERM SIGKILL]: a single SIGTERM is how servers survived teardown", got)
	}
	if !p.Exited() {
		t.Fatal("server still alive after escalation")
	}
}

func TestTerminateServe_StopsAtSIGTERMWhenHonoured(t *testing.T) {
	e := newServeTestExecutor(t)
	p := &politeProcess{pid: 5002}

	e.terminateServe("task-a", p)

	if got := p.log.get(); len(got) != 1 || got[0] != syscall.SIGTERM {
		t.Fatalf("signals = %v, want [SIGTERM] only: no SIGKILL for a server that exits politely", got)
	}
}

func TestKillAllServeProcs_ConcurrentTeardownIsBounded(t *testing.T) {
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
	oneEscalation := e.serveTermGrace + e.serveKillGrace
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
	// A signal assertion is not an exit barrier; join teardown at cleanup.
	t.Cleanup(func() { serve.simulateExit(0); exec.KillAllServeProcs() })

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
