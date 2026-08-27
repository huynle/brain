package runner

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/huynle/brain-api/internal/types"
)

// =============================================================================
// PiExecutor
// =============================================================================

// PiExecutor builds prompts and spawns Pi processes.
// Implements the TaskExecutor interface.
type PiExecutor struct {
	config         RunnerConfig
	CommandFactory CommandFactory
}

// Compile-time interface check.
var _ TaskExecutor = (*PiExecutor)(nil)

// NewPiExecutor creates a new PiExecutor with the given configuration.
func NewPiExecutor(cfg RunnerConfig) *PiExecutor {
	return &PiExecutor{
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
func (e *PiExecutor) BuildPrompt(task *types.ResolvedTask, isResume bool) string {
	return CommonBuildPrompt(task, isResume)
}

// =============================================================================
// Workdir Resolution (delegates to common)
// =============================================================================

// ResolveWorkdir resolves the working directory for a task.
// Delegates to CommonResolveWorkdir for the shared fallback chain.
func (e *PiExecutor) ResolveWorkdir(task *types.ResolvedTask) (string, error) {
	return CommonResolveWorkdir(task, e.config, e.CommandFactory)
}

// =============================================================================
// Agent Bundle Config
// =============================================================================

// agentBundleConfig represents the config.json inside an agent bundle directory.
type agentBundleConfig struct {
	SystemPromptFile string   `json:"system_prompt_file,omitempty"`
	Extension        string   `json:"extension,omitempty"`
	Thinking         string   `json:"thinking,omitempty"`
	Tools            []string `json:"tools,omitempty"`
}

// =============================================================================
// Agent Bundle Resolution
// =============================================================================

// resolveAgentArgs resolves agent name to CLI flags via agent bundle lookup.
//
// Lookup path: config.Pi.AgentsDir/<agentName>/
// If the directory exists, reads config.json for: system_prompt_file, extension, thinking, tools.
// If the directory doesn't exist, falls back to --append-system-prompt.
//
// Returns the CLI args (e.g., --system-prompt-file, -e, --thinking, --tools).
func (e *PiExecutor) resolveAgentArgs(agentName string) []string {
	if agentName == "" {
		return nil
	}

	bundleDir := filepath.Join(e.config.Pi.AgentsDir, agentName)

	// Check if agent bundle directory exists
	info, err := os.Stat(bundleDir)
	if err != nil || !info.IsDir() {
		// Graceful fallback: use append-system-prompt
		return []string{
			"--append-system-prompt",
			fmt.Sprintf("You are the %s agent.", agentName),
		}
	}

	// Load config.json from the bundle
	configPath := filepath.Join(bundleDir, "config.json")
	cfg, err := loadAgentBundleConfig(configPath)
	if err != nil {
		// config.json missing or invalid — fall back
		return []string{
			"--append-system-prompt",
			fmt.Sprintf("You are the %s agent.", agentName),
		}
	}

	var args []string

	// System prompt file
	if cfg.SystemPromptFile != "" {
		promptPath := resolveRelativeToBundleDir(cfg.SystemPromptFile, bundleDir)
		if fileExists(promptPath) {
			args = append(args, "--system-prompt-file", promptPath)
		}
	}

	// Agent-bundled extension (Layer 1)
	if cfg.Extension != "" {
		extPath := resolveRelativeToBundleDir(cfg.Extension, bundleDir)
		if fileExists(extPath) {
			args = append(args, "-e", extPath)
		}
	}

	// Thinking level
	if cfg.Thinking != "" {
		args = append(args, "--thinking", cfg.Thinking)
	}

	// Tool restrictions
	for _, tool := range cfg.Tools {
		args = append(args, "--tools", tool)
	}

	return args
}

// loadAgentBundleConfig reads and parses config.json from an agent bundle.
func loadAgentBundleConfig(path string) (*agentBundleConfig, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var cfg agentBundleConfig
	if err := json.Unmarshal(data, &cfg); err != nil {
		return nil, err
	}
	return &cfg, nil
}

// resolveRelativeToBundleDir resolves a path relative to the bundle directory.
// If the path is already absolute, it is returned as-is.
func resolveRelativeToBundleDir(path, bundleDir string) string {
	if filepath.IsAbs(path) {
		return path
	}
	return filepath.Join(bundleDir, path)
}

// fileExists returns true if the given path exists and is a regular file.
func fileExists(path string) bool {
	info, err := os.Stat(path)
	return err == nil && !info.IsDir()
}

// =============================================================================
// Extension Composition
// =============================================================================

// buildExtensionArgs composes extension CLI args from three layers:
//
//   - Layer 1: Agent-bundled extension (already included via resolveAgentArgs)
//   - Layer 2: Config always-on extensions (config.Pi.Extensions)
//   - Layer 3: Per-task extensions (task.Extensions)
//
// Short name resolution for per-task extensions:
//   - If path contains "/" → use as-is (absolute/relative path)
//   - Otherwise → resolve to config.Pi.ExtensionsDir/brain-<name>.ts
//
// All extensions are additive; Pi handles multiple -e flags.
func (e *PiExecutor) buildExtensionArgs(task *types.ResolvedTask) []string {
	var args []string

	// Layer 2: Config always-on extensions
	for _, ext := range e.config.Pi.Extensions {
		if ext != "" {
			args = append(args, "-e", ext)
		}
	}

	// Layer 3: Per-task extensions
	if task != nil {
		for _, ext := range task.Extensions {
			if ext == "" {
				continue
			}
			resolved := e.resolveExtensionPath(ext)
			args = append(args, "-e", resolved)
		}
	}

	return args
}

// resolveExtensionPath resolves a short extension name to a full path.
// If the name contains "/", it is used as-is. Otherwise it is resolved to:
// config.Pi.ExtensionsDir/brain-<name>.ts
func (e *PiExecutor) resolveExtensionPath(name string) string {
	if strings.Contains(name, "/") {
		return name
	}
	return filepath.Join(e.config.Pi.ExtensionsDir, fmt.Sprintf("brain-%s.ts", name))
}

// =============================================================================
// Model Resolution
// =============================================================================

// GetEffectiveModel returns the effective model for a task.
// Precedence: task.Model > runtimeDefaultModel > config.Pi.Model
func (e *PiExecutor) GetEffectiveModel(task *types.ResolvedTask, runtimeDefaultModel string) string {
	if task.Model != "" {
		return task.Model
	}
	if runtimeDefaultModel != "" {
		return runtimeDefaultModel
	}
	return e.config.Pi.Model
}

// =============================================================================
// Spawning
// =============================================================================

// Spawn dispatches to mode-specific spawners.
func (e *PiExecutor) Spawn(ctx context.Context, task *types.ResolvedTask, projectID string, opts SpawnOptions) (*SpawnResult, error) {
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
		return e.spawnHeadless(ctx, task, projectID, workdir, promptFile, opts.RuntimeDefaultModel)
	case ExecutionModeTUI:
		return e.spawnTUI(ctx, task, projectID, workdir, promptFile, opts)
	case ExecutionModeDashboard:
		return e.spawnDashboard(ctx, task, projectID, workdir, promptFile, opts)
	default:
		return nil, fmt.Errorf("unknown execution mode: %s", opts.Mode)
	}
}

// =============================================================================
// Headless Mode
// =============================================================================

// spawnHeadless spawns a Pi process in headless (RPC) mode.
// The prompt is written to stdin after process start.
func (e *PiExecutor) spawnHeadless(
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

	// Build CLI args
	args := e.buildHeadlessArgs(task, runtimeDefaultModel)

	// Create command via factory
	cmd := e.CommandFactory(e.config.Pi.Bin, args...)
	cmd.Dir = workdir
	cmd.Stdout = logFile
	cmd.Stderr = logFile

	// Set up stdin pipe for sending prompt
	stdin, err := cmd.StdinPipe()
	if err != nil {
		logFile.Close()
		return nil, fmt.Errorf("create stdin pipe: %w", err)
	}

	if err := cmd.Start(); err != nil {
		logFile.Close()
		return nil, fmt.Errorf("start pi process: %w", err)
	}

	// Send prompt via stdin and close
	go func() {
		defer stdin.Close()
		// A short write means the child died before reading its prompt; that
		// surfaces as the process exiting, which CheckCompletion handles.
		_, _ = stdin.Write(promptContent)
	}()

	// Create process wrapper
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

// buildHeadlessArgs constructs the CLI args for Pi in headless (RPC) mode.
func (e *PiExecutor) buildHeadlessArgs(task *types.ResolvedTask, runtimeDefaultModel string) []string {
	args := []string{"--mode", "rpc"}

	// Agent bundle resolution
	agentArgs := e.resolveAgentArgs(task.Agent)
	args = append(args, agentArgs...)

	// Extension composition (layers 2 & 3; layer 1 is in agentArgs)
	extArgs := e.buildExtensionArgs(task)
	args = append(args, extArgs...)

	// Model resolution
	model := e.GetEffectiveModel(task, runtimeDefaultModel)
	if model != "" {
		args = append(args, "--model", model)
	}

	// No-session flag
	if e.config.Pi.NoSession {
		args = append(args, "--no-session")
	}

	return args
}

// =============================================================================
// Runner Script Helper (TUI/Dashboard modes)
// =============================================================================

// buildRunnerScript creates a bash runner script for TUI/Dashboard modes.
func (e *PiExecutor) buildRunnerScript(task *types.ResolvedTask, projectID, workdir, promptFile string, opts SpawnOptions) (string, error) {
	runnerScript := filepath.Join(e.config.StateDir, fmt.Sprintf("runner_%s_%s.sh", projectID, task.ID))

	// Build agent and extension flags
	agentFlags := strings.Join(e.resolveAgentArgs(task.Agent), " ")
	extFlags := strings.Join(e.buildExtensionArgs(task), " ")

	model := e.GetEffectiveModel(task, opts.RuntimeDefaultModel)
	modelFlag := ""
	if model != "" {
		modelFlag = fmt.Sprintf(`--model "%s" `, model)
	}

	noSessionFlag := ""
	if e.config.Pi.NoSession {
		noSessionFlag = "--no-session "
	}

	// Build environment exports using common logic
	envBlock := CommonBuildEnvExports(task, e.config)

	script := fmt.Sprintf(`#!/bin/bash
cd "%s"
%s"%s" %s %s %s%s--prompt "$(cat '%s')"
exit_code=$?
echo ""
echo "Task Complete (exit: $exit_code)"
exit $exit_code
`, workdir, envBlock, e.config.Pi.Bin, agentFlags, extFlags, modelFlag, noSessionFlag, promptFile)

	if err := os.WriteFile(runnerScript, []byte(script), 0o755); err != nil {
		return "", fmt.Errorf("write runner script: %w", err)
	}
	return runnerScript, nil
}

// =============================================================================
// TUI Mode (standalone tmux window)
// =============================================================================

// spawnTUI spawns a Pi process in a new tmux window.
func (e *PiExecutor) spawnTUI(
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

	// Check if a tmux window with this name already exists
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

// spawnDashboard spawns a Pi process in a tmux pane split.
func (e *PiExecutor) spawnDashboard(
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
func (e *PiExecutor) Cleanup(taskID, projectID string) error {
	return CommonCleanup(e.config.StateDir, taskID, projectID)
}
