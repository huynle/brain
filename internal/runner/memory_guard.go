package runner

import (
	"bufio"
	"bytes"
	"context"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"os/exec"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"
)

// =============================================================================
// Memory guard
// =============================================================================
//
// max_parallel is a count, and a count cannot bound a process. On 2026-09-01
// and 2026-09-04 this runner's host panicked twice (watchdog timeout under
// total memory exhaustion) while running at max_parallel 3: a single task's
// `opencode serve` + `opencode run --attach` pair reached 27 GB + 26 GB, and
// a ten-minute-old pair was already at 10 GB each. See the brain report
// projects/brain-api/report/sgh4c2ti.md for the mechanism (a 198 MB user
// message re-broadcast on every agent step).
//
// Two independent guards live here:
//
//   - A per-task ceiling (task_memory_limit_mb) on the resident memory of the
//     task's whole process tree — driver, serve, and every descendant — polled
//     on each poll tick. Past the ceiling the task is killed and parked in
//     `blocked` with a note. It is parked, not retried: the growth is a
//     property of the workspace, and a retry would die the same way three
//     times over.
//
//   - Admission control before a spawn: refuse when host available memory is
//     under memory_threshold_percent (a setting that had been declared,
//     defaulted, validated, and shown in the config UI since the Go port —
//     and read by nothing), or when OpenCode's SQLite store has grown past
//     opencode_db_max_gb (that file was 101 GB when the second panic hit, and
//     its growth is the same 198 MB payload written once per step).
//
// Measurement goes through `ps` rather than a cgo or gopsutil dependency: one
// `ps -eo pid=,ppid=,rss=` per tick is cheap, identical on darwin and linux,
// and gives the whole tree in one snapshot so parents and children are
// measured at the same instant.

// DefaultTaskMemoryLimitMB is the default per-task ceiling. A healthy
// OpenCode pair sits around 1.5 GB; the pairs that took the host down were
// at 10 GB within ten minutes and 27 GB at the end. 8 GB leaves room for a
// large test suite or a browser under the agent while still tripping long
// before the machine starts swapping.
const DefaultTaskMemoryLimitMB = 8192

// DefaultOpencodeDBMaxGB is the default ceiling on OpenCode's SQLite store.
// Upstream reports of healthy multi-week installs sit at a few GB; the store
// that accompanied the crashes was 101 GB. 32 GB is far above normal use and
// well below the point where write-lock stalls start aborting agent turns.
const DefaultOpencodeDBMaxGB = 32

// procSample is one row of a process-table snapshot.
type procSample struct {
	PID      int
	PPID     int
	RSSBytes int64
}

// sampleProcessTable snapshots every process on the host. Package variable so
// tests can substitute a fixed table.
var sampleProcessTable = func() (map[int]procSample, error) {
	out, err := exec.Command("ps", "-eo", "pid=,ppid=,rss=").Output()
	if err != nil {
		return nil, fmt.Errorf("ps: %w", err)
	}
	return parseProcessTable(out)
}

// parseProcessTable parses `ps -eo pid=,ppid=,rss=` output. RSS is reported
// in kilobytes on both darwin and linux.
func parseProcessTable(out []byte) (map[int]procSample, error) {
	table := make(map[int]procSample)
	sc := bufio.NewScanner(bytes.NewReader(out))
	for sc.Scan() {
		fields := strings.Fields(sc.Text())
		if len(fields) < 3 {
			continue
		}
		pid, err1 := strconv.Atoi(fields[0])
		ppid, err2 := strconv.Atoi(fields[1])
		rssKB, err3 := strconv.ParseInt(fields[2], 10, 64)
		if err1 != nil || err2 != nil || err3 != nil {
			continue
		}
		table[pid] = procSample{PID: pid, PPID: ppid, RSSBytes: rssKB * 1024}
	}
	if err := sc.Err(); err != nil {
		return nil, err
	}
	return table, nil
}

