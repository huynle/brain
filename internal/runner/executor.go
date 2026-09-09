package runner

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/huynle/brain-api/internal/types"
)

// =============================================================================
// Types
// =============================================================================

// ResumeMode selects how a supervisor-driven resume rebuilds executor context.
type ResumeMode string

const (
	// ResumeModeSameSession reuses the prior executor session id directly
	// (OpenCode true same-session resume).
	ResumeModeSameSession ResumeMode = "same_session"
	// ResumeModeRehydrate starts a fresh session seeded with a bounded prior
	// transcript (Pi, or OpenCode when the session cannot be reloaded).
	ResumeModeRehydrate ResumeMode = "rehydrate"
)

// SpawnOptions configures how a task is spawned.
type SpawnOptions struct {
	Mode                ExecutionMode
	Workdir             string
	IsResume            bool
	PaneID              string
	WindowName          string
	RuntimeDefaultModel string
	LogWriter           io.Writer

	// Resume plumbing (Phase 1). These carry supervisor session-resume intent
	// into Spawn; behavior wiring lands in a later phase.
	ResumeSessionID string     // stored prior session id to reuse (OpenCode same-session)
	ResumeMode      ResumeMode // same_session | rehydrate; empty => legacy IsResume behavior
	InjectedContext string     // supervisor-provided context injected into the prompt
	PriorTranscript string     // optional pre-fetched/bounded transcript for rehydrate
}

// SpawnResult holds the result of spawning a task process.
type SpawnResult struct {
	PID                int
	Proc               Process
	PaneID             string
	WindowName         string
	PromptFile         string
	OpencodePort       int
	SessionID          string
	ExistingSessionIDs map[string]struct{}
	Workdir            string
}

// CommandFactory creates exec.Cmd instances. Injected for testability.
type CommandFactory func(name string, args ...string) *exec.Cmd

// =============================================================================
// OpenCodeExecutor
// =============================================================================

// OpenCodeExecutor builds prompts and spawns OpenCode processes.
// Implements the TaskExecutor interface.
type OpenCodeExecutor struct {
	config         RunnerConfig
	CommandFactory CommandFactory
	// Set at construction, before any serve lifecycle work starts.
	serveTermGrace time.Duration
	serveKillGrace time.Duration

	// serveProcs holds the persistent `opencode serve` process backing each
	// attachable headless task, keyed by task ID. The task is driven by a
	// separate `opencode run --attach` process (tracked for completion); the
	// serve process is torn down in Cleanup.
	serveAdmission sync.RWMutex // shutdown excludes the entire headless spawn
	serveMu        sync.Mutex
	serveStopping  bool
	serveProcs     map[string]*ownedServe
	servePending   map[*ownedServe]struct{}
	serveRecovered map[string]serveProcRecord // unresolved records from startup
}

// An entry is a task generation, not just a task ID. Old driver watchers and
// teardown workers must never remove or signal a replacement generation.
type ownedServe struct {
	taskID string
	proc   Process
	record serveProcRecord
	done   chan struct{} // non-nil only while a termination attempt is running
}

// Compile-time interface check.
var _ TaskExecutor = (*OpenCodeExecutor)(nil)

// CanResumeSession reports whether this runner can reload the given OpenCode
// session's history from disk, enabling a true same-session resume. It is a
// read-only probe: it never opens a live connection and never mutates state.
func (e *OpenCodeExecutor) CanResumeSession(sessionID string) SessionResumeCapability {
	if sessionID == "" {
		return SessionResumeCapability{SameSession: false, Reason: "no prior session id"}
	}
	// Prefer the SQLite store (durable, reloadable even for dead sessions);
	// fall back to the legacy file layout. A non-trivial transcript means the
	// session's history exists on this runner.
	if history, err := readSessionHistorySQLite(sessionID); err == nil && hasSessionHistory(history) {
		return SessionResumeCapability{SameSession: true}
	}
	if history, err := readSessionHistory(sessionID); err == nil && hasSessionHistory(history) {
		return SessionResumeCapability{SameSession: true}
	}
	return SessionResumeCapability{SameSession: false, Reason: "session history not found on disk"}
}

// hasSessionHistory reports whether a marshaled session transcript carries real
// content, filtering out empty / "[]" / "null" results.
func hasSessionHistory(history []byte) bool {
	s := strings.TrimSpace(string(history))
	switch s {
	case "", "[]", "null", "{}":
		return false
	default:
		return len(s) > 0
	}
}

// NewExecutor creates a new OpenCodeExecutor with the given configuration.
// Named NewExecutor (not NewOpenCodeExecutor) for backward compatibility.
func NewExecutor(cfg RunnerConfig) *OpenCodeExecutor {
	return &OpenCodeExecutor{
		config:         cfg,
		serveTermGrace: 5 * time.Second,
		serveKillGrace: 2 * time.Second,
		CommandFactory: func(name string, args ...string) *exec.Cmd {
			return exec.Command(name, args...)
		},
		serveProcs: make(map[string]*ownedServe),
	}
}

// serveProcsFileName persists the PIDs of live `opencode serve` processes in
// the state dir, so a runner that comes up after a crash can reap what its
// predecessor left behind.
//
// The in-memory map alone was not enough. `serve` is a separate PID from the
// `run` driver the ProcessManager tracks, and it was torn down only from
// Cleanup on normal completion and from a goroutine tied to the driver's
// exit. A runner shutdown kills the driver and returns before that goroutine
// wakes, and a crash never runs it at all — so every restart with work in
// flight orphaned one `serve` per task, each holding its full heap forever.
// Five of them, up to 31 hours old, had a 36GB machine deep into swap on
// 2026-09-03. Nothing on disk named them, so no later runner could find them.
const serveProcsFileName = "serve-procs.json"

// serveProcRecord is one persisted serve process.
type serveProcRecord struct {
	PID       int    `json:"pid"`
	ProjectID string `json:"project_id"`
	StartedAt string `json:"started_at"`
}

// serveProcsState is the on-disk form of serveProcs, keyed by task ID.
type serveProcsState struct {
	Procs map[string]serveProcRecord `json:"procs"`
}

// A recovered PID has no child handle. Revalidate its command before EACH
// signal, including escalation after the grace period, to avoid PID reuse.
type recoveredServeProcess struct {
	*PidProcess
	matches func(int) bool
}

