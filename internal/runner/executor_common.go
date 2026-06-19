package runner

import (
	"fmt"
	"net/url"
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
		worktreePath, err := ensureWorktreeForTaskWithConfig(task, config, cmdFactory)
		if err != nil {
			return "", fmt.Errorf("ensure worktree: %w", err)
		}
		if worktreePath != "" {
			return worktreePath, nil
		}
		// ensureWorktree returned "" (no branch set, or is main/master) — fall through
		// only for tasks that did not request explicit worktree branch isolation.
		if task.ExecutionMode == "worktree" || task.GitBranch != "" || task.FeatureID != "" || task.GitRemote != "" {
			return "", fmt.Errorf("worktree mode requires a valid git repo context; set target_workdir/workdir to an existing git repo or provide git_remote with repo_cache_dir")
		}
	}

	// Fallback chain (both modes)
	if task.TargetWorkdir != "" {
		if _, err := os.Stat(task.TargetWorkdir); err == nil {
			return task.TargetWorkdir, nil
		}
	}
	if task.Workdir != "" {
		p := resolveTaskWorkdirPath(task.Workdir)
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
	return ensureWorktreeForTaskWithConfig(task, RunnerConfig{}, cmdFactory)
}

func ensureWorktreeForTaskWithConfig(task *types.ResolvedTask, config RunnerConfig, cmdFactory CommandFactory) (string, error) {
	// Guard: explicit current_branch mode
	if task.ExecutionMode == "current_branch" {
		return "", nil
	}
	branch := task.GitBranch
	if branch == "" {
		branch = task.FeatureID
	}
	// Guard: no branch specified
	if branch == "" {
		return resolveRepoContext(task, config, cmdFactory)
	}

	mainRepoPath, err := resolveRepoContext(task, config, cmdFactory)
	if err != nil {
		return "", err
	}
	if mainRepoPath == "" {
		// No repo context, can't create worktree
		return "", nil
	}

	// Skip separate worktrees for default branches, but keep the valid repo context.
	if branch == "main" || branch == "master" {
		return mainRepoPath, nil
	}

	// Check if branch is the current branch in the main repo
	cmd := cmdFactory("git", "-C", mainRepoPath, "branch", "--show-current")
	if out, err := cmd.Output(); err == nil {
		if strings.TrimSpace(string(out)) == branch {
			return mainRepoPath, nil // Branch is current in main repo, run there
		}
	}

	// Check if branch already checked out in an existing worktree
	cmd = cmdFactory("git", "-C", mainRepoPath, "worktree", "list", "--porcelain")
	if out, err := cmd.Output(); err == nil {
		if existingPath := findWorktreeForBranch(string(out), branch); existingPath != "" {
			return existingPath, nil
		}
	}

	// Compute worktree path: {mainRepo}/.worktrees/{sanitized-branch}
	sanitizedBranch := sanitizeBranchName(branch)
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
	checkCmd := cmdFactory("git", "-C", mainRepoPath, "rev-parse", "--verify", branch)
	branchExists := checkCmd.Run() == nil

	// Create worktree
	if branchExists {
		cmd = cmdFactory("git", "-C", mainRepoPath, "worktree", "add", worktreePath, branch)
	} else {
		// Get default branch
		defaultBranch := getDefaultBranch(cmdFactory, mainRepoPath)
		cmd = cmdFactory("git", "-C", mainRepoPath, "worktree", "add", "-b", branch, worktreePath, defaultBranch)
	}
	if out, err := cmd.CombinedOutput(); err != nil {
		return "", fmt.Errorf("git worktree add failed: %s: %w", strings.TrimSpace(string(out)), err)
	}

	return worktreePath, nil
}

func resolveRepoContext(task *types.ResolvedTask, config RunnerConfig, cmdFactory CommandFactory) (string, error) {
	if task.TargetWorkdir != "" {
		if isGitRepo(task.TargetWorkdir, cmdFactory) {
			return task.TargetWorkdir, nil
		}
	}

	if task.Workdir != "" {
		p := resolveTaskWorkdirPath(task.Workdir)
		if isGitRepo(p, cmdFactory) {
			return p, nil
		}
	}

	if task.GitRemote != "" {
		return ensureCachedRemoteRepo(task.GitRemote, config, cmdFactory)
	}

	return "", nil
}