// processTree returns the given roots plus every live descendant, in BFS
// order, deduplicated. Roots that are not in the table are skipped: a pid the
// snapshot does not know is a process that has already exited.
func processTree(table map[int]procSample, roots ...int) []int {
	children := make(map[int][]int, len(table))
	for pid, p := range table {
		children[p.PPID] = append(children[p.PPID], pid)
	}
	seen := make(map[int]bool)
	var order []int
	queue := make([]int, 0, len(roots))
	for _, r := range roots {
		if r <= 0 || seen[r] {
			continue
		}
		if _, ok := table[r]; !ok {
			continue
		}
		seen[r] = true
		queue = append(queue, r)
	}
	for len(queue) > 0 {
		pid := queue[0]
		queue = queue[1:]
		order = append(order, pid)
		for _, c := range children[pid] {
			if !seen[c] {
				seen[c] = true
				queue = append(queue, c)
			}
		}
	}
	return order
}

// treeRSS sums resident memory over a process tree.
func treeRSS(table map[int]procSample, pids []int) int64 {
	var total int64
	for _, pid := range pids {
		total += table[pid].RSSBytes
	}
	return total
}

// hostMemory reports total and available bytes on this host. "Available" is
// what the kernel could hand out without swapping: free + inactive +
// speculative + purgeable pages on darwin (free alone is always near zero on
// a Mac, the compressor keeps it that way), MemAvailable on linux. Package
// variable so tests can substitute fixed numbers.
var hostMemory = func() (total, available int64, err error) {
	switch runtime.GOOS {
	case "darwin":
		return darwinHostMemory()
	case "linux":
		return linuxHostMemory()
	default:
		return 0, 0, fmt.Errorf("host memory: unsupported platform %s", runtime.GOOS)
	}
}

func darwinHostMemory() (total, available int64, err error) {
	memsize, err := exec.Command("sysctl", "-n", "hw.memsize").Output()
	if err != nil {
		return 0, 0, fmt.Errorf("sysctl hw.memsize: %w", err)
	}
	total, err = strconv.ParseInt(strings.TrimSpace(string(memsize)), 10, 64)
	if err != nil {
		return 0, 0, fmt.Errorf("parse hw.memsize: %w", err)
	}
	vmstat, err := exec.Command("vm_stat").Output()
	if err != nil {
		return 0, 0, fmt.Errorf("vm_stat: %w", err)
	}
	available, err = parseVMStatAvailable(vmstat)
	if err != nil {
		return 0, 0, err
	}
	return total, available, nil
}

// parseVMStatAvailable extracts available bytes from `vm_stat` output.
func parseVMStatAvailable(out []byte) (int64, error) {
	var pageSize int64 = 4096
	pages := map[string]int64{}
	sc := bufio.NewScanner(bytes.NewReader(out))
	for sc.Scan() {
		line := sc.Text()
		if strings.HasPrefix(line, "Mach Virtual Memory Statistics") {
			// "(page size of 16384 bytes)"
			if i := strings.Index(line, "page size of "); i >= 0 {
				rest := strings.TrimPrefix(line[i:], "page size of ")
				if n, err := strconv.ParseInt(strings.Fields(rest)[0], 10, 64); err == nil && n > 0 {
					pageSize = n
				}
			}
			continue
		}
		name, value, ok := strings.Cut(line, ":")
		if !ok {
			continue
		}
		value = strings.TrimSuffix(strings.TrimSpace(value), ".")
		n, err := strconv.ParseInt(value, 10, 64)
		if err != nil {
			continue
		}
		pages[strings.TrimSpace(name)] = n
	}
	if _, ok := pages["Pages free"]; !ok {
		return 0, errors.New("vm_stat: no 'Pages free' line")
	}
	avail := pages["Pages free"] + pages["Pages inactive"] + pages["Pages speculative"] + pages["Pages purgeable"]
	return avail * pageSize, nil
}

func linuxHostMemory() (total, available int64, err error) {
	data, err := os.ReadFile("/proc/meminfo")
	if err != nil {
		return 0, 0, err
	}
	return parseMeminfo(data)
}

// parseMeminfo extracts MemTotal and MemAvailable (kB) from /proc/meminfo.
func parseMeminfo(data []byte) (total, available int64, err error) {
	sc := bufio.NewScanner(bytes.NewReader(data))
	for sc.Scan() {
		fields := strings.Fields(sc.Text())
		if len(fields) < 2 {
			continue
		}
		n, perr := strconv.ParseInt(fields[1], 10, 64)
		if perr != nil {
			continue
		}
		switch fields[0] {
		case "MemTotal:":
			total = n * 1024
		case "MemAvailable:":
			available = n * 1024
		}
	}
	if total == 0 {
		return 0, 0, errors.New("/proc/meminfo: no MemTotal")
	}
	return total, available, nil
}