func (p *recoveredServeProcess) Kill(sig os.Signal) error {
	if !p.matches(p.Pid()) {
		return fmt.Errorf("recorded serve PID %d no longer matches", p.Pid())
	}
	return p.PidProcess.Kill(sig)
}

func (e *OpenCodeExecutor) serveProcsPath() string {
	return filepath.Join(e.config.StateDir, serveProcsFileName)
}

// trackServeProcFor records the persistent serve process backing a headless
// task, and the project it belongs to for the persisted record.
func (e *OpenCodeExecutor) trackServeProcFor(taskID, projectID string, proc Process) {
	e.serveMu.Lock()
	if e.serveProcs == nil {
		e.serveProcs = make(map[string]*ownedServe)
	}
	if old := e.serveProcs[taskID]; old != nil {
		e.beginServeTeardownLocked(old)
	}
	entry := &ownedServe{taskID: taskID, proc: proc, record: serveProcRecord{
		PID: proc.Pid(), ProjectID: projectID, StartedAt: time.Now().UTC().Format(time.RFC3339),
	}}
	e.serveProcs[taskID] = entry
	e.persistServeProcsLocked()
	var done <-chan struct{}
	if e.serveStopping {
		done = e.beginServeTeardownLocked(entry)
	}
	e.serveMu.Unlock()
	if done != nil {
		<-done // a late caller retains responsibility; never silently admit it
	}
}

// terminateServe escalates SIGTERM → SIGKILL on one serve process.
//
// A single SIGTERM was the old behaviour, and it is how a server mid-request
// or slow to handle the signal survived teardown with brain having already
// discarded its handle. Every sibling kill in the runner escalates; this one
// did not.
func (e *OpenCodeExecutor) terminateServe(taskID string, proc Process) bool {
	if proc == nil || proc.Exited() {
		return true
	}
	_ = proc.Kill(syscall.SIGTERM)
	if waitServeExit(proc, e.serveTermGrace) {
		return true
	}
	slog.Warn("opencode serve ignored SIGTERM; sending SIGKILL", "task_id", taskID, "pid", proc.Pid())
	_ = proc.Kill(syscall.SIGKILL)
	return waitServeExit(proc, e.serveKillGrace)
}

// waitServeExit polls Exited until it is true or d elapses.
func waitServeExit(proc Process, d time.Duration) bool {
	deadline := time.Now().Add(d)
	for time.Now().Before(deadline) {
		if proc.Exited() {
			return true
		}
		time.Sleep(50 * time.Millisecond)
	}
	return proc.Exited()
}

// ServePID reports the pid of the live `opencode serve` process backing a
// task, or 0 when there is none. The memory guard uses it to measure the
// task's whole process tree: the server is the half of the pair the
// ProcessManager never tracked, and the half that reached 27 GB.
func (e *OpenCodeExecutor) ServePID(taskID string) int {
	e.serveMu.Lock()
	entry := e.serveProcs[taskID]
	e.serveMu.Unlock()
	if entry == nil || entry.proc.Exited() {
		return 0
	}
	return entry.proc.Pid()
}

// Cleanup remains asynchronous, but shutdown can join the same attempt.
func (e *OpenCodeExecutor) killServeProc(taskID string) <-chan struct{} {
	e.serveMu.Lock()
	defer e.serveMu.Unlock()
	return e.beginServeTeardownLocked(e.serveProcs[taskID])
}

func (e *OpenCodeExecutor) killServeGeneration(taskID string, proc Process) {
	e.serveMu.Lock()
	defer e.serveMu.Unlock()
	if entry := e.serveProcs[taskID]; entry != nil && entry.proc == proc {
		e.beginServeTeardownLocked(entry)
	}
}

// Caller holds serveMu. Failed attempts stay pending and may be retried by
// shutdown; only confirmed exit permits forgetting the recovery record.
func (e *OpenCodeExecutor) beginServeTeardownLocked(entry *ownedServe) <-chan struct{} {
	if entry == nil {
		done := make(chan struct{})
		close(done)
		return done
	}
	if entry.done != nil {
		return entry.done
	}
	if e.serveProcs[entry.taskID] == entry {
		delete(e.serveProcs, entry.taskID)
	}
	if e.servePending == nil {
		e.servePending = make(map[*ownedServe]struct{})
	}
	e.servePending[entry] = struct{}{}
	done := make(chan struct{})
	entry.done = done
	go func() {
		exited := e.terminateServe(entry.taskID, entry.proc)
		e.serveMu.Lock()
		defer e.serveMu.Unlock()
		if exited {
			delete(e.servePending, entry)
		} else {
			slog.Warn("opencode serve exit unconfirmed; retaining recovery record", "task_id", entry.taskID, "pid", entry.proc.Pid())
		}
		entry.done = nil
		e.persistServeProcsLocked()
		close(done)
	}()
	return done
}

// KillAllServeProcs terminates every serve process this executor started.
//
// Runner shutdown calls this and waits for it. It cannot rely on the
// per-task goroutine that ties a server's lifetime to its driver: that
// goroutine polls the driver once a second and may then hold for session
// idle, and Stop() returns — and the process exits — long before it gets
// there. After admitted headless spawns finish, servers are torn down
// concurrently (one escalation, not one per task). An unconfirmed exit stays
// recoverable on disk; this method does not promise that SIGKILL succeeded.
func (e *OpenCodeExecutor) KillAllServeProcs() {
	e.serveAdmission.Lock()
	defer e.serveAdmission.Unlock()
	e.serveMu.Lock()
	e.serveStopping = true
	var attempts []<-chan struct{}
	for _, entry := range e.serveProcs {
		e.beginServeTeardownLocked(entry)
	}
	for entry := range e.servePending {
		attempts = append(attempts, e.beginServeTeardownLocked(entry))
	}
	e.serveMu.Unlock()
	for {
		for _, done := range attempts {
			<-done
		}
		attempts = nil
		e.serveMu.Lock()
		for entry := range e.servePending {
			if entry.done != nil {
				attempts = append(attempts, entry.done)
			}
		}
		e.serveMu.Unlock()
		if len(attempts) == 0 {
			return
		}
	}
}

