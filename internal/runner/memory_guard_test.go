package runner

import (
	"context"
	"errors"
	"strings"
	"sync"
	"syscall"
	"testing"
	"time"
)

// =============================================================================
// Process table parsing and tree walking
// =============================================================================

func TestParseProcessTable(t *testing.T) {
	out := []byte(`    1     0  19584
  5852  5628 121408
  5932  5852 1158608
  6111  5852 450336
  6127  5932  31200
garbage line
  7000  6127
`)
	table, err := parseProcessTable(out)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if len(table) != 5 {
		t.Fatalf("expected 5 rows, got %d: %+v", len(table), table)
	}
	if got := table[5932]; got.PPID != 5852 || got.RSSBytes != 1158608*1024 {
		t.Errorf("row 5932 = %+v", got)
	}
}

func TestProcessTree_IncludesDescendantsAndSkipsDeadRoots(t *testing.T) {
	table := map[int]procSample{
		1:    {PID: 1, PPID: 0, RSSBytes: 1},
		100:  {PID: 100, PPID: 1, RSSBytes: 10},     // runner
		200:  {PID: 200, PPID: 100, RSSBytes: 1000}, // serve
		201:  {PID: 201, PPID: 100, RSSBytes: 500},  // driver
		300:  {PID: 300, PPID: 200, RSSBytes: 30},   // serve child (browser helper)
		301:  {PID: 301, PPID: 300, RSSBytes: 3},    // grandchild
		9999: {PID: 9999, PPID: 1, RSSBytes: 777},   // unrelated
	}
	tree := processTree(table, 201, 200, 424242 /* exited */, 0)
	want := map[int]bool{200: true, 201: true, 300: true, 301: true}
	if len(tree) != len(want) {
		t.Fatalf("tree = %v, want pids %v", tree, want)
	}
	for _, pid := range tree {
		if !want[pid] {
			t.Errorf("unexpected pid %d in tree %v", pid, tree)
		}
	}
	if rss := treeRSS(table, tree); rss != 1000+500+30+3 {
		t.Errorf("treeRSS = %d, want %d", rss, 1533)
	}
	// The runner itself and unrelated processes must never be counted.
	for _, pid := range tree {
		if pid == 100 || pid == 9999 || pid == 1 {
			t.Errorf("tree leaked outside the task: %v", tree)
		}
	}
}

func TestParseVMStatAvailable(t *testing.T) {
	out := []byte(`Mach Virtual Memory Statistics: (page size of 16384 bytes)
Pages free:                               11849.
Pages active:                            883301.
Pages inactive:                         1015412.
Pages speculative:                        23115.
Pages throttled:                              0.
Pages wired down:                        349418.
Pages purgeable:                          24040.
"Translation faults":                 123456789.
`)
	got, err := parseVMStatAvailable(out)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	want := int64(11849+1015412+23115+24040) * 16384
	if got != want {
		t.Errorf("available = %d, want %d", got, want)
	}
	if _, err := parseVMStatAvailable([]byte("nonsense\n")); err == nil {
		t.Error("expected an error for output without 'Pages free'")
	}
}

func TestParseMeminfo(t *testing.T) {
	data := []byte("MemTotal:       32768000 kB\nMemFree:         1000000 kB\nMemAvailable:   16384000 kB\nBuffers: 1 kB\n")
	total, avail, err := parseMeminfo(data)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if total != 32768000*1024 || avail != 16384000*1024 {
		t.Errorf("total=%d avail=%d", total, avail)
	}
}

// =============================================================================
// Per-task ceiling
// =============================================================================

// serveAwareExecutor is a mockExecutor that also reports a serve pid, the way
// OpenCodeExecutor does, so the guard's tree includes the server half.
type serveAwareExecutor struct {
	*mockExecutor
	servePIDs map[string]int
}

func (e *serveAwareExecutor) ServePID(taskID string) int { return e.servePIDs[taskID] }

