package runner

import (
	"context"
	"fmt"
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
// Executor
// =============================================================================

// Executor builds prompts and spawns OpenCode processes.
type Executor struct {
	config         RunnerConfig
	CommandFactory CommandFactory
}

// NewExecutor creates a new Executor with the given configuration.
func NewExecutor(cfg RunnerConfig) *Executor {
	return &Executor{
		config: cfg,
		CommandFactory: func(name string, args ...string) *exec.Cmd {
			return exec.Command(name, args...)
		},
	}
}

// =============================================================================
// Prompt Building
// =============================================================================

// BuildPrompt builds the prompt string for a task.
// If the task has a direct_prompt, it is used verbatim.
// Otherwise, a standard prompt referencing the brain-runner-queue skill is generated.
func (e *Executor) BuildPrompt(task *types.ResolvedTask, isResume bool) string {
	// If direct_prompt is set, use it verbatim (bypasses brain-runner-queue skill workflow)
	if task.DirectPrompt != "" {
		return task.DirectPrompt
	}

	if isResume {
		return fmt.Sprintf(`Load the brain-runner-queue skill and RESUME the interrupted task at brain path: %s

IMPORTANT: This task was previously in_progress but was interrupted.

Use brain_recall to read the task details, then:
1. Check the task file for any progress notes or partial work
2. Assess what work (if any) was already completed
3. If work was partially done, continue from where it left off
4. If unclear what was done, restart the task from the beginning
5. Follow the brain-runner-queue skill workflow to completion
6. Create atomic git commit
7. Capture commit hash (`+"`git rev-parse HEAD`"+`)
8. Mark as completed with summary and include commit hash (note that this was a resumed task)

Start now.`, task.Path)
	}

	return fmt.Sprintf(`Load the brain-runner-queue skill and process the task at brain path: %s

Use brain_recall to read the task details, then follow the brain-runner-queue skill workflow:
1. Mark the task as in_progress
2. Triage complexity (Route A/B/C)
3. Execute the appropriate route
4. Run tests if applicable
5. Create atomic git commit
6. Capture commit hash (`+"`git rev-parse HEAD`"+`)
7. Mark as completed with summary and include commit hash

Start now.`, task.Path)
}

// =============================================================================
// Workdir Resolution
// =============================================================================

// ResolveWorkdir resolves the working directory for a task.
// Priority: target_workdir (if exists) > resolved_workdir (if exists) > config default.
func (e *Executor) ResolveWorkdir(task *types.ResolvedTask) string {
	// target_workdir is an explicit override (absolute path)
	if task.TargetWorkdir != "" {
		if _, err := os.Stat(task.TargetWorkdir); err == nil {
			return task.TargetWorkdir
		}
	}

	// resolved_workdir from the API (already resolved by the server)
	if task.ResolvedWorkdir != "" {
		if _, err := os.Stat(task.ResolvedWorkdir); err == nil {
			return task.ResolvedWorkdir
		}
	}

	return e.config.WorkDir
}

// =============================================================================
// Agent / Model Resolution
// =============================================================================

// GetEffectiveAgent returns the effective agent for a task.
// Precedence: task.Agent > config.Opencode.Agent
func (e *Executor) GetEffectiveAgent(task *types.ResolvedTask) string {
	if task.Agent != "" {
		return task.Agent
	}
	return e.config.Opencode.Agent
}

// GetEffectiveModel returns the effective model for a task.
// Precedence: task.Model > runtimeDefaultModel > config.Opencode.Model
func (e *Executor) GetEffectiveModel(task *types.ResolvedTask, runtimeDefaultModel string) string {
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
func (e *Executor) Spawn(ctx context.Context, task *types.ResolvedTask, projectID string, opts SpawnOptions) (*SpawnResult, error) {
	// Ensure state directory exists
	if err := os.MkdirAll(e.config.StateDir, 0o755); err != nil {
		return nil, fmt.Errorf("ensure state dir: %w", err)
	}

	// Build and save prompt
	prompt := e.BuildPrompt(task, opts.IsResume)
	promptFile := filepath.Join(e.config.StateDir, fmt.Sprintf("prompt_%s_%s.txt", projectID, task.ID))
	if err := os.WriteFile(promptFile, []byte(prompt), 0o644); err != nil {
		return nil, fmt.Errorf("write prompt file: %w", err)
	}

	// Resolve workdir
	workdir := opts.Workdir
	if workdir == "" {
		workdir = e.ResolveWorkdir(task)
	}

	switch opts.Mode {
	case ExecutionModeBackground:
		return e.spawnBackground(ctx, task, projectID, workdir, promptFile, opts.RuntimeDefaultModel)
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

// spawnBackground spawns an OpenCode process in background mode using `opencode run`.
func (e *Executor) spawnBackground(
	ctx context.Context,
	task *types.ResolvedTask,
	projectID string,
	workdir string,
	promptFile string,
	runtimeDefaultModel string,
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
	model := e.GetEffectiveModel(task, runtimeDefaultModel)

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
	cmd.Stdout = logFile
	cmd.Stderr = logFile

	if err := cmd.Start(); err != nil {
		logFile.Close()
		return nil, fmt.Errorf("start opencode process: %w", err)
	}

	return &SpawnResult{
		PID:        cmd.Process.Pid,
		PromptFile: promptFile,
		Workdir:    workdir,
	}, nil
}

// =============================================================================
// Runner Script Helper
// =============================================================================

// buildRunnerScript creates a bash runner script for TUI/Dashboard modes.
// Returns the path to the written script file.
func (e *Executor) buildRunnerScript(task *types.ResolvedTask, projectID, workdir, promptFile string, opts SpawnOptions) (string, error) {
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

	script := fmt.Sprintf(`#!/bin/bash
cd "%s"
"%s" %s%s--port 0 --prompt "$(cat '%s')"
exit_code=$?
echo ""
echo "Task Complete (exit: $exit_code)"
exit $exit_code
`, workdir, e.config.Opencode.Bin, agentFlag, modelFlag, promptFile)

	if err := os.WriteFile(runnerScript, []byte(script), 0o755); err != nil {
		return "", fmt.Errorf("write runner script: %w", err)
	}
	return runnerScript, nil
}

// =============================================================================
// TUI Mode (standalone tmux window)
// =============================================================================

// spawnTUI spawns an OpenCode process in a new tmux window.
func (e *Executor) spawnTUI(
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
		WindowName: windowName,
		PromptFile: promptFile,
		Workdir:    workdir,
	}, nil
}

// =============================================================================
// Dashboard Mode (pane in existing window)
// =============================================================================

// spawnDashboard spawns an OpenCode process in a tmux pane split.
func (e *Executor) spawnDashboard(
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
		PaneID:     paneID,
		PromptFile: promptFile,
		Workdir:    workdir,
	}, nil
}

// =============================================================================
// Cleanup
// =============================================================================

// Cleanup removes temporary files for a task.
func (e *Executor) Cleanup(taskID, projectID string) error {
	files := []string{
		filepath.Join(e.config.StateDir, fmt.Sprintf("prompt_%s_%s.txt", projectID, taskID)),
		filepath.Join(e.config.StateDir, fmt.Sprintf("runner_%s_%s.sh", projectID, taskID)),
		filepath.Join(e.config.StateDir, fmt.Sprintf("output_%s_%s.log", projectID, taskID)),
	}

	for _, f := range files {
		if err := os.Remove(f); err != nil && !os.IsNotExist(err) {
			return fmt.Errorf("remove %s: %w", f, err)
		}
	}

	return nil
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
func ParseLsofOutput(output string) (int, error) {
	if output == "" {
		return 0, fmt.Errorf("empty lsof output")
	}

	for _, line := range strings.Split(output, "\n") {
		if !strings.Contains(line, listenSuffix) {
			continue
		}

		// Find the NAME field — it's the last whitespace-delimited field before (LISTEN)
		// Example: TCP *:52341 (LISTEN)
		// We need to extract the host:port part
		fields := strings.Fields(line)
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
	return ParseLsofOutput(string(output))
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