// persistServeProcsLocked writes owned serve PIDs to the state dir. Failure is
// logged, not returned: the map is still authoritative for this process, and
// the file only matters to a successor after a crash.
// serveMu serializes snapshot AND publication, including recovery. Atomic
// rename prevents a crash during a write from truncating the previous record.
func (e *OpenCodeExecutor) persistServeProcsLocked() {
	if e.config.StateDir == "" {
		return
	}
	state := serveProcsState{Procs: make(map[string]serveProcRecord, len(e.serveProcs))}
	for key, rec := range e.serveRecovered {
		state.Procs[key] = rec
	}
	add := func(key string, rec serveProcRecord) {
		// Preserve the legacy task-ID key where possible. Overlapping task
		// generations need distinct records; recovery treats keys as labels.
		if _, exists := state.Procs[key]; exists {
			key = fmt.Sprintf("%s#%d", key, rec.PID)
		}
		state.Procs[key] = rec
	}
	for taskID, entry := range e.serveProcs {
		add(taskID, entry.record)
	}
	for entry := range e.servePending {
		add(entry.taskID, entry.record)
	}

	path := e.serveProcsPath()
	if len(state.Procs) == 0 {
		if err := os.Remove(path); err != nil && !os.IsNotExist(err) {
			slog.Warn("failed to clear serve process record", "path", path, "error", err)
		}
		return
	}
	b, err := json.MarshalIndent(state, "", "  ")
	if err != nil {
		return
	}
	if err := os.WriteFile(path+".tmp", b, 0o644); err != nil {
		slog.Warn("failed to persist serve process record", "path", path, "error", err)
		return
	}
	if err := os.Rename(path+".tmp", path); err != nil {
		slog.Warn("failed to publish serve process record", "path", path, "error", err)
	}
}

// ReapLeftoverServeProcs kills serve processes recorded by a previous runner
// that are still alive. Unconfirmed exits retain their original record.
//
// A PID is only signalled if it is alive AND its command line still looks
// like `opencode serve` — PIDs are reused, and a stale record must never kill
// whatever unrelated process inherited the number. Called once at startup.
func (e *OpenCodeExecutor) ReapLeftoverServeProcs() {
	e.serveMu.Lock()
	defer e.serveMu.Unlock()
	path := e.serveProcsPath()
	b, err := os.ReadFile(path)
	if err != nil {
		return
	}

	var state serveProcsState
	if err := json.Unmarshal(b, &state); err != nil {
		slog.Warn("serve process record is unreadable; not reaping", "path", path, "error", err)
		_ = os.Remove(path)
		return
	}
	e.serveRecovered = make(map[string]serveProcRecord)

	reaped := 0
	for taskID, rec := range state.Procs {
		if rec.PID <= 0 || !IsPidAlive(rec.PID) {
			continue
		}
		if !e.looksLikeOpencodeServe(rec.PID) {
			slog.Info("skipping recorded serve pid: command line no longer matches (pid reused)",
				"task_id", taskID, "pid", rec.PID)
			continue
		}
		proc := &recoveredServeProcess{PidProcess: NewPidProcess(rec.PID), matches: e.looksLikeOpencodeServe}
		if !e.terminateServe(taskID, proc) {
			e.serveRecovered[taskID] = rec
			slog.Warn("leftover serve exit unconfirmed; retaining recovery record", "task_id", taskID, "pid", rec.PID)
			continue
		}
		reaped++
		slog.Info("reaped leftover opencode serve from a previous runner",
			"task_id", taskID, "project_id", rec.ProjectID, "pid", rec.PID, "started_at", rec.StartedAt)
	}
	if reaped > 0 {
		slog.Info("reaped leftover opencode serve processes", "count", reaped)
	}
	e.persistServeProcsLocked()
}

// looksLikeOpencodeServe reports whether pid's current command line is an
// opencode serve invocation. Goes through CommandFactory so tests can answer
// without a real process table.
func (e *OpenCodeExecutor) looksLikeOpencodeServe(pid int) bool {
	cmd := e.CommandFactory("ps", "-o", "command=", "-p", strconv.Itoa(pid))
	out, err := cmd.Output()
	if err != nil {
		return false
	}
	line := strings.ToLower(strings.TrimSpace(string(out)))
	bin := strings.ToLower(filepath.Base(e.config.Opencode.Bin))
	if bin == "" || bin == "." {
		bin = "opencode"
	}
	return strings.Contains(line, bin) && strings.Contains(line, "serve")
}

// =============================================================================
// Prompt Building (delegates to common)
// =============================================================================

// BuildPrompt builds the prompt string for a task.
// Delegates to CommonBuildPrompt for the shared prompt template.
func (e *OpenCodeExecutor) BuildPrompt(task *types.ResolvedTask, isResume bool) string {
	return CommonBuildPrompt(task, isResume)
}

// =============================================================================
// Workdir Resolution (delegates to common)
// =============================================================================

// ResolveWorkdir resolves the working directory for a task.
// Delegates to CommonResolveWorkdir for the shared fallback chain.
func (e *OpenCodeExecutor) ResolveWorkdir(task *types.ResolvedTask) (string, error) {
	return CommonResolveWorkdir(task, e.config, e.CommandFactory)
}

// ensureWorktree is retained for test compatibility.
func (e *OpenCodeExecutor) ensureWorktree(task *types.ResolvedTask) (string, error) {
	return ensureWorktreeForTaskWithConfig(task, e.config, e.CommandFactory)
}

// =============================================================================
// Agent / Model Resolution
// =============================================================================

// GetEffectiveAgent returns the effective agent for a task.
// Precedence: task.Agent > config.Opencode.Agent
func (e *OpenCodeExecutor) GetEffectiveAgent(task *types.ResolvedTask) string {
	if task.Agent != "" {
		return task.Agent
	}
	return e.config.Opencode.Agent
}

// GetEffectiveModel returns the effective model for a task.
// Precedence: task.Model > runtimeDefaultModel > config.Opencode.Model
func (e *OpenCodeExecutor) GetEffectiveModel(task *types.ResolvedTask, runtimeDefaultModel string) string {
	if task.Model != "" {
		return task.Model
	}
	if runtimeDefaultModel != "" {
		return runtimeDefaultModel
	}
	return e.config.Opencode.Model
}