// stubMemoryProbes swaps the package-level probes for the duration of a test
// and records every descendant signal instead of delivering it.
func stubMemoryProbes(t *testing.T, table map[int]procSample) *[]int {
	t.Helper()
	origSample, origSignal := sampleProcessTable, signalProcess
	var mu sync.Mutex
	signalled := &[]int{}
	sampleProcessTable = func() (map[int]procSample, error) { return table, nil }
	signalProcess = func(pid int, sig syscall.Signal) error {
		mu.Lock()
		defer mu.Unlock()
		*signalled = append(*signalled, pid)
		return nil
	}
	t.Cleanup(func() {
		sampleProcessTable, signalProcess = origSample, origSignal
	})
	return signalled
}

func TestCheckMemoryLimits_KillsAndBlocksTaskOverLimit(t *testing.T) {
	const gb = 1024 * 1024 * 1024
	table := map[int]procSample{
		100: {PID: 100, PPID: 1, RSSBytes: 100 * 1024 * 1024},   // runner
		200: {PID: 200, PPID: 100, RSSBytes: 6 * gb},            // serve
		201: {PID: 201, PPID: 100, RSSBytes: 5 * gb},            // driver
		300: {PID: 300, PPID: 200, RSSBytes: 50 * 1024 * 1024},  // serve child
		500: {PID: 500, PPID: 100, RSSBytes: 700 * 1024 * 1024}, // a healthy task's driver
	}
	signalled := stubMemoryProbes(t, table)

	client := newMockClient()
	exec := &serveAwareExecutor{mockExecutor: newMockExecutor(), servePIDs: map[string]int{"fat": 200}}
	pm := newMockProcessMgr()
	tr := NewTaskRunner(TaskRunnerOptions{
		Projects:   []string{"proj-a"},
		Config:     testRunnerConfig(),
		Mode:       ExecutionModeHeadless,
		Client:     client,
		Executors:  map[string]TaskExecutor{"opencode": exec},
		ProcessMgr: pm,
		StateMgr:   newMockStateMgr(),
	})
	tr.config.TaskMemoryLimitMB = 8192

	fat := RunningTask{ID: "fat", Path: "projects/proj-a/task/fat.md", ProjectID: "proj-a", ExecutorType: "opencode", InstanceID: "inst_fat", StartedAt: time.Now()}
	slim := RunningTask{ID: "slim", Path: "projects/proj-a/task/slim.md", ProjectID: "proj-a", ExecutorType: "opencode", StartedAt: time.Now()}
	if err := pm.Add("fat", fat, newMockProcess(201)); err != nil {
		t.Fatal(err)
	}
	if err := pm.Add("slim", slim, newMockProcess(500)); err != nil {
		t.Fatal(err)
	}

	tr.checkMemoryLimits(context.Background())

	// The driver was killed through the process manager…
	if len(pm.killCalls) != 1 || pm.killCalls[0] != "fat" {
		t.Fatalf("killCalls = %v, want [fat]", pm.killCalls)
	}
	// …the executor cleaned up (which is what tears down `serve`)…
	if len(exec.cleanupCalls) != 1 || exec.cleanupCalls[0].TaskID != "fat" {
		t.Errorf("cleanupCalls = %v, want fat", exec.cleanupCalls)
	}
	// …the untracked descendant got a signal, the roots did not (they have
	// their own escalating teardown)…
	if len(*signalled) != 1 || (*signalled)[0] != 300 {
		t.Errorf("descendant signals = %v, want [300]", *signalled)
	}
	// …the task is parked in blocked with an explanatory note…
	if len(client.updateStatusCalls) != 1 || client.updateStatusCalls[0].Status != "blocked" || client.updateStatusCalls[0].TaskPath != fat.Path {
		t.Errorf("updateStatusCalls = %+v, want one blocked for %s", client.updateStatusCalls, fat.Path)
	}
	if len(client.appendCalls) != 1 || !strings.Contains(client.appendCalls[0].Content, "memory limit exceeded") ||
		!strings.Contains(client.appendCalls[0].Content, "11.0 GB") {
		t.Errorf("appendCalls = %+v, want a memory-limit note quoting 11.0 GB", client.appendCalls)
	}
	// …its dispatch lease is released and it is gone from the process table…
	if len(client.releaseDispatchCalls) != 1 || client.releaseDispatchCalls[0].TaskID != "fat" {
		t.Errorf("releaseDispatchCalls = %+v, want one for fat", client.releaseDispatchCalls)
	}
	if pm.Get("fat") != nil {
		t.Error("fat should have been removed from the process manager")
	}
	// …and the healthy task was left alone.
	if pm.Get("slim") == nil {
		t.Error("slim should still be tracked")
	}
	if tr.stats.Failed != 1 {
		t.Errorf("stats.Failed = %d, want 1", tr.stats.Failed)
	}
}

