package runner

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
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

// Executor builds prompts and spawns task processes. It dispatches to different
// backends (OpenCode, Pi RPC, script) based on the task's executor field.
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
// For worktree execution mode, it ensures a git worktree exists and returns its path.
// Fallback chain: worktree > target_workdir > resolved_workdir > config default.
func (e *Executor) ResolveWorkdir(task *types.ResolvedTask) (string, error) {
	executionMode := task.ExecutionMode
	if executionMode == "" {
		executionMode = "worktree" // default matches origin/main
	}

	// Worktree mode: try to create/find a git worktree
	if executionMode == "worktree" {
		worktreePath, err := e.ensureWorktree(task)
		if err != nil {
			return "", fmt.Errorf("ensure worktree: %w", err)
		}
		if worktreePath != "" {
			return worktreePath, nil
		}
		// ensureWorktree returned "" (no branch set, or is main/master) — fall through
	}

	// Fallback chain (both modes)
	if task.TargetWorkdir != "" {
		if _, err := os.Stat(task.TargetWorkdir); err == nil {
			return task.TargetWorkdir, nil
		}
	}
	if task.Workdir != "" {
		homeDir, _ := os.UserHomeDir()
		p := filepath.Join(homeDir, task.Workdir)
		if _, err := os.Stat(p); err == nil {
			return p, nil
		}
	}
	if task.ResolvedWorkdir != "" {
		if _, err := os.Stat(task.ResolvedWorkdir); err == nil {
			return task.ResolvedWorkdir, nil
		}
	}
	return e.config.WorkDir, nil
}

// ensureWorktree ensures a git worktree exists for the task's branch.
// Returns the worktree path, or "" if worktree mode doesn't apply.
// Returns an error if worktree creation fails.
func (e *Executor) ensureWorktree(task *types.ResolvedTask) (string, error) {
	// Guard: explicit current_branch mode
	if task.ExecutionMode == "current_branch" {
		return "", nil
	}
	// Guard: no branch specified
	if task.GitBranch == "" {
		return "", nil
	}
	// Guard: skip for default branches
	if task.GitBranch == "main" || task.GitBranch == "master" {
		return "", nil
	}

	// Resolve main repo path
	mainRepoPath := ""
	if task.Workdir != "" {
		homeDir, _ := os.UserHomeDir()
		mainRepoPath = filepath.Join(homeDir, task.Workdir)
	} else if task.TargetWorkdir != "" {
		if _, err := os.Stat(task.TargetWorkdir); err == nil {
			mainRepoPath = task.TargetWorkdir
		}
	}
	if mainRepoPath == "" {
		// No repo context, can't create worktree
		return "", nil
	}

	// Check if branch is the current branch in the main repo
	cmd := e.CommandFactory("git", "-C", mainRepoPath, "branch", "--show-current")
	if out, err := cmd.Output(); err == nil {
		if strings.TrimSpace(string(out)) == task.GitBranch {
			return "", nil // Branch is current in main repo, run there
		}
	}

	// Check if branch already checked out in an existing worktree
	cmd = e.CommandFactory("git", "-C", mainRepoPath, "worktree", "list", "--porcelain")
	if out, err := cmd.Output(); err == nil {
		if existingPath := findWorktreeForBranch(string(out), task.GitBranch); existingPath != "" {
			return existingPath, nil
		}
	}

	// Compute worktree path: {mainRepo}/.worktrees/{sanitized-branch}
	sanitizedBranch := sanitizeBranchName(task.GitBranch)
	worktreePath := filepath.Join(mainRepoPath, ".worktrees", sanitizedBranch)

	// If already exists on disk, reuse
	if _, err := os.Stat(worktreePath); err == nil {
		return worktreePath, nil
	}

	// Verify main repo exists
	if _, err := os.Stat(mainRepoPath); err != nil {
		return "", fmt.Errorf("main repo not found: %s", mainRepoPath)
	}

	// Ensure .worktrees/ directory exists
	worktreesDir := filepath.Join(mainRepoPath, ".worktrees")
	if err := os.MkdirAll(worktreesDir, 0o755); err != nil {
		return "", fmt.Errorf("create .worktrees dir: %w", err)
	}

	// Ensure .worktrees is in .gitignore
	ensureWorktreesIgnored(mainRepoPath)

	// Check if branch exists
	checkCmd := e.CommandFactory("git", "-C", mainRepoPath, "rev-parse", "--verify", task.GitBranch)
	branchExists := checkCmd.Run() == nil

	// Create worktree
	if branchExists {
		cmd = e.CommandFactory("git", "-C", mainRepoPath, "worktree", "add", worktreePath, task.GitBranch)
	} else {
		// Get default branch
		defaultBranch := getDefaultBranch(e.CommandFactory, mainRepoPath)
		cmd = e.CommandFactory("git", "-C", mainRepoPath, "worktree", "add", "-b", task.GitBranch, worktreePath, defaultBranch)
	}
	if out, err := cmd.CombinedOutput(); err != nil {
		return "", fmt.Errorf("git worktree add failed: %s: %w", strings.TrimSpace(string(out)), err)
	}

	return worktreePath, nil
}

