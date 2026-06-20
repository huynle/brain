package runner

import (
	"context"
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

// SpawnOptions configures how a task is spawned.
type SpawnOptions struct {
	Mode                ExecutionMode
	Workdir             string
	IsResume            bool
	PaneID              string
	WindowName          string
	RuntimeDefaultModel string
	LogWriter           io.Writer
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

	// serveProcs holds the persistent `opencode serve` process backing each
	// attachable headless task, keyed by task ID. The task is driven by a
	// separate `opencode run --attach` process (tracked for completion); the
	// serve process is torn down in Cleanup.
	serveMu    sync.Mutex
	serveProcs map[string]Process
}

// Compile-time interface check.
var _ TaskExecutor = (*OpenCodeExecutor)(nil)

// NewExecutor creates a new OpenCodeExecutor with the given configuration.
// Named NewExecutor (not NewOpenCodeExecutor) for backward compatibility.
func NewExecutor(cfg RunnerConfig) *OpenCodeExecutor {
	return &OpenCodeExecutor{
		config: cfg,
		CommandFactory: func(name string, args ...string) *exec.Cmd {
			return exec.Command(name, args...)
		},
		serveProcs: make(map[string]Process),
	}
}

// trackServeProc records the persistent serve process backing a headless task.
func (e *OpenCodeExecutor) trackServeProc(taskID string, proc Process) {
	e.serveMu.Lock()
	defer e.serveMu.Unlock()
	if e.serveProcs == nil {
		e.serveProcs = make(map[string]Process)
	}
	e.serveProcs[taskID] = proc
}

// killServeProc terminates and forgets the serve process for a task, if any.
func (e *OpenCodeExecutor) killServeProc(taskID string) {
	e.serveMu.Lock()
	proc := e.serveProcs[taskID]
	delete(e.serveProcs, taskID)
	e.serveMu.Unlock()
	if proc != nil && !proc.Exited() {
		_ = proc.Kill(syscall.SIGTERM)
	}
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
	return ensureWorktreeForTask(task, e.CommandFactory)
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
//   - "opencode" (default): spawns an OpenCode process via headless/TUI/dashboard modes
//   - "pi": spawns a Pi RPC subprocess communicating via JSONL over stdin/stdout
//   - "script": placeholder for script-based execution (implemented in task #11)
//
// Unknown executor types fail the task with a clear error message.
func (e *OpenCodeExecutor) Spawn(ctx context.Context, task *types.ResolvedTask, projectID string, opts SpawnOptions) (*SpawnResult, error) {
	// Ensure state directory exists
	if err := os.MkdirAll(e.config.StateDir, 0o755); err != nil {
		return nil, fmt.Errorf("ensure state dir: %w", err)
	}
	// Build and save prompt
	prompt := e.BuildPrompt(task, opts.IsResume)
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

	// Dispatch based on executor type
	executorType := resolveExecutorType(task)
	switch executorType {
	case "opencode":
		return e.spawnOpencode(ctx, task, projectID, workdir, promptFile, opts)
	case "pi":
		return e.spawnPi(ctx, task, projectID, workdir, promptFile)
	case "script":
		return e.spawnScript(ctx, task, projectID, workdir, promptFile)
	default:
		return nil, fmt.Errorf("unknown executor type: %q (valid types: opencode, pi, script)", executorType)
	}
}

// spawnOpencode dispatches to mode-specific OpenCode spawners.
func (e *OpenCodeExecutor) spawnOpencode(ctx context.Context, task *types.ResolvedTask, projectID, workdir, promptFile string, opts SpawnOptions) (*SpawnResult, error) {
	switch opts.Mode {
	case ExecutionModeHeadless:
		return e.spawnHeadless(ctx, task, projectID, workdir, promptFile, opts)
	case ExecutionModeTUI:
		return e.spawnTUI(ctx, task, projectID, workdir, promptFile, opts)
	case ExecutionModeDashboard:
		return e.spawnDashboard(ctx, task, projectID, workdir, promptFile, opts)
	default:
		return nil, fmt.Errorf("unknown execution mode: %s", opts.Mode)
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
func (e *OpenCodeExecutor) spawnScript(ctx context.Context, task *types.ResolvedTask, projectID, workdir, promptFile string) (*SpawnResult, error) {
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
	cmd.Stdout = logFile
	cmd.Stderr = logFile

	// Propagate environment
	cmd.Env = os.Environ()
	if e.config.BrainAPIURL != "" {
		cmd.Env = append(cmd.Env, "BRAIN_API_URL="+e.config.BrainAPIURL)
	}
	if e.config.APIToken != "" {
		cmd.Env = append(cmd.Env, "BRAIN_API_TOKEN="+e.config.APIToken)
	}
	// Task-level env overrides
	for k, v := range task.Env {
		cmd.Env = append(cmd.Env, k+"="+v)
	}

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
	if e.config.Control.Disabled {
		return e.spawnHeadlessDirect(workdir, projectID, task, promptFile, opts, 0)
	}

	port, existingSessionIDs, serveProc, err := e.startHeadlessServer(workdir, projectID, task.ID)
	if err != nil {
		slog.Warn("headless server unavailable, running task non-attachable",
			"task_id", task.ID, "error", err)
		return e.spawnHeadlessDirect(workdir, projectID, task, promptFile, opts, 0)
	}

	res, err := e.spawnHeadlessDirect(workdir, projectID, task, promptFile, opts, port)
	if err != nil {
		// Driver failed to start — don't leak the server we started.
		_ = serveProc.Kill(syscall.SIGTERM)
		return nil, err
	}
	res.ExistingSessionIDs = existingSessionIDs
	e.trackServeProc(task.ID, serveProc)

	// Tie the server's lifetime to the driver process: when the run process
	// exits (completion, kill, crash, or runner shutdown), tear the server
	// down. Cleanup() is a redundant idempotent safety net.
	driver := res.Proc
	go func() {
		for !driver.Exited() {
			time.Sleep(time.Second)
		}
		e.killServeProc(task.ID)
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
	}
	if agent != "" {
		args = append(args, "--agent", agent)
	}
	if model != "" {
		args = append(args, "--model", model)
	}
	args = append(args, string(promptContent))

	cmd := e.CommandFactory(e.config.Opencode.Bin, args...)
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
	cmd.Dir = workdir
	cmd.Stdout = logFile
	cmd.Stderr = logFile

	if err := cmd.Start(); err != nil {
		logFile.Close()
		return 0, nil, nil, fmt.Errorf("start opencode serve: %w", err)
	}
	proc := NewOsProcess(cmd)
	go func() {
		<-proc.Done()
		logFile.Close()
	}()

	// Poll for the listening port, then confirm the HTTP server is ready.
	for attempt := 0; attempt < 15; attempt++ {
		if proc.Exited() {
			return 0, nil, nil, fmt.Errorf("opencode serve exited during startup (code %d)", proc.ExitCode())
		}
		if port, derr := DiscoverPort(proc.Pid()); derr == nil && port > 0 && instanceHealthy(port) {
			baseline, _ := listSessionIDs(port)
			return port, baseline, proc, nil
		}
		time.Sleep(1 * time.Second)
	}
	_ = proc.Kill(syscall.SIGTERM)
	return 0, nil, nil, fmt.Errorf("opencode serve did not bind a port in time")
}

// =============================================================================
// Runner Script Helper
// =============================================================================

// buildRunnerScript creates a bash runner script for TUI/Dashboard modes.
// Returns the path to the written script file.
func (e *OpenCodeExecutor) buildRunnerScript(task *types.ResolvedTask, projectID, workdir, promptFile string, opts SpawnOptions) (string, error) {
	agent := e.GetEffectiveAgent(task)
	model := e.GetEffectiveModel(task, opts.RuntimeDefaultModel)

	runnerScript := filepath.Join(e.config.StateDir, fmt.Sprintf("runner_%s_%s.sh", projectID, task.ID))
	agentFlag := ""
	if agent != "" {
		agentFlag = fmt.Sprintf(`--agent "%s" `, agent)
	}
	modelFlag := ""
	if model != "" {
		modelFlag = fmt.Sprintf(`--model "%s" `, model)
	}

	// Build environment exports using common logic
	envBlock := CommonBuildEnvExports(task, e.config)

	script := fmt.Sprintf(`#!/bin/bash
cd "%s"
%s"%s" %s%s--port 0 --prompt "$(cat '%s')"
exit_code=$?
echo ""
echo "Task Complete (exit: $exit_code)"
exit $exit_code
`, workdir, envBlock, e.config.Opencode.Bin, agentFlag, modelFlag, promptFile)

	if err := os.WriteFile(runnerScript, []byte(script), 0o755); err != nil {
		return "", fmt.Errorf("write runner script: %w", err)
	}
	return runnerScript, nil
}

// =============================================================================
// TUI Mode (standalone tmux window)
// =============================================================================

// spawnTUI spawns an OpenCode process in a new tmux window.
func (e *OpenCodeExecutor) spawnTUI(
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