func TestCheckMemoryLimits_DriverAloneUnderLimitButPairOver(t *testing.T) {
	// The whole point of measuring the pair: each half is under the ceiling,
	// together they are not. A driver-only check would have passed this.
	const gb = 1024 * 1024 * 1024
	table := map[int]procSample{
		200: {PID: 200, PPID: 1, RSSBytes: 5 * gb}, // serve
		201: {PID: 201, PPID: 1, RSSBytes: 4 * gb}, // driver
	}
	stubMemoryProbes(t, table)
	client := newMockClient()
	exec := &serveAwareExecutor{mockExecutor: newMockExecutor(), servePIDs: map[string]int{"t1": 200}}
	pm := newMockProcessMgr()
	tr := NewTaskRunner(TaskRunnerOptions{
		Projects: []string{"proj-a"}, Config: testRunnerConfig(), Mode: ExecutionModeHeadless,
		Client: client, Executors: map[string]TaskExecutor{"opencode": exec}, ProcessMgr: pm, StateMgr: newMockStateMgr(),
	})
	tr.config.TaskMemoryLimitMB = 8192
	_ = pm.Add("t1", RunningTask{ID: "t1", Path: "projects/proj-a/task/t1.md", ProjectID: "proj-a", ExecutorType: "opencode"}, newMockProcess(201))

	tr.checkMemoryLimits(context.Background())

	if len(pm.killCalls) != 1 {
		t.Fatalf("expected the pair to be killed, killCalls = %v", pm.killCalls)
	}
}

func TestCheckMemoryLimits_DisabledWhenLimitZero(t *testing.T) {
	const gb = 1024 * 1024 * 1024
	table := map[int]procSample{201: {PID: 201, PPID: 1, RSSBytes: 50 * gb}}
	stubMemoryProbes(t, table)
	client := newMockClient()
	pm := newMockProcessMgr()
	tr := newTestRunner(client, newMockExecutor(), pm, newMockStateMgr())
	tr.config.TaskMemoryLimitMB = 0
	_ = pm.Add("t1", RunningTask{ID: "t1", Path: "p", ProjectID: "proj-a"}, newMockProcess(201))

	tr.checkMemoryLimits(context.Background())

	if len(pm.killCalls) != 0 || len(client.updateStatusCalls) != 0 {
		t.Errorf("guard should be inert at limit 0: kills=%v status=%v", pm.killCalls, client.updateStatusCalls)
	}
}

func TestCheckMemoryLimits_SampleFailureIsNotAKill(t *testing.T) {
	origSample := sampleProcessTable
	sampleProcessTable = func() (map[int]procSample, error) { return nil, errors.New("ps exploded") }
	t.Cleanup(func() { sampleProcessTable = origSample })
	client := newMockClient()
	pm := newMockProcessMgr()
	tr := newTestRunner(client, newMockExecutor(), pm, newMockStateMgr())
	tr.config.TaskMemoryLimitMB = 1
	_ = pm.Add("t1", RunningTask{ID: "t1", Path: "p", ProjectID: "proj-a"}, newMockProcess(201))

	tr.checkMemoryLimits(context.Background())

	if len(pm.killCalls) != 0 {
		t.Errorf("a failed measurement must not kill anything: %v", pm.killCalls)
	}
}

// =============================================================================
// Admission control
// =============================================================================

func stubAdmissionProbes(t *testing.T, total, available int64, memErr error, dbSize int64, dbErr error) {
	t.Helper()
	origMem, origDB := hostMemory, opencodeDBSize
	hostMemory = func() (int64, int64, error) { return total, available, memErr }
	opencodeDBSize = func() (int64, error) { return dbSize, dbErr }
	t.Cleanup(func() { hostMemory, opencodeDBSize = origMem, origDB })
}