// findWorktreeForBranch parses `git worktree list --porcelain` output and
// returns the path of the worktree that has the given branch checked out.
func findWorktreeForBranch(porcelainOutput, branch string) string {
	var currentPath string
	targetRef := "refs/heads/" + branch
	for _, line := range strings.Split(porcelainOutput, "\n") {
		if strings.HasPrefix(line, "worktree ") {
			currentPath = strings.TrimPrefix(line, "worktree ")
		} else if strings.HasPrefix(line, "branch ") {
			ref := strings.TrimPrefix(line, "branch ")
			if ref == targetRef && currentPath != "" {
				return currentPath
			}
		} else if line == "" {
			currentPath = ""
		}
	}
	return ""
}

// sanitizeBranchName converts a git branch name to a safe directory name.
// "feature/xyz" -> "feature-xyz"
func sanitizeBranchName(branch string) string {
	s := strings.ReplaceAll(branch, "/", "-")
	var result strings.Builder
	for _, c := range s {
		if (c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z') || (c >= '0' && c <= '9') || c == '-' || c == '_' {
			result.WriteRune(c)
		}
	}
	return result.String()
}

// getDefaultBranch returns "main" or "master" based on what exists.
func getDefaultBranch(cmdFactory CommandFactory, repoPath string) string {
	cmd := cmdFactory("git", "-C", repoPath, "rev-parse", "--verify", "main")
	if cmd.Run() == nil {
		return "main"
	}
	return "master"
}

// ensureWorktreesIgnored adds ".worktrees" to .gitignore if not already present.
func ensureWorktreesIgnored(repoPath string) {
	gitignorePath := filepath.Join(repoPath, ".gitignore")
	content, err := os.ReadFile(gitignorePath)
	if err != nil {
		// No .gitignore or can't read - create one
		_ = os.WriteFile(gitignorePath, []byte(".worktrees\n"), 0o644)
		return
	}
	for _, line := range strings.Split(string(content), "\n") {
		if strings.TrimSpace(line) == ".worktrees" {
			return // Already ignored
		}
	}
	// Append
	_ = os.WriteFile(gitignorePath, append(content, []byte("\n.worktrees\n")...), 0o644)
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
		var err error
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
func (e *Executor) spawnOpencode(ctx context.Context, task *types.ResolvedTask, projectID, workdir, promptFile string, opts SpawnOptions) (*SpawnResult, error) {
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

// spawnPi spawns a Pi RPC subprocess that communicates via JSONL over stdin/stdout.
// It uses the existing PiRPCProcess infrastructure from pi_rpc.go.
func (e *Executor) spawnPi(ctx context.Context, task *types.ResolvedTask, projectID, workdir, promptFile string) (*SpawnResult, error) {
	// Read prompt content
	promptContent, err := os.ReadFile(promptFile)
	if err != nil {
		return nil, fmt.Errorf("read prompt file: %w", err)
	}

	// Build command — use "pi" binary, configurable via PiBin if set
	piBin := "pi"
	if e.config.PiBin != "" {
		piBin = e.config.PiBin
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
func (e *Executor) spawnScript(ctx context.Context, task *types.ResolvedTask, projectID, workdir, promptFile string) (*SpawnResult, error) {
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
func (e *Executor) validateScriptCommand(command string) error {
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
func (e *Executor) validateScriptWorkdir(workdir string) error {
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

// spawnHeadless spawns an OpenCode process in headless mode using `opencode run`.
func (e *Executor) spawnHeadless(
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

	// Build environment exports so the spawned OpenCode agent connects
	// to the same brain server the runner is using
	envBlock := e.buildEnvExports(task)

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

// buildEnvExports builds shell export statements for env vars that should be
// propagated to spawned OpenCode processes.
//
// Merge order (later wins):
//  1. Config passthrough: env var names from RunnerConfig.EnvPassthrough,
//     values read from the runner's own environment
//  2. Config-level hardcoded: BRAIN_API_URL and BRAIN_API_TOKEN from runner config
//  3. Task-level: task.Env map overrides everything
func (e *Executor) buildEnvExports(task *types.ResolvedTask) string {
	env := make(map[string]string)

	// 1. Passthrough: read named vars from the runner's own environment
	for _, name := range e.config.EnvPassthrough {
		if val := os.Getenv(name); val != "" {
			env[name] = val
		}
	}

	// 2. Always ensure BRAIN_API_URL and BRAIN_API_TOKEN from runner config
	//    (these may differ from the parent env if explicitly configured)
	if e.config.BrainAPIURL != "" {
		env["BRAIN_API_URL"] = e.config.BrainAPIURL
	}
	if e.config.APIToken != "" {
		env["BRAIN_API_TOKEN"] = e.config.APIToken
	}

	// 3. Task-level overrides win
	if task != nil {
		for k, v := range task.Env {
			env[k] = v
		}
	}

	if len(env) == 0 {
		return ""
	}

	// Sort keys for deterministic output
	keys := make([]string, 0, len(env))
	for k := range env {
		keys = append(keys, k)
	}
	sort.Strings(keys)

	var lines []string
	for _, k := range keys {
		lines = append(lines, fmt.Sprintf(`export %s="%s"`, k, env[k]))
	}
	return strings.Join(lines, "\n") + "\n"
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
		Proc:       NewPidProcess(pid),
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