// =============================================================================
// Spawning
// =============================================================================

// resolveExecutorType returns the effective executor type for a task.
// Empty or unset executor defaults to "opencode" for backward compatibility.
func resolveExecutorType(task *types.ResolvedTask) string {
	if task.Executor == "" {
		return "opencode"
	}
	return task.Executor
}

// Spawn dispatches to executor-specific and mode-specific spawners.
// The task's Executor field determines which executor backend is used:
//   - "opencode" (default): spawns an OpenCode process via headless/tmux/dashboard modes
//   - "pi": spawns a Pi RPC subprocess communicating via JSONL over stdin/stdout
//   - "script": placeholder for script-based execution (implemented in task #11)
//
// Unknown executor types fail the task with a clear error message.
func (e *OpenCodeExecutor) Spawn(ctx context.Context, task *types.ResolvedTask, projectID string, opts SpawnOptions) (*SpawnResult, error) {
	if err := validateTaskGitRemote(task.GitRemote, e.config); err != nil {
		return nil, err
	}
	if task.TargetWorkdir != "" {
		if err := validateSpawnWorkdir(task.TargetWorkdir, e.config); err != nil {
			return nil, fmt.Errorf("target workdir: %w", err)
		}
	}
	// Ensure state directory exists
	if err := os.MkdirAll(e.config.StateDir, 0o755); err != nil {
		return nil, fmt.Errorf("ensure state dir: %w", err)
	}
	// Build and save prompt. Only a real OpenCode run can perform a true
	// same-session resume; a task routed to pi/script cannot, so
	// sameSessionAllowed follows the resolved executor type. selectResumePrompt
	// falls back to the legacy CommonBuildPrompt for an empty ResumeMode.
	executorType := resolveExecutorType(task)
	sameSessionAllowed := executorType == "opencode"
	prompt := selectResumePrompt(task, opts, sameSessionAllowed)
	promptFile, err := WritePromptFile(e.config.StateDir, projectID, task.ID, prompt)
	if err != nil {
		return nil, err
	}

	// Resolve workdir
	workdir := opts.Workdir
	if workdir == "" {
		workdir, err = e.ResolveWorkdir(task)
		if err != nil {
			return nil, fmt.Errorf("resolve workdir: %w", err)
		}
	}

	if err := validateSpawnWorkdir(workdir, e.config); err != nil {
		return nil, fmt.Errorf("resolve workdir: %w", err)
	}
	// Dispatch based on executor type
	switch executorType {
	case "opencode":
		return e.spawnOpencode(ctx, task, projectID, workdir, promptFile, opts)
	case "pi":
		return e.spawnPi(ctx, task, projectID, workdir, promptFile)
	case "script":
		return e.spawnScript(ctx, task, projectID, workdir, promptFile, opts)
	default:
		return nil, fmt.Errorf("unknown executor type: %q (valid types: opencode, pi, script)", executorType)
	}
}

// spawnOpencode dispatches to mode-specific OpenCode spawners.
func (e *OpenCodeExecutor) spawnOpencode(ctx context.Context, task *types.ResolvedTask, projectID, workdir, promptFile string, opts SpawnOptions) (*SpawnResult, error) {
	switch opts.Mode.SpawnMode() {
	case ExecutionModeHeadless:
		return e.spawnHeadless(ctx, task, projectID, workdir, promptFile, opts)
	case ExecutionModeTmux:
		return e.spawnTmux(ctx, task, projectID, workdir, promptFile, opts)
	case ExecutionModeDashboard:
		return e.spawnDashboard(ctx, task, projectID, workdir, promptFile, opts)
	default:
		return nil, fmt.Errorf("unknown execution mode: %q (valid modes: headless, foreground, tmux, dashboard)", opts.Mode)
	}
}

// spawnPi spawns a Pi RPC subprocess that communicates via JSONL over stdin/stdout.
// It uses the existing PiRPCProcess infrastructure from pi_rpc.go.
func (e *OpenCodeExecutor) spawnPi(ctx context.Context, task *types.ResolvedTask, projectID, workdir, promptFile string) (*SpawnResult, error) {
	// Read prompt content
	promptContent, err := os.ReadFile(promptFile)
	if err != nil {
		return nil, fmt.Errorf("read prompt file: %w", err)
	}

	// Build command — use "pi" binary, configurable via PiBin if set
	piBin := "pi"
	if e.config.Pi.Bin != "" {
		piBin = e.config.Pi.Bin
	}

	cmd := e.CommandFactory(piBin)
	cmd.Env = childEnvironment(task, e.config)
	cmd.Dir = workdir

	// Create output log for stderr (stdout is used for JSONL protocol)
	outputFile := filepath.Join(e.config.StateDir, fmt.Sprintf("output_%s_%s.log", projectID, task.ID))
	logFile, err := os.Create(outputFile)
	if err != nil {
		return nil, fmt.Errorf("create output log: %w", err)
	}
	cmd.Stderr = logFile

	// Start via PiRPCProcess — handles stdin/stdout pipes and process lifecycle
	piProc, err := NewPiRPCProcess(cmd)
	if err != nil {
		logFile.Close()
		return nil, fmt.Errorf("start pi process: %w", err)
	}

	// Close log file when process exits
	go func() {
		<-piProc.Done()
		logFile.Close()
	}()

	// Send the initial prompt
	if err := piProc.SendPrompt(string(promptContent)); err != nil {
		_ = piProc.Kill(nil)
		return nil, fmt.Errorf("send initial prompt to pi: %w", err)
	}

	return &SpawnResult{
		PID:        piProc.PID(),
		Proc:       piProc,
		PromptFile: promptFile,
		Workdir:    workdir,
	}, nil
}