func resolveTaskWorkdirPath(workdir string) string {
	if filepath.IsAbs(workdir) {
		return workdir
	}
	homeDir, _ := os.UserHomeDir()
	return filepath.Join(homeDir, workdir)
}

func isGitRepo(path string, cmdFactory CommandFactory) bool {
	if _, err := os.Stat(path); err != nil {
		return false
	}
	cmd := cmdFactory("git", "-C", path, "rev-parse", "--show-toplevel")
	return cmd.Run() == nil
}

func ensureCachedRemoteRepo(remote string, config RunnerConfig, cmdFactory CommandFactory) (string, error) {
	parsed, err := validateGitRemote(remote, config.RequireHTTPS)
	if err != nil {
		return "", err
	}

	token := config.GitToken
	if token == "" && config.GitTokenEnv != "" {
		token = os.Getenv(config.GitTokenEnv)
	}
	if token == "" && !config.AllowUnauthenticatedHTTPS {
		return "", fmt.Errorf("git token is required for HTTPS git_remote; set git_token/git_token_env or enable allow_unauthenticated_https")
	}
	if config.RepoCacheDir == "" {
		return "", fmt.Errorf("repo_cache_dir is required when git_remote is set")
	}
	if err := os.MkdirAll(config.RepoCacheDir, 0o755); err != nil {
		return "", fmt.Errorf("create repo cache dir: %w", err)
	}

	repoPath := filepath.Join(config.RepoCacheDir, cacheDirNameForRemote(parsed))
	if isGitRepo(repoPath, cmdFactory) {
		args := gitAuthArgs(token, "-C", repoPath, "fetch", "--prune", "origin")
		cmd := cmdFactory("git", args...)
		if out, err := cmd.CombinedOutput(); err != nil {
			return "", fmt.Errorf("git fetch failed for cached repo %s: %s: %w", repoPath, redactGitToken(strings.TrimSpace(string(out)), token), err)
		}
		return repoPath, nil
	}

	args := gitAuthArgs(token, "clone", remote, repoPath)
	cmd := cmdFactory("git", args...)
	if out, err := cmd.CombinedOutput(); err != nil {
		return "", fmt.Errorf("git clone failed for %s: %s: %w", remote, redactGitToken(strings.TrimSpace(string(out)), token), err)
	}
	return repoPath, nil
}

func redactGitToken(output, token string) string {
	redacted := strings.ReplaceAll(output, "Authorization: Bearer "+token, "<git token redacted>")
	if token != "" {
		redacted = strings.ReplaceAll(redacted, token, "<redacted>")
	}
	return redacted
}

func validateGitRemote(remote string, requireHTTPS bool) (*url.URL, error) {
	parsed, err := url.Parse(remote)
	if err != nil || parsed.Scheme == "" || parsed.Host == "" {
		return nil, fmt.Errorf("git_remote must be a valid HTTPS URL; SSH remotes are not allowed")
	}
	if parsed.Scheme == "ssh" || strings.HasPrefix(remote, "git@") {
		return nil, fmt.Errorf("git_remote must use HTTPS; SSH remotes are not allowed")
	}
	if parsed.User != nil {
		return nil, fmt.Errorf("git_remote must not contain embedded credentials")
	}
	if requireHTTPS && parsed.Scheme != "https" {
		return nil, fmt.Errorf("git_remote must use HTTPS when require_https is enabled")
	}
	if parsed.Scheme != "https" && parsed.Scheme != "http" {
		return nil, fmt.Errorf("git_remote must use HTTP or HTTPS")
	}
	return parsed, nil
}

func cacheDirNameForRemote(remote *url.URL) string {
	name := remote.Host + strings.TrimSuffix(remote.Path, ".git")
	name = strings.Trim(name, "/")
	return sanitizeBranchName(strings.ReplaceAll(name, "/", "-"))
}

func gitAuthArgs(token string, args ...string) []string {
	if token == "" {
		return args
	}
	withAuth := []string{"-c", "http.extraheader=Authorization: Bearer " + token}
	return append(withAuth, args...)
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

	taskContent := formatTaskContentForPrompt(task.Content)

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

Start now.%s`, task.Path, taskContent)
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

Start now.%s`, task.Path, taskContent)
}

func formatTaskContentForPrompt(content string) string {
	if content == "" {
		return ""
	}
	return fmt.Sprintf("\n\nTask content from Brain API:\n\n```markdown\n%s\n```", content)
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