// opencodeDBSize returns the on-disk size of OpenCode's SQLite store plus its
// WAL. Package variable so tests can substitute a fixed size. A missing file
// is size 0, not an error: a host that has never run OpenCode has nothing to
// guard.
var opencodeDBSize = func() (int64, error) {
	path, err := opencodeDBPath()
	if err != nil {
		return 0, err
	}
	var total int64
	for _, p := range []string{path, path + "-wal"} {
		st, err := os.Stat(p)
		if err != nil {
			if os.IsNotExist(err) {
				continue
			}
			return 0, err
		}
		total += st.Size()
	}
	return total, nil
}

// signalProcess is the low-level kill used for descendants the ProcessManager
// does not track. Package variable so tests never signal real pids.
var signalProcess = func(pid int, sig syscall.Signal) error {
	return syscall.Kill(pid, sig)
}

// serveProcLocator is implemented by executors that keep a server process
// beside each task's driver (OpenCodeExecutor). The memory guard needs its
// pid because the server is the half of the pair that grew to 27 GB, and it
// is not in the ProcessManager.
type serveProcLocator interface {
	ServePID(taskID string) int
}

// serveProcessPID returns the pid of the executor-owned server backing a task,
// or 0 when there is none.
func (tr *TaskRunner) serveProcessPID(task RunningTask) int {
	var exec TaskExecutor
	if task.ExecutorType != "" && tr.executorRegistry != nil {
		if e, ok := tr.executorRegistry.Get(task.ExecutorType); ok {
			exec = e
		}
	}
	if exec == nil {
		exec = tr.executor
	}
	if exec == nil && tr.executorRegistry != nil {
		if e, ok := tr.executorRegistry.Get(DefaultExecutorName); ok {
			exec = e
		}
	}
	if loc, ok := exec.(serveProcLocator); ok && loc != nil {
		return loc.ServePID(task.ID)
	}
	return 0
}

// taskMemoryLimitBytes is the configured per-task ceiling, 0 when disabled.
func (tr *TaskRunner) taskMemoryLimitBytes() int64 {
	if tr.config.TaskMemoryLimitMB <= 0 {
		return 0
	}
	return int64(tr.config.TaskMemoryLimitMB) * 1024 * 1024
}

// checkMemoryLimits measures every running task's process tree against the
// per-task ceiling and kills the ones over it. Called once per poll tick.
func (tr *TaskRunner) checkMemoryLimits(ctx context.Context) {
	limit := tr.taskMemoryLimitBytes()
	if limit <= 0 {
		return
	}
	running := tr.processMgr.GetAllRunning()
	if len(running) == 0 {
		return
	}
	table, err := sampleProcessTable()
	if err != nil {
		tr.memoryGuardWarnOnce("sample", "memory guard: cannot sample process table: %v", err)
		return
	}
	for _, info := range running {
		if ctx.Err() != nil {
			return
		}
		if info.Proc == nil {
			continue
		}
		roots := []int{info.Proc.Pid()}
		if serve := tr.serveProcessPID(info.Task); serve > 0 {
			roots = append(roots, serve)
		}
		tree := processTree(table, roots...)
		rss := treeRSS(table, tree)
		if rss <= limit {
			continue
		}
		tr.handleMemoryLimitExceeded(ctx, info, rss, limit, tree)
	}
}