// spawnScript runs a shell command directly instead of spawning an AI agent.
// The task's DirectPrompt field contains the command to execute via bash -c.
// Output (stdout+stderr) is captured to a log file and the process is tracked
// via the standard Process interface for completion detection.
//
// Security: requires ScriptConfig.Enabled, validates against allowed/blocked
// command lists, enforces workdir restrictions, and applies timeout.
func (e *OpenCodeExecutor) spawnScript(ctx context.Context, task *types.ResolvedTask, projectID, workdir, promptFile string, opts SpawnOptions) (*SpawnResult, error) {
	// 1. Check if scripts are enabled
	if !e.config.Script.Enabled {
		return nil, fmt.Errorf("script executor is disabled: set script.enabled=true in runner config to allow script execution")
	}

	// 2. Extract command from DirectPrompt (script tasks use direct_prompt as the command)
	command := task.DirectPrompt
	if command == "" {
		return nil, fmt.Errorf("script executor requires direct_prompt to contain the shell command")
	}

	// 3. Validate command against allowed/blocked lists
	if err := e.validateScriptCommand(command); err != nil {
		return nil, fmt.Errorf("script command rejected: %w", err)
	}

	// 4. Validate workdir against restrictions
	if err := e.validateScriptWorkdir(workdir); err != nil {
		return nil, fmt.Errorf("script workdir rejected: %w", err)
	}

	// 5. Apply timeout
	timeout := e.config.Script.MaxTimeout
	if timeout <= 0 {
		timeout = 300 // default 5 minutes
	}
	scriptCtx, cancel := context.WithTimeout(ctx, time.Duration(timeout)*time.Second)

	// 6. Create output log file
	outputFile := filepath.Join(e.config.StateDir, fmt.Sprintf("output_%s_%s.log", projectID, task.ID))
	logFile, err := os.Create(outputFile)
	if err != nil {
		cancel()
		return nil, fmt.Errorf("create output log: %w", err)
	}

	// 7. Build the command
	cmd := e.CommandFactory("bash", "-c", command)
	cmd.Dir = workdir

	// Tee to the log streamer as well as the file, the same way spawnOpencode
	// does. Without this a script task's output only ever reaches the on-disk
	// log, so it is invisible to GET /tasks/{p}/{t}/logs and to the SSE tail —
	// the Processes view's log pane stays permanently empty for exactly the
	// executor whose output has nowhere else to be read.
	output := io.Writer(logFile)
	if opts.LogWriter != nil {
		output = io.MultiWriter(logFile, opts.LogWriter)
	}
	cmd.Stdout = output
	cmd.Stderr = output

	// Propagate environment
	cmd.Env = childEnvironment(task, e.config)

	// 8. Start the process
	if err := cmd.Start(); err != nil {
		logFile.Close()
		cancel()
		return nil, fmt.Errorf("start script process: %w", err)
	}

	// 9. Create process wrapper
	proc := NewOsProcess(cmd)

	// Close log file and cancel context when process exits
	go func() {
		<-proc.Done()
		logFile.Close()
		cancel()
	}()

	// Enforce timeout: kill process if context deadline exceeded
	go func() {
		<-scriptCtx.Done()
		if scriptCtx.Err() == context.DeadlineExceeded {
			// Force kill the process on timeout
			_ = proc.Kill(syscall.SIGKILL)
		}
	}()

	return &SpawnResult{
		PID:        cmd.Process.Pid,
		Proc:       proc,
		PromptFile: promptFile,
		Workdir:    workdir,
	}, nil
}

// validateScriptCommand checks the command against allowed and blocked lists.
func (e *OpenCodeExecutor) validateScriptCommand(command string) error {
	cfg := e.config.Script

	// Check allowed commands (whitelist)
	if len(cfg.AllowedCommands) > 0 {
		allowed := false
		for _, prefix := range cfg.AllowedCommands {
			if strings.HasPrefix(command, prefix) {
				allowed = true
				break
			}
		}
		if !allowed {
			return fmt.Errorf("command %q does not match any allowed command prefix", command)
		}
	}

	// Check blocked commands (blacklist)
	for _, prefix := range cfg.BlockedCommands {
		if strings.HasPrefix(command, prefix) {
			return fmt.Errorf("command %q matches blocked command prefix %q", command, prefix)
		}
	}

	return nil
}

// validateScriptWorkdir checks workdir against workdir_restrict list.
func (e *OpenCodeExecutor) validateScriptWorkdir(workdir string) error {
	restrictions := e.config.Script.WorkdirRestrict
	if len(restrictions) == 0 {
		return nil // no restrictions
	}

	absWorkdir, err := filepath.Abs(workdir)
	if err != nil {
		return fmt.Errorf("resolve absolute workdir: %w", err)
	}

	for _, allowed := range restrictions {
		absAllowed, err := filepath.Abs(allowed)
		if err != nil {
			continue
		}
		// Check if workdir is under the allowed prefix
		if strings.HasPrefix(absWorkdir, absAllowed) {
			return nil
		}
	}

	return fmt.Errorf("workdir %q is not under any allowed path: %v", workdir, restrictions)
}

// =============================================================================
// Background Mode
// =============================================================================