func TestSpawnAdmission_MemoryLow(t *testing.T) {
	const gb = 1024 * 1024 * 1024
	stubAdmissionProbes(t, 36*gb, 2*gb, nil, 0, nil) // 5% available
	tr := newTestRunner(newMockClient(), newMockExecutor(), newMockProcessMgr(), newMockStateMgr())
	tr.config.MemoryThresholdPercent = 10
	tr.config.OpencodeDBMaxGB = 0

	d := tr.spawnAdmission("pi")
	if d == nil || d.Code != "memory_low" {
		t.Fatalf("denial = %+v, want memory_low", d)
	}
	if !strings.Contains(d.Message, "5%") {
		t.Errorf("message should quote the percentage: %q", d.Message)
	}
}

func TestSpawnAdmission_MemoryFine(t *testing.T) {
	const gb = 1024 * 1024 * 1024
	stubAdmissionProbes(t, 36*gb, 18*gb, nil, 0, nil)
	tr := newTestRunner(newMockClient(), newMockExecutor(), newMockProcessMgr(), newMockStateMgr())
	tr.config.MemoryThresholdPercent = 10
	tr.config.OpencodeDBMaxGB = 0
	if d := tr.spawnAdmission("opencode"); d != nil {
		t.Fatalf("unexpected denial: %+v", d)
	}
}

func TestSpawnAdmission_OpencodeDBTooLarge_OnlyGatesOpencode(t *testing.T) {
	const gb = 1024 * 1024 * 1024
	stubAdmissionProbes(t, 36*gb, 18*gb, nil, 101*gb, nil)
	tr := newTestRunner(newMockClient(), newMockExecutor(), newMockProcessMgr(), newMockStateMgr())
	tr.config.MemoryThresholdPercent = 10
	tr.config.OpencodeDBMaxGB = 32

	if d := tr.spawnAdmission("opencode"); d == nil || d.Code != "opencode_db_too_large" {
		t.Fatalf("opencode denial = %+v, want opencode_db_too_large", d)
	}
	if d := tr.spawnAdmission(""); d == nil || d.Code != "opencode_db_too_large" {
		t.Fatalf("default-executor denial = %+v, want opencode_db_too_large", d)
	}
	if d := tr.spawnAdmission("pi"); d != nil {
		t.Fatalf("pi should not be gated by the opencode store: %+v", d)
	}
}

func TestSpawnAdmission_ProbeFailureAllows(t *testing.T) {
	stubAdmissionProbes(t, 0, 0, errors.New("no vm_stat"), 0, errors.New("no home"))
	tr := newTestRunner(newMockClient(), newMockExecutor(), newMockProcessMgr(), newMockStateMgr())
	tr.config.MemoryThresholdPercent = 10
	tr.config.OpencodeDBMaxGB = 32
	if d := tr.spawnAdmission("opencode"); d != nil {
		t.Fatalf("a broken probe must fail open, got %+v", d)
	}
}

func TestSpawnAdmission_DisabledWhenZero(t *testing.T) {
	const gb = 1024 * 1024 * 1024
	stubAdmissionProbes(t, 36*gb, 1*gb, nil, 500*gb, nil)
	tr := newTestRunner(newMockClient(), newMockExecutor(), newMockProcessMgr(), newMockStateMgr())
	tr.config.MemoryThresholdPercent = 0
	tr.config.OpencodeDBMaxGB = 0
	if d := tr.spawnAdmission("opencode"); d != nil {
		t.Fatalf("both guards off, got %+v", d)
	}
}

func TestClaimAndSpawn_RefusedBeforeClaimWhenMemoryLow(t *testing.T) {
	const gb = 1024 * 1024 * 1024
	stubAdmissionProbes(t, 36*gb, 1*gb, nil, 0, nil)
	client := newMockClient()
	tr := newTestRunner(client, newMockExecutor(), newMockProcessMgr(), newMockStateMgr())
	tr.config.MemoryThresholdPercent = 10

	err := tr.claimAndSpawn(context.Background(), testTask("t1", "proj-a"), "proj-a")
	if !errors.Is(err, ErrAdmissionDenied) {
		t.Fatalf("err = %v, want ErrAdmissionDenied", err)
	}
	if len(client.claimCalls) != 0 {
		t.Errorf("the task must not be claimed when admission is refused: %+v", client.claimCalls)
	}
}