// handleMemoryLimitExceeded kills a task whose process tree has outgrown the
// ceiling and parks it in `blocked` with an explanatory note. Mirrors the
// blocked branch of handleIdleThresholdExceeded so every side table (process
// manager, instance registry, dispatch lease, log streamer, tmux, executor
// artifacts, stats) is settled the same way.
func (tr *TaskRunner) handleMemoryLimitExceeded(ctx context.Context, info ProcessInfo, rss, limit int64, tree []int) {
	task := info.Task
	gb := func(b int64) string { return fmt.Sprintf("%.1f GB", float64(b)/(1024*1024*1024)) }

	tr.logger.Printf("memory guard: task %s/%s process tree %v is at %s, over the %s limit — killing",
		task.ProjectID, task.ID, tree, gb(rss), gb(limit))
	slog.Error("memory guard: task over memory limit, killing",
		"task_id", task.ID, "project_id", task.ProjectID,
		"rss_bytes", rss, "limit_bytes", limit, "pids", tree, "runner_id", tr.runnerID)

	tr.stopLogStreamer(task.ID)

	// The driver first (SIGTERM, then SIGKILL after 5 s), then the executor's
	// own artifacts — which for OpenCode is the `serve` process, torn down with
	// the same escalation. Descendants the ProcessManager never knew about
	// (a browser helper, an MCP gateway) get a SIGTERM so they do not outlive
	// their parents; they are children of processes that are being killed, so
	// a pid reused in the few milliseconds between snapshot and signal is not a
	// realistic hazard.
	tr.processMgr.Kill(ctx, task.ID)
	tr.cleanupTaskArtifacts(task)
	driverPID := info.Proc.Pid()
	servePID := tr.serveProcessPID(task)
	for _, pid := range tree {
		if pid == driverPID || pid == servePID {
			continue
		}
		_ = signalProcess(pid, syscall.SIGTERM)
	}

	note := fmt.Sprintf(
		"\n\n---\n**Killed by runner: memory limit exceeded.** The task's process tree (pids %v) "+
			"reached %s against a %s ceiling (`runner.task_memory_limit_mb`) at %s. "+
			"Parked in `blocked` rather than retried: this much growth is a property of the "+
			"workspace, not of the run — most often OpenCode embedding a huge workspace diff in "+
			"the user message and re-broadcasting it on every step (see brain report sgh4c2ti). "+
			"Fix the cause, then resume the task.\n",
		tree, gb(rss), gb(limit), time.Now().UTC().Format(time.RFC3339))
	if err := tr.client.AppendToTask(ctx, task.Path, note); err != nil {
		tr.logger.Printf("memory guard: failed to append note to %s: %v", task.ID, err)
	}

	tr.processMgr.Remove(task.ID)
	tr.removeInstance(task.InstanceID)

	tr.emitEvent(RunnerEvent{
		Type:       EventTaskStatusChanged,
		TaskID:     task.ID,
		ProjectID:  task.ProjectID,
		TaskPath:   task.Path,
		FeatureID:  task.FeatureID,
		FromStatus: "in_progress",
		ToStatus:   "blocked",
	})
	tr.releaseDispatchLease(ctx, task.ProjectID, task.ID)

	if err := tr.client.UpdateTaskStatus(ctx, task.Path, "blocked"); err != nil {
		tr.logger.Printf("memory guard: failed to mark %s blocked: %v", task.ID, err)
	}

	tr.mu.Lock()
	tr.stats.Failed++
	if tr.processMgr.RunningCount() == 0 {
		tr.status = RunnerStatusPolling
	}
	tr.mu.Unlock()

	tr.cleanupTaskTmux(task)

	tr.emitEvent(RunnerEvent{
		Type:      EventTaskFailed,
		TaskID:    task.ID,
		ProjectID: task.ProjectID,
		TaskPath:  task.Path,
		FeatureID: task.FeatureID,
		Reason:    "memory_limit_exceeded",
	})
}

// =============================================================================
// Admission control
// =============================================================================

// admissionDenial names why a spawn was refused. Code is stable and machine
// readable (it is reported as the dispatch rejection reason); Message is for
// the operator.
type admissionDenial struct {
	Code    string
	Message string
}

func (d *admissionDenial) Error() string {
	return fmt.Sprintf("admission denied (%s): %s", d.Code, d.Message)
}

// ErrAdmissionDenied is wrapped by every admission error so callers can
// distinguish "refused to start" from "tried and failed".
var ErrAdmissionDenied = errors.New("admission denied")

// admissionError adapts a denial to the error chain used by claimAndSpawn.
func admissionError(d *admissionDenial) error {
	return fmt.Errorf("%w: %s: %s", ErrAdmissionDenied, d.Code, d.Message)
}