// spawnHeadless spawns a headless OpenCode task.
//
// By default it makes the task attachable: a persistent `opencode serve`
// process is started (a discoverable HTTP port → registered as the task
// instance), and the task is driven by `opencode run --attach` against it.
// Completion is detected by the run process exiting (unchanged); the serve
// process is torn down in Cleanup.
//
// `opencode run` alone never binds a port (it serves in-process), so it is
// not attachable. When the bridge is disabled (config.Control.Disabled) or
// the serve process fails to come up, this falls back to a plain in-process
// `opencode run` so the task still executes — just not attachable.
func (e *OpenCodeExecutor) spawnHeadless(
	ctx context.Context,
	task *types.ResolvedTask,
	projectID string,
	workdir string,
	promptFile string,
	opts SpawnOptions,
) (*SpawnResult, error) {
	e.serveAdmission.RLock()
	defer e.serveAdmission.RUnlock()
	e.serveMu.Lock()
	stopping := e.serveStopping
	e.serveMu.Unlock()
	if stopping {
		return nil, fmt.Errorf("headless executor is shut down")
	}
	if e.config.Control.Disabled {
		return e.spawnHeadlessDirect(workdir, projectID, task, promptFile, opts, 0, "")
	}

	port, existingSessionIDs, serveProc, err := startHeadlessServerFn(e, workdir, projectID, task.ID)
	if err != nil {
		slog.Warn("headless server unavailable, running task non-attachable",
			"task_id", task.ID, "error", err)
		return e.spawnHeadlessDirect(workdir, projectID, task, promptFile, opts, 0, "")
	}

	// Pin the session up front rather than guessing it afterwards. Creating
	// it here and passing it to `run --session` is the only way to know which
	// session is this task's: `GET /session` lists the whole store, so two
	// tasks sharing a workdir see each other's sessions and post-hoc
	// discovery cannot tell them apart (see createOpencodeSession).
	//
	// Same-session resume is the exception: the supervisor already knows the
	// prior session id, and its history lives in that session, so we reuse it
	// directly and skip creating a fresh one. Everything else (rehydrate and
	// the legacy path) creates a fresh session as before.
	//
	// A create failure is not fatal — the task still runs, and the legacy
	// discovery path picks up whatever session `run` creates for itself.
	var sessionID string
	if opts.ResumeMode == ResumeModeSameSession && opts.ResumeSessionID != "" {
		sessionID = opts.ResumeSessionID
	} else {
		sessionID, err = createOpencodeSessionFn(port, task.Title)
		if err != nil {
			slog.Warn("could not pre-create opencode session; falling back to session discovery",
				"task_id", task.ID, "port", port, "error", err)
			sessionID = ""
		}
	}

	res, err := e.spawnHeadlessDirect(workdir, projectID, task, promptFile, opts, port, sessionID)
	if err != nil {
		// Driver failed to start — don't leak the server we started.
		e.killServeGeneration(task.ID, serveProc)
		return nil, err
	}
	res.ExistingSessionIDs = existingSessionIDs
	res.SessionID = sessionID

	// Tie the server's lifetime to the driver process: when the run process
	// exits (completion, kill, crash, or runner shutdown), tear the server
	// down. Cleanup() is a redundant idempotent safety net.
	//
	// One exception: an injected prompt (goal steering, control-plane send)
	// runs its turn ON the serve process, and the driver's own turn ending
	// does not mean that turn is done. After a clean driver exit, wait for
	// the session to go idle (bounded by steerHoldMax) before killing the
	// server, so steered work isn't torn down mid-flight. This mirrors the
	// completion hold in ProcessManager.CheckCompletion.
	driver := res.Proc
	go func() {
		for !driver.Exited() {
			time.Sleep(time.Second)
		}
		if driver.ExitCode() == 0 {
			deadline := time.Now().Add(steerHoldMax)
			for time.Now().Before(deadline) {
				if sessionStatusForPort(port) != "busy" {
					break
				}
				// A question-tool turn leaves the session reporting busy
				// even though the turn ended. Stop waiting early once the
				// transcript confirms the turn completed. Without a session
				// id we can't probe, so fall back to busy-only waiting.
				if sessionID != "" {
					if ended, _, ok := checkOpencodeTurnEnded(port, sessionID); ok && ended {
						break
					}
				}
				time.Sleep(2 * time.Second)
			}
		}
		e.killServeGeneration(task.ID, serveProc)
	}()

	return res, nil
}

// spawnHeadlessDirect runs `opencode run`. When attachPort > 0 it drives a
// persistent server via `--attach`; otherwise it runs the model in-process
// (not attachable). Returns a SpawnResult whose Proc is the run process
// (its exit signals completion) and whose OpencodePort is attachPort.
func (e *OpenCodeExecutor) spawnHeadlessDirect(
	workdir, projectID string,
	task *types.ResolvedTask,
	promptFile string,
	opts SpawnOptions,
	attachPort int,
	attachSession string,
) (*SpawnResult, error) {
	outputFile := filepath.Join(e.config.StateDir, fmt.Sprintf("output_%s_%s.log", projectID, task.ID))
	logFile, err := os.Create(outputFile)
	if err != nil {
		return nil, fmt.Errorf("create output log: %w", err)
	}

	promptContent, err := os.ReadFile(promptFile)
	if err != nil {
		logFile.Close()
		return nil, fmt.Errorf("read prompt file: %w", err)
	}

	agent := e.GetEffectiveAgent(task)
	model := e.GetEffectiveModel(task, opts.RuntimeDefaultModel)

	args := []string{"run"}
	if attachPort > 0 {
		args = append(args, "--attach", fmt.Sprintf("http://127.0.0.1:%d", attachPort))
		if attachSession != "" {
			args = append(args, "--session", attachSession)
		}
	}
	if agent != "" {
		args = append(args, "--agent", agent)
	}
	if model != "" {
		args = append(args, "--model", model)
	}
	args = append(args, string(promptContent))

	cmd := e.CommandFactory(e.config.Opencode.Bin, args...)
	cmd.Env = childEnvironment(task, e.config)
	cmd.Dir = workdir

	var output io.Writer = logFile
	if opts.LogWriter != nil {
		output = io.MultiWriter(logFile, opts.LogWriter)
	}
	cmd.Stdout = output
	cmd.Stderr = output

	if err := cmd.Start(); err != nil {
		logFile.Close()
		return nil, fmt.Errorf("start opencode process: %w", err)
	}

	proc := NewOsProcess(cmd)
	go func() {
		<-proc.Done()
		logFile.Close()
	}()

	return &SpawnResult{
		PID:          cmd.Process.Pid,
		Proc:         proc,
		PromptFile:   promptFile,
		Workdir:      workdir,
		OpencodePort: attachPort,
		SessionID:    attachSession,
	}, nil
}

// startHeadlessServer spawns `opencode serve --port 0` and waits for it to
// bind a healthy HTTP port. Returns the port and the server process, or an
// error if it never becomes ready (caller falls back to in-process run).
func (e *OpenCodeExecutor) startHeadlessServer(workdir, projectID, taskID string) (int, map[string]struct{}, Process, error) {
	serveLog := filepath.Join(e.config.StateDir, fmt.Sprintf("serve_%s_%s.log", projectID, taskID))
	logFile, err := os.Create(serveLog)
	if err != nil {
		return 0, nil, nil, fmt.Errorf("create serve log: %w", err)
	}

	cmd := e.CommandFactory(e.config.Opencode.Bin, "serve", "--port", "0")
	cmd.Env = childEnvironment(nil, e.config)
	cmd.Dir = workdir
	cmd.Stdout = logFile
	cmd.Stderr = logFile

	if err := cmd.Start(); err != nil {
		logFile.Close()
		return 0, nil, nil, fmt.Errorf("start opencode serve: %w", err)
	}
	proc := NewOsProcess(cmd)
	e.trackServeProcFor(taskID, projectID, proc)
	go func() {
		<-proc.Done()
		logFile.Close()
	}()

	// Poll for the listening port, then confirm the HTTP server is ready.
	for attempt := 0; attempt < 15; attempt++ {
		if proc.Exited() {
			e.killServeGeneration(taskID, proc)
			return 0, nil, nil, fmt.Errorf("opencode serve exited during startup (code %d)", proc.ExitCode())
		}
		if port, derr := DiscoverPort(proc.Pid()); derr == nil && port > 0 && instanceHealthy(port) {
			baseline, _ := listSessionIDs(port)
			return port, baseline, proc, nil
		}
		time.Sleep(1 * time.Second)
	}
	e.killServeGeneration(taskID, proc)
	return 0, nil, nil, fmt.Errorf("opencode serve did not bind a port in time")
}

