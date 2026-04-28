package runner

import (
	"context"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"

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
	PID          int
	Proc         Process
	PaneID       string
	WindowName   string
	PromptFile   string
	OpencodePort int
	SessionID    string
	Workdir      string
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

// Spawn dispatches to mode-specific spawners.
func (e *OpenCodeExecutor) Spawn(ctx context.Context, task *types.ResolvedTask, projectID string, opts SpawnOptions) (*SpawnResult, error) {
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

// =============================================================================
// Background Mode
// =============================================================================

// spawnHeadless spawns an OpenCode process in headless mode using `opencode run`.
func (e *OpenCodeExecutor) spawnHeadless(
	ctx context.Context,
	task *types.ResolvedTask,
	projectID string,
	workdir string,
	promptFile string,
	opts SpawnOptions,
) (*SpawnResult, error) {
	// Create output log file
	outputFile := filepath.Join(e.config.StateDir, fmt.Sprintf("output_%s_%s.log", projectID, task.ID))
	logFile, err := os.Create(outputFile)
	if err != nil {
		return nil, fmt.Errorf("create output log: %w", err)
	}

	// Read prompt content
	promptContent, err := os.ReadFile(promptFile)
	if err != nil {
		logFile.Close()
		return nil, fmt.Errorf("read prompt file: %w", err)
	}

	// Build command args
	agent := e.GetEffectiveAgent(task)
	model := e.GetEffectiveModel(task, opts.RuntimeDefaultModel)

	args := []string{"run"}
	if agent != "" {
		args = append(args, "--agent", agent)
	}
	if model != "" {
		args = append(args, "--model", model)
	}
	args = append(args, string(promptContent))

	// Create command via factory (allows test injection)
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

	// Create process wrapper that tracks exit status.
	// NewOsProcess starts a goroutine that calls cmd.Wait() internally.
	proc := NewOsProcess(cmd)

	// Close the log file when the process exits
	go func() {
		<-proc.Done()
		logFile.Close()
	}()

	return &SpawnResult{
		PID:        cmd.Process.Pid,
		Proc:       proc,
		PromptFile: promptFile,
		Workdir:    workdir,
	}, nil
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

// Cleanup removes temporary files for a task.
func (e *OpenCodeExecutor) Cleanup(taskID, projectID string) error {
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
