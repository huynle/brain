package runner

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/huynle/brain-api/internal/types"
)

// =============================================================================
// Shared Executor Logic
//
// Functions in this file are reusable across all executor implementations
// (OpenCode, Pi, etc.). They handle workdir resolution, prompt building,
// environment variable propagation, and temp file management.
// =============================================================================

// =============================================================================
// Workdir Resolution
// =============================================================================

// CommonResolveWorkdir resolves the working directory for a task.
// For worktree execution mode, it ensures a git worktree exists and returns its path.
// Fallback chain: worktree > target_workdir > resolved_workdir > config default.
//
// This is executor-agnostic and can be used by any executor implementation.
func CommonResolveWorkdir(task *types.ResolvedTask, config RunnerConfig, cmdFactory CommandFactory) (string, error) {
	executionMode := task.ExecutionMode
	if executionMode == "" {
		executionMode = "worktree" // default matches origin/main
	}

	// Worktree mode: try to create/find a git worktree
	if executionMode == "worktree" {
		worktreePath, err := ensureWorktreeForTask(task, cmdFactory)
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
	return config.WorkDir, nil
}

// ensureWorktreeForTask ensures a git worktree exists for the task's branch.
// Returns the worktree path, or "" if worktree mode doesn't apply.
// Returns an error if worktree creation fails.
func ensureWorktreeForTask(task *types.ResolvedTask, cmdFactory CommandFactory) (string, error) {
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
	cmd := cmdFactory("git", "-C", mainRepoPath, "branch", "--show-current")
	if out, err := cmd.Output(); err == nil {
		if strings.TrimSpace(string(out)) == task.GitBranch {
			return "", nil // Branch is current in main repo, run there
		}
	}

	// Check if branch already checked out in an existing worktree
	cmd = cmdFactory("git", "-C", mainRepoPath, "worktree", "list", "--porcelain")
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
	checkCmd := cmdFactory("git", "-C", mainRepoPath, "rev-parse", "--verify", task.GitBranch)
	branchExists := checkCmd.Run() == nil

	// Create worktree
	if branchExists {
		cmd = cmdFactory("git", "-C", mainRepoPath, "worktree", "add", worktreePath, task.GitBranch)
	} else {
		// Get default branch
		defaultBranch := getDefaultBranch(cmdFactory, mainRepoPath)
		cmd = cmdFactory("git", "-C", mainRepoPath, "worktree", "add", "-b", task.GitBranch, worktreePath, defaultBranch)
	}
	if out, err := cmd.CombinedOutput(); err != nil {
		return "", fmt.Errorf("git worktree add failed: %s: %w", strings.TrimSpace(string(out)), err)
	}

	return worktreePath, nil
}

// =============================================================================
// Prompt Building
// =============================================================================

// CommonBuildPrompt builds the standard prompt string for a task.
// If the task has a direct_prompt, it is used verbatim.
// Otherwise, a standard prompt referencing the brain-runner-queue skill is generated.
func CommonBuildPrompt(task *types.ResolvedTask, isResume bool) string {
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
// Environment Variable Propagation
// =============================================================================

// CommonBuildEnvExports builds shell export statements for env vars that should be
// propagated to spawned executor processes.
//
// Merge order (later wins):
//  1. Config passthrough: env var names from RunnerConfig.EnvPassthrough,
//     values read from the runner's own environment
//  2. Config-level hardcoded: BRAIN_API_URL and BRAIN_API_TOKEN from runner config
//  3. Task-level: task.Env map overrides everything
func CommonBuildEnvExports(task *types.ResolvedTask, config RunnerConfig) string {
	env := make(map[string]string)

	// 1. Passthrough: read named vars from the runner's own environment
	for _, name := range config.EnvPassthrough {
		if val := os.Getenv(name); val != "" {
			env[name] = val
		}
	}

	// 2. Always ensure BRAIN_API_URL and BRAIN_API_TOKEN from runner config
	if config.BrainAPIURL != "" {
		env["BRAIN_API_URL"] = config.BrainAPIURL
	}
	if config.APIToken != "" {
		env["BRAIN_API_TOKEN"] = config.APIToken
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

// CommonBuildEnvMap builds a flat map of env vars that should be propagated
// to spawned executor processes. Same merge logic as CommonBuildEnvExports
// but returns a map for use with exec.Cmd.Env.
func CommonBuildEnvMap(task *types.ResolvedTask, config RunnerConfig) map[string]string {
	env := make(map[string]string)

	// 1. Passthrough
	for _, name := range config.EnvPassthrough {
		if val := os.Getenv(name); val != "" {
			env[name] = val
		}
	}

	// 2. Config-level
	if config.BrainAPIURL != "" {
		env["BRAIN_API_URL"] = config.BrainAPIURL
	}
	if config.APIToken != "" {
		env["BRAIN_API_TOKEN"] = config.APIToken
	}

	// 3. Task-level
	if task != nil {
		for k, v := range task.Env {
			env[k] = v
		}
	}

	return env
}

// =============================================================================
// Temp File Management
// =============================================================================

// WritePromptFile writes the prompt to a temp file and returns its path.
func WritePromptFile(stateDir, projectID, taskID, prompt string) (string, error) {
	if err := os.MkdirAll(stateDir, 0o755); err != nil {
		return "", fmt.Errorf("ensure state dir: %w", err)
	}
	promptFile := filepath.Join(stateDir, fmt.Sprintf("prompt_%s_%s.txt", projectID, taskID))
	if err := os.WriteFile(promptFile, []byte(prompt), 0o644); err != nil {
		return "", fmt.Errorf("write prompt file: %w", err)
	}
	return promptFile, nil
}

// =============================================================================
// Git Worktree Utilities
// =============================================================================

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

// CommonCleanup removes temporary files for a task.
func CommonCleanup(stateDir, taskID, projectID string) error {
	files := []string{
		filepath.Join(stateDir, fmt.Sprintf("prompt_%s_%s.txt", projectID, taskID)),
		filepath.Join(stateDir, fmt.Sprintf("runner_%s_%s.sh", projectID, taskID)),
		filepath.Join(stateDir, fmt.Sprintf("output_%s_%s.log", projectID, taskID)),
	}

	for _, f := range files {
		if err := os.Remove(f); err != nil && !os.IsNotExist(err) {
			return fmt.Errorf("remove %s: %w", f, err)
		}
	}

	return nil
}