// =============================================================================
// Runner Script Helper
// =============================================================================

// buildRunnerScript creates a bash runner script for tmux/dashboard modes.
// Returns the path to the written script file.
func (e *OpenCodeExecutor) buildRunnerScript(task *types.ResolvedTask, projectID, workdir, promptFile string, opts SpawnOptions) (string, error) {
	agent := e.GetEffectiveAgent(task)
	model := e.GetEffectiveModel(task, opts.RuntimeDefaultModel)

	runnerScript := filepath.Join(e.config.StateDir, fmt.Sprintf("runner_%s_%s.sh", projectID, task.ID))
	agentFlag := ""
	if agent != "" {
		agentFlag = "--agent " + shellEnvQuote(agent) + " "
	}
	modelFlag := ""
	if model != "" {
		modelFlag = "--model " + shellEnvQuote(model) + " "
	}

	body := fmt.Sprintf(`cd %s || exit 1
%s %s%s--port 0 --prompt "$(cat %s)"
exit_code=$?
echo ""
echo "Task Complete (exit: $exit_code)"
exit $exit_code
`, shellEnvQuote(workdir), shellEnvQuote(e.config.Opencode.Bin), agentFlag, modelFlag, shellEnvQuote(promptFile))
	script := childRunnerScript(body, task, e.config)

	// WriteFile's mode only applies to new files. Restrict existing launchers
	// before writing the sanitized environment, which can contain provider secrets.
	if err := os.Chmod(runnerScript, 0o700); err != nil && !os.IsNotExist(err) {
		return "", fmt.Errorf("restrict runner script permissions: %w", err)
	}
	if err := os.WriteFile(runnerScript, []byte(script), 0o700); err != nil {
		return "", fmt.Errorf("write runner script: %w", err)
	}
	return runnerScript, nil
}

// =============================================================================
// Tmux Mode (standalone tmux window)
// =============================================================================

// spawnTmux spawns an OpenCode process in a new tmux window.
func (e *OpenCodeExecutor) spawnTmux(
	ctx context.Context,
	task *types.ResolvedTask,
	projectID string,
	workdir string,
	promptFile string,
	opts SpawnOptions,
) (*SpawnResult, error) {
	// Build window name
	shortID := task.ID
	if len(task.ID) > 8 {
		shortID = task.ID[len(task.ID)-8:]
	}
	windowName := opts.WindowName
	if windowName == "" {
		windowName = fmt.Sprintf("%s-%s", projectID, shortID)
	}

	// Check if a tmux window with this name already exists (prevent duplicates)
	checkCmd := e.CommandFactory("tmux", "list-windows", "-F", "#{window_name}")
	if checkOutput, err := checkCmd.Output(); err == nil {
		for _, line := range strings.Split(strings.TrimSpace(string(checkOutput)), "\n") {
			if line == windowName {
				return nil, fmt.Errorf("tmux window %q already exists (duplicate spawn prevented)", windowName)
			}
		}
	}

	runnerScript, err := e.buildRunnerScript(task, projectID, workdir, promptFile, opts)
	if err != nil {
		return nil, err
	}

	// Create tmux window
	tmuxCmd := e.CommandFactory("tmux", "new-window", "-d", "-n", windowName, "-c", workdir, runnerScript)
	if err := tmuxCmd.Run(); err != nil {
		return nil, fmt.Errorf("create tmux window: %w", err)
	}

	// Get PID from tmux pane
	pidCmd := e.CommandFactory("tmux", "list-panes", "-t", windowName, "-F", "#{pane_pid}")
	pidOutput, err := pidCmd.Output()
	if err != nil {
		return nil, fmt.Errorf("get pane pid: %w", err)
	}
	pid, _ := strconv.Atoi(strings.TrimSpace(string(pidOutput)))

	return &SpawnResult{
		PID:        pid,
		Proc:       NewPidProcess(pid),
		WindowName: windowName,
		PromptFile: promptFile,
		Workdir:    workdir,
	}, nil
}

// =============================================================================
// Dashboard Mode (pane in existing window)
// =============================================================================

// spawnDashboard spawns an OpenCode process in a tmux pane split.
func (e *OpenCodeExecutor) spawnDashboard(
	ctx context.Context,
	task *types.ResolvedTask,
	projectID string,
	workdir string,
	promptFile string,
	opts SpawnOptions,
) (*SpawnResult, error) {
	runnerScript, err := e.buildRunnerScript(task, projectID, workdir, promptFile, opts)
	if err != nil {
		return nil, err
	}

	// Split existing pane
	splitArgs := []string{"split-window", "-h", "-d", "-P", "-F", "#{pane_id}", runnerScript}
	if opts.PaneID != "" {
		splitArgs = []string{"split-window", "-t", opts.PaneID, "-h", "-d", "-P", "-F", "#{pane_id}", runnerScript}
	}

	splitCmd := e.CommandFactory("tmux", splitArgs...)
	paneOutput, err := splitCmd.Output()
	if err != nil {
		return nil, fmt.Errorf("split tmux pane: %w", err)
	}
	paneID := strings.TrimSpace(string(paneOutput))

	// Get PID
	pidCmd := e.CommandFactory("tmux", "list-panes", "-a", "-F", "#{pane_id} #{pane_pid}")
	pidOutput, err := pidCmd.Output()
	if err != nil {
		return nil, fmt.Errorf("get pane pid: %w", err)
	}

	pid := 0
	for _, line := range strings.Split(string(pidOutput), "\n") {
		if strings.HasPrefix(line, paneID+" ") {
			parts := strings.Fields(line)
			if len(parts) >= 2 {
				pid, _ = strconv.Atoi(parts[1])
			}
			break
		}
	}

	return &SpawnResult{
		PID:        pid,
		Proc:       NewPidProcess(pid),
		PaneID:     paneID,
		PromptFile: promptFile,
		Workdir:    workdir,
	}, nil
}