// admissionCacheTTL bounds how often the host is probed. Several dispatches
// can arrive within a second; the answer does not change that fast.
const admissionCacheTTL = 3 * time.Second

type admissionState struct {
	mu       sync.Mutex
	at       time.Time
	memory   *admissionDenial
	memErr   error
	db       *admissionDenial
	dbErr    error
	lastWarn map[string]time.Time
}

// spawnAdmission decides whether a task may be spawned right now. executorName
// scopes the OpenCode-store check to OpenCode tasks; the memory check applies
// to every executor, because host memory is host memory. A probe failure is
// logged and treated as "allow": a guard that fails closed on a missing
// `vm_stat` would silently stop a runner that was perfectly healthy.
func (tr *TaskRunner) spawnAdmission(executorName string) *admissionDenial {
	st := &tr.admission
	st.mu.Lock()
	defer st.mu.Unlock()

	if time.Since(st.at) > admissionCacheTTL {
		st.memory, st.memErr = tr.probeHostMemory()
		st.db, st.dbErr = tr.probeOpencodeDB()
		st.at = time.Now()
	}
	if st.memErr != nil {
		tr.memoryGuardWarnOnceLocked(st, "memprobe", "memory guard: cannot read host memory, admission check skipped: %v", st.memErr)
	}
	if st.dbErr != nil {
		tr.memoryGuardWarnOnceLocked(st, "dbprobe", "memory guard: cannot stat opencode.db, admission check skipped: %v", st.dbErr)
	}
	if st.memory != nil {
		return st.memory
	}
	if st.db != nil && isOpencodeExecutorName(executorName) {
		return st.db
	}
	return nil
}

func isOpencodeExecutorName(name string) bool {
	return name == "" || name == "opencode" || name == DefaultExecutorName
}

func (tr *TaskRunner) probeHostMemory() (*admissionDenial, error) {
	threshold := tr.config.MemoryThresholdPercent
	if threshold <= 0 {
		return nil, nil
	}
	total, available, err := hostMemory()
	if err != nil {
		return nil, err
	}
	if total <= 0 {
		return nil, errors.New("host memory: total is zero")
	}
	pct := int(available * 100 / total)
	if pct >= threshold {
		return nil, nil
	}
	return &admissionDenial{
		Code: "memory_low",
		Message: fmt.Sprintf("host has %d%% memory available (%.1f of %.1f GB), below runner.memory_threshold_percent=%d",
			pct, float64(available)/(1<<30), float64(total)/(1<<30), threshold),
	}, nil
}

func (tr *TaskRunner) probeOpencodeDB() (*admissionDenial, error) {
	maxGB := tr.config.OpencodeDBMaxGB
	if maxGB <= 0 {
		return nil, nil
	}
	size, err := opencodeDBSize()
	if err != nil {
		return nil, err
	}
	limit := int64(maxGB) << 30
	if size <= limit {
		return nil, nil
	}
	return &admissionDenial{
		Code: "opencode_db_too_large",
		Message: fmt.Sprintf("opencode.db is %.1f GB, over runner.opencode_db_max_gb=%d; every OpenCode process on this host "+
			"pays for that file (write-lock stalls, and the growth itself is usually a runaway summary.diffs — "+
			"see brain report sgh4c2ti). Prune the event table and VACUUM, or raise the limit",
			float64(size)/(1<<30), maxGB),
	}, nil
}

// memoryGuardWarnOnce logs a warning at most once a minute per key so a
// persistent probe failure does not flood the log on every tick.
func (tr *TaskRunner) memoryGuardWarnOnce(key, format string, args ...interface{}) {
	st := &tr.admission
	st.mu.Lock()
	defer st.mu.Unlock()
	tr.memoryGuardWarnOnceLocked(st, key, format, args...)
}

func (tr *TaskRunner) memoryGuardWarnOnceLocked(st *admissionState, key, format string, args ...interface{}) {
	if st.lastWarn == nil {
		st.lastWarn = make(map[string]time.Time)
	}
	if time.Since(st.lastWarn[key]) < time.Minute {
		return
	}
	st.lastWarn[key] = time.Now()
	tr.logger.Printf(format, args...)
}