// =============================================================================
// Cleanup (delegates to common)
// =============================================================================

// Cleanup tears down the task's persistent serve process (if any) and removes
// temporary files. Called on task completion via cleanupTaskArtifacts.
func (e *OpenCodeExecutor) Cleanup(taskID, projectID string) error {
	e.killServeProc(taskID)
	return CommonCleanup(e.config.StateDir, taskID, projectID)
}

// =============================================================================
// Port Discovery
// =============================================================================

// listenSuffix is the marker for LISTEN lines in lsof output.
const listenSuffix = "(LISTEN)"

// portFromName extracts the port number from an lsof NAME field.
// Handles: *:52341, 127.0.0.1:3000, [::]:8080
func portFromName(name string) (int, bool) {
	// Find the last colon — port is after it
	idx := strings.LastIndex(name, ":")
	if idx < 0 {
		return 0, false
	}
	portStr := name[idx+1:]
	// Remove any trailing whitespace or (LISTEN) etc
	portStr = strings.TrimSpace(portStr)
	port, err := strconv.Atoi(portStr)
	if err != nil {
		return 0, false
	}
	return port, true
}

// ParseLsofOutput parses lsof output to find the first LISTEN port.
// Expected format: `lsof -i -P -n -p <pid>`
// If pid > 0, only lines matching that PID are considered (the lsof output
// column format is: COMMAND PID USER FD TYPE DEVICE SIZE/OFF NODE NAME).
func ParseLsofOutput(output string) (int, error) {
	return ParseLsofOutputForPID(output, 0)
}

// ParseLsofOutputForPID parses lsof output to find the first LISTEN port
// belonging to the given PID. If pid is 0, any PID matches.
func ParseLsofOutputForPID(output string, pid int) (int, error) {
	if output == "" {
		return 0, fmt.Errorf("empty lsof output")
	}

	pidStr := ""
	if pid > 0 {
		pidStr = strconv.Itoa(pid)
	}

	for _, line := range strings.Split(output, "\n") {
		if !strings.Contains(line, listenSuffix) {
			continue
		}

		fields := strings.Fields(line)
		if len(fields) < 2 {
			continue
		}

		// Filter by PID if specified (PID is the second field)
		if pidStr != "" && fields[1] != pidStr {
			continue
		}

		// Find the NAME field — it's the last whitespace-delimited field before (LISTEN)
		for i, f := range fields {
			if f == "(LISTEN)" && i > 0 {
				port, ok := portFromName(fields[i-1])
				if ok {
					return port, nil
				}
			}
		}
	}

	return 0, fmt.Errorf("no LISTEN port found in lsof output")
}

// DiscoverPort attempts to discover the port a process is listening on
// by running `lsof -i -P -n -p <pid>`.
func DiscoverPort(pid int) (int, error) {
	cmd := exec.Command("lsof", "-i", "-P", "-n", "-p", strconv.Itoa(pid))
	output, err := cmd.Output()
	if err != nil {
		return 0, fmt.Errorf("lsof failed: %w", err)
	}
	return ParseLsofOutputForPID(string(output), pid)
}

// OpencodeListener describes one OpenCode HTTP server discovered on the host.
type OpencodeListener struct {
	PID  int
	Port int
}

// ParseLsofListeners parses the output of
// `lsof -a -i -P -n -c opencode -sTCP:LISTEN` into a list of (pid, port)
// pairs. The header row and any non-LISTEN lines are skipped. A line that
// can't be parsed is silently dropped — this is best-effort discovery and
// must not surface lsof quirks to the caller.
func ParseLsofListeners(output string) []OpencodeListener {
	var listeners []OpencodeListener
	for _, line := range strings.Split(output, "\n") {
		if !strings.Contains(line, listenSuffix) {
			continue
		}
		fields := strings.Fields(line)
		if len(fields) < 2 {
			continue
		}
		pid, err := strconv.Atoi(fields[1])
		if err != nil {
			continue
		}
		// NAME is the field immediately preceding "(LISTEN)".
		for i, f := range fields {
			if f == listenSuffix && i > 0 {
				if port, ok := portFromName(fields[i-1]); ok && port > 0 {
					listeners = append(listeners, OpencodeListener{PID: pid, Port: port})
				}
				break
			}
		}
	}
	return listeners
}

// DiscoverOpencodeListeners returns only listeners in the caller's current
// runner-owned PID set. An empty ownership set grants no discovery access.
// Ownership must come from process tracking, never session-list baselines.
//
// Returns nil on any lsof error — discovery is best-effort and must not
// fail the caller.
func DiscoverOpencodeListeners(ownedPIDs map[int]bool) []OpencodeListener {
	if len(ownedPIDs) == 0 {
		return nil
	}
	cmd := exec.Command("lsof", "-a", "-i", "-P", "-n", "-c", "opencode", "-sTCP:LISTEN")
	output, err := cmd.Output()
	if err != nil {
		return nil
	}
	return filterOwnedListeners(ParseLsofListeners(string(output)), ownedPIDs)
}

func filterOwnedListeners(listeners []OpencodeListener, ownedPIDs map[int]bool) []OpencodeListener {
	var owned []OpencodeListener
	for _, listener := range listeners {
		if listener.PID > 0 && ownedPIDs[listener.PID] {
			owned = append(owned, listener)
		}
	}
	return owned
}

// =============================================================================
// PID Utilities
// =============================================================================

// IsPidAlive checks if a process with the given PID is still running.
// Uses syscall.Kill with signal 0 to probe without actually sending a signal.
func IsPidAlive(pid int) bool {
	if pid <= 0 {
		return false
	}
	proc, err := os.FindProcess(pid)
	if err != nil {
		return false
	}
	err = proc.Signal(syscall.Signal(0))
	return err == nil
}
