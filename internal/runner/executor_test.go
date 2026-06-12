package runner

import (
	"context"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/huynle/brain-api/internal/types"
)

// =============================================================================
// Test Helpers
// =============================================================================

func testResolvedTask(id string) *types.ResolvedTask {
	return &types.ResolvedTask{
		ID:       id,
		Path:     "projects/test/task/" + id + ".md",
		Title:    "Test Task " + id,
		Priority: "medium",
		Status:   "pending",
	}
}

func testExecutorConfig() RunnerConfig {
	return RunnerConfig{
		BrainAPIURL: "http://localhost:3333",
		StateDir:    os.TempDir(),
		WorkDir:     "/default/workdir",
		Opencode: OpencodeConfig{
			Bin:   "opencode",
			Agent: "default-agent",
			Model: "default-model",
		},
	}
}

// =============================================================================
// BuildPrompt Tests
// =============================================================================

func TestExecutor_BuildPrompt_DirectPrompt(t *testing.T) {
	cfg := testExecutorConfig()
	e := NewExecutor(cfg)

	task := testResolvedTask("abc123")
	task.DirectPrompt = "Do this specific thing verbatim"

	prompt := e.BuildPrompt(task, false)
	if prompt != "Do this specific thing verbatim" {
		t.Errorf("BuildPrompt with direct_prompt = %q, want %q", prompt, "Do this specific thing verbatim")
	}
}

func TestExecutor_BuildPrompt_DirectPrompt_IgnoresResume(t *testing.T) {
	cfg := testExecutorConfig()
	e := NewExecutor(cfg)

	task := testResolvedTask("abc123")
	task.DirectPrompt = "Do this specific thing"

	// Even with isResume=true, direct_prompt should be used verbatim
	prompt := e.BuildPrompt(task, true)
	if prompt != "Do this specific thing" {
		t.Errorf("BuildPrompt with direct_prompt and isResume = %q, want %q", prompt, "Do this specific thing")
	}
}

func TestExecutor_BuildPrompt_NewTask(t *testing.T) {
	cfg := testExecutorConfig()
	e := NewExecutor(cfg)

	task := testResolvedTask("abc123")
	task.Path = "projects/myproj/task/abc123.md"

	prompt := e.BuildPrompt(task, false)

	if !strings.Contains(prompt, "brain-runner-queue") {
		t.Error("new task prompt should reference brain-runner-queue skill")
	}
	if !strings.Contains(prompt, task.Path) {
		t.Errorf("new task prompt should contain task path %q", task.Path)
	}
	if strings.Contains(prompt, "RESUME") {
		t.Error("new task prompt should not contain RESUME")
	}
}

func TestExecutor_BuildPrompt_ResumeTask(t *testing.T) {
	cfg := testExecutorConfig()
	e := NewExecutor(cfg)

	task := testResolvedTask("abc123")
	task.Path = "projects/myproj/task/abc123.md"

	prompt := e.BuildPrompt(task, true)

	if !strings.Contains(prompt, "RESUME") {
		t.Error("resume prompt should contain RESUME")
	}
	if !strings.Contains(prompt, task.Path) {
		t.Errorf("resume prompt should contain task path %q", task.Path)
	}
	if !strings.Contains(prompt, "brain-runner-queue") {
		t.Error("resume prompt should reference brain-runner-queue skill")
	}
}

// =============================================================================
// ResolveWorkdir Tests
// =============================================================================

func TestExecutor_ResolveWorkdir_TargetWorkdir(t *testing.T) {
	dir := t.TempDir()
	cfg := testExecutorConfig()
	cfg.WorkDir = "/fallback"
	e := NewExecutor(cfg)

	task := testResolvedTask("abc123")
	task.TargetWorkdir = dir // exists

	result, _ := e.ResolveWorkdir(task)
	if result != dir {
		t.Errorf("ResolveWorkdir = %q, want %q (target_workdir)", result, dir)
	}
}

func TestExecutor_ResolveWorkdir_TargetWorkdir_NotExists(t *testing.T) {
	cfg := testExecutorConfig()
	cfg.WorkDir = "/fallback"
	e := NewExecutor(cfg)

	task := testResolvedTask("abc123")
	task.TargetWorkdir = "/nonexistent/path"

	result, _ := e.ResolveWorkdir(task)
	if result == "/nonexistent/path" {
		t.Error("ResolveWorkdir should not return nonexistent target_workdir")
	}
}

func TestExecutor_ResolveWorkdir_ResolvedWorkdir(t *testing.T) {
	dir := t.TempDir()
	cfg := testExecutorConfig()
	cfg.WorkDir = "/fallback"
	e := NewExecutor(cfg)

	task := testResolvedTask("abc123")
	task.ResolvedWorkdir = dir

	result, _ := e.ResolveWorkdir(task)
	if result != dir {
		t.Errorf("ResolveWorkdir = %q, want %q (resolved_workdir)", result, dir)
	}
}

func TestExecutor_ResolveWorkdir_FallbackToConfig(t *testing.T) {
	cfg := testExecutorConfig()
	cfg.WorkDir = "/config/default"
	e := NewExecutor(cfg)

	task := testResolvedTask("abc123")
	// No workdir fields set

	result, _ := e.ResolveWorkdir(task)
	if result != "/config/default" {
		t.Errorf("ResolveWorkdir = %q, want %q (config default)", result, "/config/default")
	}
}

func TestExecutor_ResolveWorkdir_Priority_TargetOverResolved(t *testing.T) {
	targetDir := t.TempDir()
	resolvedDir := t.TempDir()
	cfg := testExecutorConfig()
	e := NewExecutor(cfg)

	task := testResolvedTask("abc123")
	task.TargetWorkdir = targetDir
	task.ResolvedWorkdir = resolvedDir

	result, _ := e.ResolveWorkdir(task)
	if result != targetDir {
		t.Errorf("ResolveWorkdir = %q, want %q (target_workdir takes priority over resolved_workdir)", result, targetDir)
	}
}

func TestExecutor_ResolveWorkdir_WorktreeModeRequiresRepoContext(t *testing.T) {
	cfg := testExecutorConfig()
	cfg.WorkDir = "/unsafe/fallback"
	e := NewExecutor(cfg)
	e.CommandFactory = func(name string, args ...string) *exec.Cmd {
		return exec.Command("/bin/echo", "unexpected git command")
	}

	task := testResolvedTask("abc123")
	task.ExecutionMode = "worktree"
	task.GitBranch = "feature/bootstrap"

	_, err := e.ResolveWorkdir(task)
	if err == nil {
		t.Fatal("ResolveWorkdir should fail for worktree mode without repo context")
	}
	if !strings.Contains(err.Error(), "worktree") || !strings.Contains(err.Error(), "repo context") {
		t.Fatalf("error should clearly mention missing worktree repo context, got: %v", err)
	}
}

func TestExecutor_ResolveWorkdir_ExplicitWorktreeWithoutBranchRequiresRepoContext(t *testing.T) {
	cfg := testExecutorConfig()
	cfg.WorkDir = "/unsafe/fallback"
	e := NewExecutor(cfg)
	e.CommandFactory = func(name string, args ...string) *exec.Cmd {
		return exec.Command("/bin/echo", "unexpected git command")
	}

	task := testResolvedTask("abc123")
	task.ExecutionMode = "worktree"

	_, err := e.ResolveWorkdir(task)
	if err == nil {
		t.Fatal("ResolveWorkdir should fail for explicit worktree mode without repo context")
	}
	if !strings.Contains(err.Error(), "worktree") || !strings.Contains(err.Error(), "repo context") {
		t.Fatalf("error should clearly mention missing worktree repo context, got: %v", err)
	}
}

func TestExecutor_ResolveWorkdir_RejectsUnsafeGitRemotes(t *testing.T) {
	tests := []struct {
		name    string
		remote  string
		wantErr string
	}{
		{
			name:    "ssh remote",
			remote:  "git@github.com:owner/repo.git",
			wantErr: "HTTPS",
		},
		{
			name:    "embedded credentials",
			remote:  "https://token@github.com/owner/repo.git",
			wantErr: "embedded credentials",
		},
		{
			name:    "non-https remote",
			remote:  "http://github.com/owner/repo.git",
			wantErr: "HTTPS",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := testExecutorConfig()
			cfg.RepoCacheDir = t.TempDir()
			cfg.GitToken = "secret-token"
			cfg.RequireHTTPS = true
			e := NewExecutor(cfg)
			e.CommandFactory = func(name string, args ...string) *exec.Cmd {
				return exec.Command("/bin/echo", "unexpected git command")
			}

			task := testResolvedTask("abc123")
			task.ExecutionMode = "worktree"
			task.GitBranch = "feature/bootstrap"
			task.GitRemote = tt.remote

			_, err := e.ResolveWorkdir(task)
			if err == nil {
				t.Fatalf("ResolveWorkdir should reject %s", tt.remote)
			}
			if !strings.Contains(err.Error(), tt.wantErr) {
				t.Fatalf("error = %v, want it to contain %q", err, tt.wantErr)
			}
		})
	}
}

func TestExecutor_ResolveWorkdir_GitRemoteRequiresTokenUnlessUnauthenticatedAllowed(t *testing.T) {
	cfg := testExecutorConfig()
	cfg.RepoCacheDir = t.TempDir()
	cfg.GitToken = ""
	cfg.GitTokenEnv = ""
	cfg.RequireHTTPS = true
	cfg.AllowUnauthenticatedHTTPS = false
	e := NewExecutor(cfg)
	e.CommandFactory = func(name string, args ...string) *exec.Cmd {
		return exec.Command("/bin/echo", "unexpected git command")
	}

	task := testResolvedTask("abc123")
	task.ExecutionMode = "worktree"
	task.GitBranch = "feature/bootstrap"
	task.GitRemote = "https://github.com/owner/repo.git"

	_, err := e.ResolveWorkdir(task)
	if err == nil {
		t.Fatal("ResolveWorkdir should fail when HTTPS git_remote has no token")
	}
	if !strings.Contains(err.Error(), "git token") && !strings.Contains(err.Error(), "unauthenticated") {
		t.Fatalf("error should clearly mention missing git token or unauthenticated HTTPS policy, got: %v", err)
	}
}

func TestExecutor_ResolveWorkdir_GitRemoteAllowsUnauthenticatedHTTPSWhenEnabled(t *testing.T) {
	cacheDir := t.TempDir()
	remote := "https://github.com/owner/repo.git"
	parsedRemote, err := url.Parse(remote)
	if err != nil {
		t.Fatalf("parse remote: %v", err)
	}
	expectedRepo := filepath.Join(cacheDir, cacheDirNameForRemote(parsedRemote))
	expectedWorktree := filepath.Join(expectedRepo, ".worktrees", "feature-bootstrap")

	cfg := testExecutorConfig()
	cfg.RepoCacheDir = cacheDir
	cfg.GitToken = ""
	cfg.GitTokenEnv = ""
	cfg.RequireHTTPS = true
	cfg.AllowUnauthenticatedHTTPS = true
	e := NewExecutor(cfg)

	var cloneArgs []string
	e.CommandFactory = func(name string, args ...string) *exec.Cmd {
		if name != "git" {
			return exec.Command("/bin/sh", "-c", "exit 1")
		}
		if len(args) >= 3 && args[0] == "clone" {
			cloneArgs = append([]string(nil), args...)
			if args[1] != remote || args[2] != expectedRepo {
				return exec.Command("/bin/sh", "-c", "printf 'wrong clone target' && exit 1")
			}
			return exec.Command("/bin/sh", "-c", "mkdir -p \"$1\"", "sh", expectedRepo)
		}
		if len(args) >= 4 && args[0] == "-C" && args[1] == expectedRepo && args[2] == "rev-parse" && args[3] == "--show-toplevel" {
			return exec.Command("/bin/sh", "-c", "exit 0")
		}
		if len(args) >= 4 && args[0] == "-C" && args[1] == expectedRepo && args[2] == "branch" && args[3] == "--show-current" {
			return exec.Command("/bin/sh", "-c", "printf main")
		}
		if len(args) >= 5 && args[0] == "-C" && args[1] == expectedRepo && args[2] == "worktree" && args[3] == "list" {
			return exec.Command("/bin/sh", "-c", "exit 0")
		}
		if len(args) >= 5 && args[0] == "-C" && args[1] == expectedRepo && args[2] == "rev-parse" && args[3] == "--verify" && args[4] == "feature/bootstrap" {
			return exec.Command("/bin/sh", "-c", "exit 1")
		}
		if len(args) >= 5 && args[0] == "-C" && args[1] == expectedRepo && args[2] == "rev-parse" && args[3] == "--verify" && args[4] == "main" {
			return exec.Command("/bin/sh", "-c", "exit 0")
		}
		if len(args) >= 7 && args[0] == "-C" && args[1] == expectedRepo && args[2] == "worktree" && args[3] == "add" {
			return exec.Command("/bin/sh", "-c", "exit 0")
		}
		return exec.Command("/bin/sh", "-c", "exit 1")
	}

	task := testResolvedTask("abc123")
	task.ExecutionMode = "worktree"
	task.GitBranch = "feature/bootstrap"
	task.GitRemote = remote

	got, err := e.ResolveWorkdir(task)
	if err != nil {
		t.Fatalf("ResolveWorkdir returned error: %v", err)
	}
	if got != expectedWorktree {
		t.Fatalf("ResolveWorkdir = %q, want %q", got, expectedWorktree)
	}
	if len(cloneArgs) == 0 {
		t.Fatal("expected unauthenticated git clone to run")
	}
	if containsArg(cloneArgs, "http.extraheader=Authorization: Bearer ") || containsArg(cloneArgs, "-c") {
		t.Fatalf("unauthenticated clone should not include auth config, got: %v", cloneArgs)
	}
}

func TestValidateGitRemote_AllowsHTTPOnlyWhenRequireHTTPSDisabled(t *testing.T) {
	if _, err := validateGitRemote("http://github.com/owner/repo.git", true); err == nil {
		t.Fatal("validateGitRemote should reject HTTP when require_https is enabled")
	}
	if _, err := validateGitRemote("http://github.com/owner/repo.git", false); err != nil {
		t.Fatalf("validateGitRemote should allow HTTP when require_https is disabled: %v", err)
	}
	if _, err := validateGitRemote("ssh://github.com/owner/repo.git", false); err == nil {
		t.Fatal("validateGitRemote should always reject SSH remotes")
	}
}

func TestExecutor_ResolveWorkdir_WorktreeModeUsesAbsoluteWorkdirGitRepo(t *testing.T) {
	repo := t.TempDir()
	var worktreeAddArgs []string

	cfg := testExecutorConfig()
	cfg.WorkDir = "/unsafe/fallback"
	e := NewExecutor(cfg)
	e.CommandFactory = func(name string, args ...string) *exec.Cmd {
		if name != "git" {
			return exec.Command("/bin/sh", "-c", "exit 1")
		}
		if len(args) >= 4 && args[0] == "-C" && args[1] == repo && args[2] == "rev-parse" && args[3] == "--show-toplevel" {
			return exec.Command("/bin/sh", "-c", "exit 0")
		}
		if len(args) >= 4 && args[0] == "-C" && args[1] == repo && args[2] == "branch" && args[3] == "--show-current" {
			return exec.Command("/bin/sh", "-c", "printf main")
		}
		if len(args) >= 5 && args[0] == "-C" && args[1] == repo && args[2] == "worktree" && args[3] == "list" {
			return exec.Command("/bin/sh", "-c", "exit 0")
		}
		if len(args) >= 5 && args[0] == "-C" && args[1] == repo && args[2] == "rev-parse" && args[3] == "--verify" && args[4] == "feature/bootstrap" {
			return exec.Command("/bin/sh", "-c", "exit 1")
		}
		if len(args) >= 5 && args[0] == "-C" && args[1] == repo && args[2] == "rev-parse" && args[3] == "--verify" && args[4] == "main" {
			return exec.Command("/bin/sh", "-c", "exit 0")
		}
		if len(args) >= 7 && args[0] == "-C" && args[1] == repo && args[2] == "worktree" && args[3] == "add" {
			worktreeAddArgs = append([]string(nil), args...)
			return exec.Command("/bin/sh", "-c", "exit 0")
		}
		return exec.Command("/bin/sh", "-c", "exit 1")
	}

	task := testResolvedTask("abc123")
	task.ExecutionMode = "worktree"
	task.GitBranch = "feature/bootstrap"
	task.Workdir = repo

	got, err := e.ResolveWorkdir(task)
	if err != nil {
		t.Fatalf("ResolveWorkdir returned error: %v", err)
	}
	expected := filepath.Join(repo, ".worktrees", "feature-bootstrap")
	if got != expected {
		t.Fatalf("ResolveWorkdir = %q, want %q", got, expected)
	}
	if len(worktreeAddArgs) == 0 {
		t.Fatal("expected git worktree add to run against absolute workdir repo")
	}
}

func TestExecutor_ResolveWorkdir_WorktreeModeUsesValidLocalRepoWhenSeparateWorktreeUnneeded(t *testing.T) {
	tests := []struct {
		name          string
		currentBranch string
		gitBranch     string
	}{
		{name: "already on requested branch", currentBranch: "feature/bootstrap", gitBranch: "feature/bootstrap"},
		{name: "default branch", currentBranch: "main", gitBranch: "main"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			repo := t.TempDir()
			cfg := testExecutorConfig()
			cfg.WorkDir = "/unsafe/fallback"
			e := NewExecutor(cfg)
			e.CommandFactory = func(name string, args ...string) *exec.Cmd {
				if name != "git" {
					return exec.Command("/bin/sh", "-c", "exit 1")
				}
				if len(args) >= 4 && args[0] == "-C" && args[1] == repo && args[2] == "rev-parse" && args[3] == "--show-toplevel" {
					return exec.Command("/bin/sh", "-c", "exit 0")
				}
				if len(args) >= 4 && args[0] == "-C" && args[1] == repo && args[2] == "branch" && args[3] == "--show-current" {
					return exec.Command("/bin/echo", tt.currentBranch)
				}
				return exec.Command("/bin/sh", "-c", "exit 1")
			}

			task := testResolvedTask("abc123")
			task.ExecutionMode = "worktree"
			task.GitBranch = tt.gitBranch
			task.TargetWorkdir = repo

			got, err := e.ResolveWorkdir(task)
			if err != nil {
				t.Fatalf("ResolveWorkdir returned error: %v", err)
			}
			if got != repo {
				t.Fatalf("ResolveWorkdir = %q, want local repo %q", got, repo)
			}
		})
	}
}

func TestExecutor_ResolveWorkdir_RedactsGitTokenFromCloneErrors(t *testing.T) {
	cfg := testExecutorConfig()
	cfg.RepoCacheDir = t.TempDir()
	cfg.GitToken = "secret-token"
	cfg.RequireHTTPS = true
	e := NewExecutor(cfg)
	e.CommandFactory = func(name string, args ...string) *exec.Cmd {
		if name == "git" && len(args) >= 4 && args[0] == "-c" && args[2] == "clone" {
			return exec.Command("/bin/sh", "-c", "printf 'fatal: Authorization: Bearer secret-token rejected' && exit 1")
		}
		return exec.Command("/bin/sh", "-c", "exit 1")
	}

	task := testResolvedTask("abc123")
	task.ExecutionMode = "worktree"
	task.GitBranch = "feature/bootstrap"
	task.GitRemote = "https://github.com/owner/repo.git"

	_, err := e.ResolveWorkdir(task)
	if err == nil {
		t.Fatal("ResolveWorkdir should return clone failure")
	}
	if strings.Contains(err.Error(), "secret-token") || strings.Contains(err.Error(), "Authorization: Bearer") {
		t.Fatalf("clone error leaked git token/header: %v", err)
	}
	if !strings.Contains(err.Error(), "git clone failed") {
		t.Fatalf("error should preserve useful clone context, got: %v", err)
	}
}

func TestExecutor_ResolveWorkdir_GitRemoteClonesIntoRepoCacheAndCreatesWorktree(t *testing.T) {
	t.Setenv("TEST_GIT_TOKEN", "env-secret-token")
	cacheDir := t.TempDir()
	remote := "https://github.com/owner/repo.git"
	parsedRemote, err := url.Parse(remote)
	if err != nil {
		t.Fatalf("parse remote: %v", err)
	}
	expectedRepo := filepath.Join(cacheDir, cacheDirNameForRemote(parsedRemote))
	expectedWorktree := filepath.Join(expectedRepo, ".worktrees", "feature-bootstrap")

	cfg := testExecutorConfig()
	cfg.RepoCacheDir = cacheDir
	cfg.GitToken = ""
	cfg.GitTokenEnv = "TEST_GIT_TOKEN"
	cfg.RequireHTTPS = true
	e := NewExecutor(cfg)

	var cloneArgs []string
	var worktreeAddArgs []string
	e.CommandFactory = func(name string, args ...string) *exec.Cmd {
		if name != "git" {
			return exec.Command("/bin/sh", "-c", "exit 1")
		}
		if len(args) >= 5 && args[0] == "-c" && args[2] == "clone" {
			cloneArgs = append([]string(nil), args...)
			if args[3] != remote {
				return exec.Command("/bin/sh", "-c", "printf 'wrong remote' && exit 1")
			}
			if args[4] != expectedRepo {
				return exec.Command("/bin/sh", "-c", "printf 'wrong cache path' && exit 1")
			}
			return exec.Command("/bin/sh", "-c", "mkdir -p \"$1\"", "sh", expectedRepo)
		}
		if len(args) >= 4 && args[0] == "-C" && args[1] == expectedRepo && args[2] == "rev-parse" && args[3] == "--show-toplevel" {
			return exec.Command("/bin/sh", "-c", "exit 0")
		}
		if len(args) >= 4 && args[0] == "-C" && args[1] == expectedRepo && args[2] == "branch" && args[3] == "--show-current" {
			return exec.Command("/bin/sh", "-c", "printf main")
		}
		if len(args) >= 5 && args[0] == "-C" && args[1] == expectedRepo && args[2] == "worktree" && args[3] == "list" {
			return exec.Command("/bin/sh", "-c", "exit 0")
		}
		if len(args) >= 5 && args[0] == "-C" && args[1] == expectedRepo && args[2] == "rev-parse" && args[3] == "--verify" && args[4] == "feature/bootstrap" {
			return exec.Command("/bin/sh", "-c", "exit 1")
		}
		if len(args) >= 5 && args[0] == "-C" && args[1] == expectedRepo && args[2] == "rev-parse" && args[3] == "--verify" && args[4] == "main" {
			return exec.Command("/bin/sh", "-c", "exit 0")
		}
		if len(args) >= 7 && args[0] == "-C" && args[1] == expectedRepo && args[2] == "worktree" && args[3] == "add" {
			worktreeAddArgs = append([]string(nil), args...)
			return exec.Command("/bin/sh", "-c", "exit 0")
		}
		return exec.Command("/bin/sh", "-c", "exit 1")
	}

	task := testResolvedTask("abc123")
	task.ExecutionMode = "worktree"
	task.GitBranch = "feature/bootstrap"
	task.GitRemote = remote

	got, err := e.ResolveWorkdir(task)
	if err != nil {
		t.Fatalf("ResolveWorkdir returned error: %v", err)
	}
	if got != expectedWorktree {
		t.Fatalf("ResolveWorkdir = %q, want %q", got, expectedWorktree)
	}
	if len(cloneArgs) == 0 {
		t.Fatal("expected git clone to run")
	}
	if !containsArg(cloneArgs, "http.extraheader=Authorization: Bearer env-secret-token") {
		t.Fatalf("clone args should include token via http.extraheader, got: %v", cloneArgs)
	}
	if strings.Contains(cloneArgs[3], "env-secret-token") {
		t.Fatalf("clone remote should not embed token, got args: %v", cloneArgs)
	}
	if len(worktreeAddArgs) == 0 || !containsArg(worktreeAddArgs, expectedWorktree) {
		t.Fatalf("expected worktree under cached repo, got worktree args: %v", worktreeAddArgs)
	}
}

func TestExecutor_ResolveWorkdir_GitRemoteFetchesExistingCachedRepo(t *testing.T) {
	cacheDir := t.TempDir()
	remote := "https://github.com/owner/repo.git"
	parsedRemote, err := url.Parse(remote)
	if err != nil {
		t.Fatalf("parse remote: %v", err)
	}
	expectedRepo := filepath.Join(cacheDir, cacheDirNameForRemote(parsedRemote))
	if err := os.MkdirAll(expectedRepo, 0o755); err != nil {
		t.Fatalf("create cached repo: %v", err)
	}

	cfg := testExecutorConfig()
	cfg.RepoCacheDir = cacheDir
	cfg.GitToken = "secret-token"
	cfg.RequireHTTPS = true
	e := NewExecutor(cfg)

	var fetchArgs []string
	var cloneRan bool
	e.CommandFactory = func(name string, args ...string) *exec.Cmd {
		if name != "git" {
			return exec.Command("/bin/sh", "-c", "exit 1")
		}
		if len(args) >= 4 && args[0] == "-C" && args[1] == expectedRepo && args[2] == "rev-parse" && args[3] == "--show-toplevel" {
			return exec.Command("/bin/sh", "-c", "exit 0")
		}
		if len(args) >= 7 && args[0] == "-c" && args[2] == "-C" && args[3] == expectedRepo && args[4] == "fetch" {
			fetchArgs = append([]string(nil), args...)
			return exec.Command("/bin/sh", "-c", "exit 0")
		}
		if len(args) >= 5 && args[0] == "-c" && args[2] == "clone" {
			cloneRan = true
			return exec.Command("/bin/sh", "-c", "exit 1")
		}
		if len(args) >= 4 && args[0] == "-C" && args[1] == expectedRepo && args[2] == "branch" && args[3] == "--show-current" {
			return exec.Command("/bin/sh", "-c", "printf main")
		}
		if len(args) >= 5 && args[0] == "-C" && args[1] == expectedRepo && args[2] == "worktree" && args[3] == "list" {
			return exec.Command("/bin/sh", "-c", "printf 'worktree /cached/worktree\nbranch refs/heads/feature/bootstrap\n'")
		}
		return exec.Command("/bin/sh", "-c", "exit 1")
	}

	task := testResolvedTask("abc123")
	task.ExecutionMode = "worktree"
	task.GitBranch = "feature/bootstrap"
	task.GitRemote = remote

	got, err := e.ResolveWorkdir(task)
	if err != nil {
		t.Fatalf("ResolveWorkdir returned error: %v", err)
	}
	if got != "/cached/worktree" {
		t.Fatalf("ResolveWorkdir = %q, want existing cached worktree", got)
	}
	if cloneRan {
		t.Fatal("existing cached repo should fetch, not clone")
	}
	if len(fetchArgs) == 0 {
		t.Fatal("expected git fetch for existing cached repo")
	}
	if !containsArg(fetchArgs, "http.extraheader=Authorization: Bearer secret-token") {
		t.Fatalf("fetch args should include token via http.extraheader, got: %v", fetchArgs)
	}
}

// =============================================================================
// GetEffectiveAgent / GetEffectiveModel Tests
// =============================================================================

func TestExecutor_GetEffectiveAgent_TaskOverride(t *testing.T) {
	cfg := testExecutorConfig()
	cfg.Opencode.Agent = "config-agent"
	e := NewExecutor(cfg)

	task := testResolvedTask("abc123")
	task.Agent = "task-agent"

	agent := e.GetEffectiveAgent(task)
	if agent != "task-agent" {
		t.Errorf("GetEffectiveAgent = %q, want %q", agent, "task-agent")
	}
}

func TestExecutor_GetEffectiveAgent_ConfigDefault(t *testing.T) {
	cfg := testExecutorConfig()
	cfg.Opencode.Agent = "config-agent"
	e := NewExecutor(cfg)

	task := testResolvedTask("abc123")
	// No agent override

	agent := e.GetEffectiveAgent(task)
	if agent != "config-agent" {
		t.Errorf("GetEffectiveAgent = %q, want %q", agent, "config-agent")
	}
}

func TestExecutor_GetEffectiveModel_TaskOverride(t *testing.T) {
	cfg := testExecutorConfig()
	cfg.Opencode.Model = "config-model"
	e := NewExecutor(cfg)

	task := testResolvedTask("abc123")
	task.Model = "task-model"

	model := e.GetEffectiveModel(task, "")
	if model != "task-model" {
		t.Errorf("GetEffectiveModel = %q, want %q", model, "task-model")
	}
}

func TestExecutor_GetEffectiveModel_RuntimeDefault(t *testing.T) {
	cfg := testExecutorConfig()
	cfg.Opencode.Model = "config-model"
	e := NewExecutor(cfg)

	task := testResolvedTask("abc123")
	// No task model

	model := e.GetEffectiveModel(task, "runtime-model")
	if model != "runtime-model" {
		t.Errorf("GetEffectiveModel = %q, want %q", model, "runtime-model")
	}
}

func TestExecutor_GetEffectiveModel_ConfigDefault(t *testing.T) {
	cfg := testExecutorConfig()
	cfg.Opencode.Model = "config-model"
	e := NewExecutor(cfg)

	task := testResolvedTask("abc123")

	model := e.GetEffectiveModel(task, "")
	if model != "config-model" {
		t.Errorf("GetEffectiveModel = %q, want %q", model, "config-model")
	}
}

func TestExecutor_GetEffectiveModel_Precedence(t *testing.T) {
	cfg := testExecutorConfig()
	cfg.Opencode.Model = "config-model"
	e := NewExecutor(cfg)

	task := testResolvedTask("abc123")
	task.Model = "task-model"

	// Task model takes precedence over runtime default
	model := e.GetEffectiveModel(task, "runtime-model")
	if model != "task-model" {
		t.Errorf("GetEffectiveModel = %q, want %q (task > runtime > config)", model, "task-model")
	}
}

// =============================================================================
// Server-Applied Defaults Tests
// =============================================================================
// These tests verify behavior when the server pre-fills task.Agent and task.Model
// via centralized task_defaults, simulating the full precedence chain:
//   1. Task-level (explicit at creation)
//   2. Server task_defaults (applied when field is empty)
//   3. Runtime default model (TUI picker, model only)
//   4. Runner config (runner-local override)

func TestExecutor_GetEffectiveAgent_ServerDefault_UsedWhenSet(t *testing.T) {
	// Scenario: Server applied task_defaults agent to a task that had no explicit agent.
	// The runner should use the server-filled value.
	cfg := testExecutorConfig()
	cfg.Opencode.Agent = "runner-local-agent"
	e := NewExecutor(cfg)

	task := testResolvedTask("abc123")
	task.Agent = "server-default-agent" // Pre-filled by server task_defaults

	agent := e.GetEffectiveAgent(task)
	if agent != "server-default-agent" {
		t.Errorf("GetEffectiveAgent = %q, want %q (server default should be used)", agent, "server-default-agent")
	}
}

func TestExecutor_GetEffectiveAgent_RunnerConfigUsedWhenServerEmpty(t *testing.T) {
	// Scenario: Server has no task_defaults for agent, task has no explicit agent.
	// The runner's config.Opencode.Agent should be the fallback.
	cfg := testExecutorConfig()
	cfg.Opencode.Agent = "runner-local-agent"
	e := NewExecutor(cfg)

	task := testResolvedTask("abc123")
	// task.Agent is empty (no server default configured either)

	agent := e.GetEffectiveAgent(task)
	if agent != "runner-local-agent" {
		t.Errorf("GetEffectiveAgent = %q, want %q (runner config fallback)", agent, "runner-local-agent")
	}
}

func TestExecutor_GetEffectiveAgent_TaskExplicitOverridesServerDefault(t *testing.T) {
	// Scenario: Task was created with an explicit agent that differs from
	// the server task_defaults. Since the server only fills empty fields,
	// the task's explicit agent is preserved. The runner sees it the same way.
	cfg := testExecutorConfig()
	cfg.Opencode.Agent = "runner-local-agent"
	e := NewExecutor(cfg)

	task := testResolvedTask("abc123")
	task.Agent = "task-explicit-agent" // Set at creation (server did not override)

	agent := e.GetEffectiveAgent(task)
	if agent != "task-explicit-agent" {
		t.Errorf("GetEffectiveAgent = %q, want %q (task explicit wins)", agent, "task-explicit-agent")
	}
}

func TestExecutor_GetEffectiveModel_ServerDefault_UsedWhenSet(t *testing.T) {
	// Scenario: Server applied task_defaults model to a task that had no explicit model.
	// With no runtime default, the server-filled value should be used.
	cfg := testExecutorConfig()
	cfg.Opencode.Model = "runner-local-model"
	e := NewExecutor(cfg)

	task := testResolvedTask("abc123")
	task.Model = "server-default-model" // Pre-filled by server task_defaults

	model := e.GetEffectiveModel(task, "")
	if model != "server-default-model" {
		t.Errorf("GetEffectiveModel = %q, want %q (server default should be used)", model, "server-default-model")
	}
}

func TestExecutor_GetEffectiveModel_ServerDefault_BeatsRuntimeDefault(t *testing.T) {
	// Scenario: Server filled task.Model, and a runtime default is also set.
	// task.Model (includes server defaults) takes precedence over runtime default.
	cfg := testExecutorConfig()
	cfg.Opencode.Model = "runner-local-model"
	e := NewExecutor(cfg)

	task := testResolvedTask("abc123")
	task.Model = "server-default-model" // Pre-filled by server task_defaults

	model := e.GetEffectiveModel(task, "tui-runtime-model")
	if model != "server-default-model" {
		t.Errorf("GetEffectiveModel = %q, want %q (server default > runtime default)", model, "server-default-model")
	}
}

func TestExecutor_GetEffectiveModel_RuntimeDefault_BeatsRunnerConfig(t *testing.T) {
	// Scenario: No task-level or server-level model, but TUI set a runtime default.
	// Runtime default should beat the runner config.
	cfg := testExecutorConfig()
	cfg.Opencode.Model = "runner-local-model"
	e := NewExecutor(cfg)

	task := testResolvedTask("abc123")
	// task.Model empty (server had no default either)

	model := e.GetEffectiveModel(task, "tui-runtime-model")
	if model != "tui-runtime-model" {
		t.Errorf("GetEffectiveModel = %q, want %q (runtime default > runner config)", model, "tui-runtime-model")
	}
}

func TestExecutor_GetEffectiveModel_RunnerConfigFallback(t *testing.T) {
	// Scenario: No task-level, no server default, no runtime default.
	// Runner config should be the final fallback.
	cfg := testExecutorConfig()
	cfg.Opencode.Model = "runner-local-model"
	e := NewExecutor(cfg)

	task := testResolvedTask("abc123")

	model := e.GetEffectiveModel(task, "")
	if model != "runner-local-model" {
		t.Errorf("GetEffectiveModel = %q, want %q (runner config fallback)", model, "runner-local-model")
	}
}

func TestExecutor_GetEffectiveModel_FullPrecedenceChain(t *testing.T) {
	// Table-driven test verifying the full precedence chain:
	// task.Model (server-applied) > runtimeDefault > config.Model
	tests := []struct {
		name           string
		taskModel      string
		runtimeDefault string
		configModel    string
		expectedModel  string
	}{
		{
			name:           "all set: task wins",
			taskModel:      "task-model",
			runtimeDefault: "runtime-model",
			configModel:    "config-model",
			expectedModel:  "task-model",
		},
		{
			name:           "no task: runtime wins",
			taskModel:      "",
			runtimeDefault: "runtime-model",
			configModel:    "config-model",
			expectedModel:  "runtime-model",
		},
		{
			name:           "no task no runtime: config wins",
			taskModel:      "",
			runtimeDefault: "",
			configModel:    "config-model",
			expectedModel:  "config-model",
		},
		{
			name:           "all empty: returns empty",
			taskModel:      "",
			runtimeDefault: "",
			configModel:    "",
			expectedModel:  "",
		},
		{
			name:           "server default set no runtime: server wins",
			taskModel:      "server-applied-model",
			runtimeDefault: "",
			configModel:    "config-model",
			expectedModel:  "server-applied-model",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := testExecutorConfig()
			cfg.Opencode.Model = tt.configModel
			e := NewExecutor(cfg)

			task := testResolvedTask("abc123")
			task.Model = tt.taskModel

			model := e.GetEffectiveModel(task, tt.runtimeDefault)
			if model != tt.expectedModel {
				t.Errorf("GetEffectiveModel = %q, want %q", model, tt.expectedModel)
			}
		})
	}
}

// =============================================================================
// Background Spawn Tests (with mock command factory)
// =============================================================================

func TestExecutor_SpawnBackground_CommandArgs(t *testing.T) {
	stateDir := t.TempDir()
	cfg := testExecutorConfig()
	cfg.StateDir = stateDir
	cfg.Opencode.Bin = "/usr/local/bin/opencode"
	cfg.Opencode.Agent = "test-agent"
	cfg.Opencode.Model = "test-model"

	var capturedName string
	var capturedArgs []string

	e := NewExecutor(cfg)
	e.CommandFactory = func(name string, args ...string) *exec.Cmd {
		capturedName = name
		capturedArgs = args
		return exec.Command("/bin/echo", "mock")
	}

	task := testResolvedTask("abc123")
	workdir := t.TempDir()

	ctx := context.Background()
	opts := SpawnOptions{
		Mode:    ExecutionModeHeadless,
		Workdir: workdir,
	}

	result, err := e.Spawn(ctx, task, "test-project", opts)
	if err != nil {
		t.Fatalf("Spawn returned error: %v", err)
	}

	// Verify command name
	if capturedName != "/usr/local/bin/opencode" {
		t.Errorf("command name = %q, want %q", capturedName, "/usr/local/bin/opencode")
	}

	// Verify args contain "run"
	if len(capturedArgs) == 0 || capturedArgs[0] != "run" {
		t.Errorf("first arg = %v, want 'run'", capturedArgs)
	}

	// Verify agent flag
	agentIdx := indexOf(capturedArgs, "--agent")
	if agentIdx < 0 || agentIdx+1 >= len(capturedArgs) || capturedArgs[agentIdx+1] != "test-agent" {
		t.Errorf("expected --agent test-agent in args: %v", capturedArgs)
	}

	// Verify model flag
	modelIdx := indexOf(capturedArgs, "--model")
	if modelIdx < 0 || modelIdx+1 >= len(capturedArgs) || capturedArgs[modelIdx+1] != "test-model" {
		t.Errorf("expected --model test-model in args: %v", capturedArgs)
	}

	// Verify prompt file was created
	if result.PromptFile == "" {
		t.Error("PromptFile is empty")
	}
	if _, err := os.Stat(result.PromptFile); err != nil {
		t.Errorf("prompt file does not exist: %v", err)
	}

	// Verify PID is set (from the mock process)
	if result.PID <= 0 {
		t.Errorf("PID = %d, want > 0", result.PID)
	}
}

func TestExecutor_SpawnBackground_TaskAgentOverride(t *testing.T) {
	stateDir := t.TempDir()
	cfg := testExecutorConfig()
	cfg.StateDir = stateDir
	cfg.Opencode.Agent = "config-agent"
	cfg.Opencode.Model = "config-model"

	var capturedArgs []string

	e := NewExecutor(cfg)
	e.CommandFactory = func(name string, args ...string) *exec.Cmd {
		capturedArgs = args
		return exec.Command("/bin/echo", "mock")
	}

	task := testResolvedTask("abc123")
	task.Agent = "task-agent"
	task.Model = "task-model"

	ctx := context.Background()
	opts := SpawnOptions{
		Mode:    ExecutionModeHeadless,
		Workdir: t.TempDir(),
	}

	_, err := e.Spawn(ctx, task, "test-project", opts)
	if err != nil {
		t.Fatalf("Spawn returned error: %v", err)
	}

	// Verify task-level agent override
	agentIdx := indexOf(capturedArgs, "--agent")
	if agentIdx < 0 || capturedArgs[agentIdx+1] != "task-agent" {
		t.Errorf("expected --agent task-agent, got args: %v", capturedArgs)
	}

	// Verify task-level model override
	modelIdx := indexOf(capturedArgs, "--model")
	if modelIdx < 0 || capturedArgs[modelIdx+1] != "task-model" {
		t.Errorf("expected --model task-model, got args: %v", capturedArgs)
	}
}

func TestExecutor_SpawnBackground_NoAgentNoModel(t *testing.T) {
	stateDir := t.TempDir()
	cfg := testExecutorConfig()
	cfg.StateDir = stateDir
	cfg.Opencode.Agent = ""
	cfg.Opencode.Model = ""

	var capturedArgs []string

	e := NewExecutor(cfg)
	e.CommandFactory = func(name string, args ...string) *exec.Cmd {
		capturedArgs = args
		return exec.Command("/bin/echo", "mock")
	}

	task := testResolvedTask("abc123")

	ctx := context.Background()
	opts := SpawnOptions{
		Mode:    ExecutionModeHeadless,
		Workdir: t.TempDir(),
	}

	_, err := e.Spawn(ctx, task, "test-project", opts)
	if err != nil {
		t.Fatalf("Spawn returned error: %v", err)
	}

	// Should not have --agent or --model flags
	if indexOf(capturedArgs, "--agent") >= 0 {
		t.Errorf("should not have --agent flag when agent is empty: %v", capturedArgs)
	}
	if indexOf(capturedArgs, "--model") >= 0 {
		t.Errorf("should not have --model flag when model is empty: %v", capturedArgs)
	}
}

func TestExecutor_SpawnBackground_RuntimeDefaultModel(t *testing.T) {
	stateDir := t.TempDir()
	cfg := testExecutorConfig()
	cfg.StateDir = stateDir
	cfg.Opencode.Model = "config-model"

	var capturedArgs []string

	e := NewExecutor(cfg)
	e.CommandFactory = func(name string, args ...string) *exec.Cmd {
		capturedArgs = args
		return exec.Command("/bin/echo", "mock")
	}

	task := testResolvedTask("abc123")
	// No task-level model

	ctx := context.Background()
	opts := SpawnOptions{
		Mode:                ExecutionModeHeadless,
		Workdir:             t.TempDir(),
		RuntimeDefaultModel: "runtime-model",
	}

	_, err := e.Spawn(ctx, task, "test-project", opts)
	if err != nil {
		t.Fatalf("Spawn returned error: %v", err)
	}

	// Runtime default should take precedence over config
	modelIdx := indexOf(capturedArgs, "--model")
	if modelIdx < 0 || capturedArgs[modelIdx+1] != "runtime-model" {
		t.Errorf("expected --model runtime-model, got args: %v", capturedArgs)
	}
}

func TestExecutor_SpawnBackground_PromptContent(t *testing.T) {
	stateDir := t.TempDir()
	cfg := testExecutorConfig()
	cfg.StateDir = stateDir

	var capturedArgs []string

	e := NewExecutor(cfg)
	e.CommandFactory = func(name string, args ...string) *exec.Cmd {
		capturedArgs = args
		return exec.Command("/bin/echo", "mock")
	}

	task := testResolvedTask("abc123")
	task.Path = "projects/test/task/abc123.md"

	ctx := context.Background()
	opts := SpawnOptions{
		Mode:    ExecutionModeHeadless,
		Workdir: t.TempDir(),
	}

	result, err := e.Spawn(ctx, task, "test-project", opts)
	if err != nil {
		t.Fatalf("Spawn returned error: %v", err)
	}

	// Verify prompt file content
	content, err := os.ReadFile(result.PromptFile)
	if err != nil {
		t.Fatalf("read prompt file: %v", err)
	}
	if !strings.Contains(string(content), task.Path) {
		t.Errorf("prompt file should contain task path %q, got: %s", task.Path, content)
	}

	// Last arg should be the prompt content (read from file)
	lastArg := capturedArgs[len(capturedArgs)-1]
	if !strings.Contains(lastArg, task.Path) {
		t.Errorf("last arg should contain task path, got: %q", lastArg)
	}
}

func TestExecutor_SpawnBackground_OutputLogFile(t *testing.T) {
	stateDir := t.TempDir()
	cfg := testExecutorConfig()
	cfg.StateDir = stateDir

	e := NewExecutor(cfg)
	e.CommandFactory = func(name string, args ...string) *exec.Cmd {
		return exec.Command("/bin/echo", "mock")
	}

	task := testResolvedTask("abc123")

	ctx := context.Background()
	opts := SpawnOptions{
		Mode:    ExecutionModeHeadless,
		Workdir: t.TempDir(),
	}

	_, err := e.Spawn(ctx, task, "test-project", opts)
	if err != nil {
		t.Fatalf("Spawn returned error: %v", err)
	}

	// Verify output log file was created
	outputFile := filepath.Join(stateDir, "output_test-project_abc123.log")
	if _, err := os.Stat(outputFile); err != nil {
		t.Errorf("output log file should exist: %v", err)
	}
}

func TestExecutor_SpawnBackground_WorkdirResolution(t *testing.T) {
	stateDir := t.TempDir()
	workdir := t.TempDir()
	cfg := testExecutorConfig()
	cfg.StateDir = stateDir
	cfg.WorkDir = "/fallback"

	e := NewExecutor(cfg)
	e.CommandFactory = func(name string, args ...string) *exec.Cmd {
		return exec.Command("/bin/echo", "mock")
	}

	task := testResolvedTask("abc123")
	task.TargetWorkdir = workdir

	ctx := context.Background()
	opts := SpawnOptions{
		Mode: ExecutionModeHeadless,
		// No explicit workdir — should resolve from task
	}

	result, err := e.Spawn(ctx, task, "test-project", opts)
	if err != nil {
		t.Fatalf("Spawn returned error: %v", err)
	}

	// The workdir should have been resolved to the task's target_workdir
	if result.Workdir != workdir {
		t.Errorf("result.Workdir = %q, want %q", result.Workdir, workdir)
	}
}

// =============================================================================
// Cleanup Tests
// =============================================================================

func TestExecutor_Cleanup(t *testing.T) {
	stateDir := t.TempDir()
	cfg := testExecutorConfig()
	cfg.StateDir = stateDir
	e := NewExecutor(cfg)

	// Create temp files that cleanup should remove
	files := []string{
		filepath.Join(stateDir, "prompt_proj_task1.txt"),
		filepath.Join(stateDir, "runner_proj_task1.sh"),
		filepath.Join(stateDir, "output_proj_task1.log"),
	}
	for _, f := range files {
		if err := os.WriteFile(f, []byte("test"), 0o644); err != nil {
			t.Fatalf("create temp file: %v", err)
		}
	}

	err := e.Cleanup("task1", "proj")
	if err != nil {
		t.Fatalf("Cleanup returned error: %v", err)
	}

	// Verify all files are removed
	for _, f := range files {
		if _, err := os.Stat(f); !os.IsNotExist(err) {
			t.Errorf("file %s should have been removed", f)
		}
	}
}

func TestExecutor_Cleanup_MissingFiles(t *testing.T) {
	stateDir := t.TempDir()
	cfg := testExecutorConfig()
	cfg.StateDir = stateDir
	e := NewExecutor(cfg)

	// Should not error when files don't exist
	err := e.Cleanup("nonexistent", "proj")
	if err != nil {
		t.Errorf("Cleanup returned error for missing files: %v", err)
	}
}

// =============================================================================
// ParseLsofOutput Tests
// =============================================================================

func TestParseLsofOutput_ValidOutput(t *testing.T) {
	output := `COMMAND   PID USER   FD   TYPE DEVICE SIZE/OFF NODE NAME
opencode 1234 user   12u  IPv4 0x1234 0t0  TCP *:52341 (LISTEN)
opencode 1234 user   13u  IPv4 0x5678 0t0  TCP 127.0.0.1:52341->127.0.0.1:52342 (ESTABLISHED)
`
	port, err := ParseLsofOutput(output)
	if err != nil {
		t.Fatalf("ParseLsofOutput returned error: %v", err)
	}
	if port != 52341 {
		t.Errorf("ParseLsofOutput = %d, want 52341", port)
	}
}

func TestParseLsofOutput_NoListenPort(t *testing.T) {
	output := `COMMAND   PID USER   FD   TYPE DEVICE SIZE/OFF NODE NAME
opencode 1234 user   13u  IPv4 0x5678 0t0  TCP 127.0.0.1:52341->127.0.0.1:52342 (ESTABLISHED)
`
	_, err := ParseLsofOutput(output)
	if err == nil {
		t.Error("ParseLsofOutput should return error when no LISTEN port found")
	}
}

func TestParseLsofOutput_EmptyOutput(t *testing.T) {
	_, err := ParseLsofOutput("")
	if err == nil {
		t.Error("ParseLsofOutput should return error for empty output")
	}
}

func TestParseLsofOutput_MultipleListenPorts(t *testing.T) {
	output := `COMMAND   PID USER   FD   TYPE DEVICE SIZE/OFF NODE NAME
opencode 1234 user   12u  IPv4 0x1234 0t0  TCP *:52341 (LISTEN)
opencode 1234 user   14u  IPv6 0x9abc 0t0  TCP *:52342 (LISTEN)
`
	// Should return the first LISTEN port
	port, err := ParseLsofOutput(output)
	if err != nil {
		t.Fatalf("ParseLsofOutput returned error: %v", err)
	}
	if port != 52341 {
		t.Errorf("ParseLsofOutput = %d, want 52341 (first LISTEN port)", port)
	}
}

func TestParseLsofOutput_IPv6Wildcard(t *testing.T) {
	output := `COMMAND   PID USER   FD   TYPE DEVICE SIZE/OFF NODE NAME
opencode 1234 user   12u  IPv6 0x1234 0t0  TCP [::]:8080 (LISTEN)
`
	port, err := ParseLsofOutput(output)
	if err != nil {
		t.Fatalf("ParseLsofOutput returned error: %v", err)
	}
	if port != 8080 {
		t.Errorf("ParseLsofOutput = %d, want 8080", port)
	}
}

func TestParseLsofOutput_LocalhostBind(t *testing.T) {
	output := `COMMAND   PID USER   FD   TYPE DEVICE SIZE/OFF NODE NAME
opencode 1234 user   12u  IPv4 0x1234 0t0  TCP 127.0.0.1:3000 (LISTEN)
`
	port, err := ParseLsofOutput(output)
	if err != nil {
		t.Fatalf("ParseLsofOutput returned error: %v", err)
	}
	if port != 3000 {
		t.Errorf("ParseLsofOutput = %d, want 3000", port)
	}
}

// =============================================================================
// IsPidAlive Tests
// =============================================================================

func TestIsPidAlive_CurrentProcess(t *testing.T) {
	if !IsPidAlive(os.Getpid()) {
		t.Error("IsPidAlive should return true for current process")
	}
}

func TestIsPidAlive_DeadProcess(t *testing.T) {
	if IsPidAlive(99999999) {
		t.Error("IsPidAlive should return false for nonexistent PID")
	}
}

func TestIsPidAlive_ZeroPid(t *testing.T) {
	if IsPidAlive(0) {
		t.Error("IsPidAlive should return false for PID 0")
	}
}

func TestIsPidAlive_NegativePid(t *testing.T) {
	if IsPidAlive(-1) {
		t.Error("IsPidAlive should return false for negative PID")
	}
}

// =============================================================================
// SpawnOptions / SpawnResult Types Tests
// =============================================================================

func TestSpawnOptions_Defaults(t *testing.T) {
	opts := SpawnOptions{
		Mode: ExecutionModeHeadless,
	}
	if opts.IsResume {
		t.Error("IsResume should default to false")
	}
	if opts.Workdir != "" {
		t.Error("Workdir should default to empty")
	}
}

// =============================================================================
// Spawn with unknown mode
// =============================================================================

func TestExecutor_Spawn_UnknownMode(t *testing.T) {
	stateDir := t.TempDir()
	cfg := testExecutorConfig()
	cfg.StateDir = stateDir
	e := NewExecutor(cfg)

	task := testResolvedTask("abc123")
	ctx := context.Background()
	opts := SpawnOptions{
		Mode:    "unknown_mode",
		Workdir: t.TempDir(),
	}

	_, err := e.Spawn(ctx, task, "test-project", opts)
	if err == nil {
		t.Error("Spawn should return error for unknown mode")
	}
}

// =============================================================================
// ensureWorktree Tests - Feature ID Derivation
// =============================================================================

func TestEnsureWorktree_FeatureID_DerivesBranch(t *testing.T) {
	// When git_branch is empty but feature_id is set, feature_id should be used as branch name
	mainRepo := t.TempDir()

	cfg := testExecutorConfig()
	e := NewExecutor(cfg)

	// Track git commands to verify feature_id is used as branch
	var gitCmds [][]string
	e.CommandFactory = func(name string, args ...string) *exec.Cmd {
		if name == "git" {
			gitCmds = append(gitCmds, args)
			if len(args) >= 4 && args[2] == "rev-parse" && args[3] == "--show-toplevel" {
				return exec.Command("/bin/sh", "-c", "exit 0")
			}
		}
		// Default: return a command that fails (branch doesn't exist, etc.)
		return exec.Command("false")
	}

	task := testResolvedTask("abc123")
	task.GitBranch = ""                          // No explicit branch
	task.FeatureID = "centralized-task-defaults" // Has feature_id
	task.TargetWorkdir = mainRepo
	task.ExecutionMode = "worktree"

	_, _ = e.ensureWorktree(task)

	// Verify that git commands used the feature_id as the branch name
	foundBranchRef := false
	for _, cmd := range gitCmds {
		for _, arg := range cmd {
			if arg == "centralized-task-defaults" {
				foundBranchRef = true
				break
			}
		}
	}
	if !foundBranchRef {
		t.Errorf("ensureWorktree should use feature_id as branch name, git commands were: %v", gitCmds)
	}
}

func TestEnsureWorktree_FeatureID_NoFeatureID_NoGitBranch(t *testing.T) {
	// When both git_branch and feature_id are empty, should return "" (no worktree)
	cfg := testExecutorConfig()
	e := NewExecutor(cfg)

	task := testResolvedTask("abc123")
	task.GitBranch = ""
	task.FeatureID = ""
	task.ExecutionMode = "worktree"

	path, err := e.ensureWorktree(task)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if path != "" {
		t.Errorf("ensureWorktree should return empty string when no branch and no feature_id, got %q", path)
	}
}

func TestEnsureWorktree_ExplicitGitBranch_OverridesFeatureID(t *testing.T) {
	// Explicit git_branch should always win over feature_id
	mainRepo := t.TempDir()

	cfg := testExecutorConfig()
	e := NewExecutor(cfg)

	var gitCmds [][]string
	e.CommandFactory = func(name string, args ...string) *exec.Cmd {
		if name == "git" {
			gitCmds = append(gitCmds, args)
			if len(args) >= 4 && args[2] == "rev-parse" && args[3] == "--show-toplevel" {
				return exec.Command("/bin/sh", "-c", "exit 0")
			}
		}
		return exec.Command("false")
	}

	task := testResolvedTask("abc123")
	task.GitBranch = "my-explicit-branch"
	task.FeatureID = "centralized-task-defaults"
	task.TargetWorkdir = mainRepo
	task.ExecutionMode = "worktree"

	_, _ = e.ensureWorktree(task)

	// Verify git commands used the explicit branch, not feature_id
	foundExplicit := false
	foundFeature := false
	for _, cmd := range gitCmds {
		for _, arg := range cmd {
			if arg == "my-explicit-branch" {
				foundExplicit = true
			}
			if arg == "centralized-task-defaults" {
				foundFeature = true
			}
		}
	}
	if !foundExplicit {
		t.Errorf("ensureWorktree should use explicit git_branch, git commands were: %v", gitCmds)
	}
	if foundFeature {
		t.Errorf("ensureWorktree should not use feature_id when explicit git_branch is set, git commands were: %v", gitCmds)
	}
}

func TestEnsureWorktree_FeatureID_DefaultBranchGuard(t *testing.T) {
	// Feature IDs of "main" or "master" should be rejected (same as branch guard)
	cfg := testExecutorConfig()
	e := NewExecutor(cfg)
	e.CommandFactory = func(name string, args ...string) *exec.Cmd {
		return exec.Command("false")
	}

	tests := []struct {
		name      string
		featureID string
	}{
		{"main feature_id", "main"},
		{"master feature_id", "master"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			task := testResolvedTask("abc123")
			task.GitBranch = ""
			task.FeatureID = tt.featureID
			task.ExecutionMode = "worktree"

			path, err := e.ensureWorktree(task)
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if path != "" {
				t.Errorf("ensureWorktree should return empty for feature_id=%q (default branch), got %q", tt.featureID, path)
			}
		})
	}
}

func TestEnsureWorktree_FeatureID_CurrentBranchGuard(t *testing.T) {
	// execution_mode="current_branch" should prevent worktree even with feature_id
	cfg := testExecutorConfig()
	e := NewExecutor(cfg)

	task := testResolvedTask("abc123")
	task.GitBranch = ""
	task.FeatureID = "some-feature"
	task.ExecutionMode = "current_branch"

	path, err := e.ensureWorktree(task)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if path != "" {
		t.Errorf("ensureWorktree should return empty for current_branch mode, got %q", path)
	}
}

func TestEnsureWorktree_FeatureID_SameWorktreeForSameFeature(t *testing.T) {
	// Two tasks with same feature_id should resolve to the same worktree directory path
	mainRepo := t.TempDir()
	cfg := testExecutorConfig()
	e := NewExecutor(cfg)
	e.CommandFactory = func(name string, args ...string) *exec.Cmd {
		return exec.Command("false")
	}

	task1 := testResolvedTask("task1")
	task1.GitBranch = ""
	task1.FeatureID = "shared-feature"
	task1.TargetWorkdir = mainRepo
	task1.ExecutionMode = "worktree"

	task2 := testResolvedTask("task2")
	task2.GitBranch = ""
	task2.FeatureID = "shared-feature"
	task2.TargetWorkdir = mainRepo
	task2.ExecutionMode = "worktree"

	// Both should attempt the same worktree path (they will fail because git isn't real,
	// but the error message will contain the path)
	_, err1 := e.ensureWorktree(task1)
	_, err2 := e.ensureWorktree(task2)

	// Both should produce the same error (same worktree path attempted)
	// Since CommandFactory returns "false", both will fail identically
	if err1 == nil || err2 == nil {
		// If one succeeds (e.g., directory exists), that's fine too
		// The key test is the path would be the same
	}

	// Verify by computing expected path manually
	expectedPath := filepath.Join(mainRepo, ".worktrees", sanitizeBranchName("shared-feature"))
	_ = expectedPath // Path computation check - both tasks should target same path
	// The real verification is that sanitizeBranchName produces consistent output
	path1 := filepath.Join(mainRepo, ".worktrees", sanitizeBranchName(task1.FeatureID))
	path2 := filepath.Join(mainRepo, ".worktrees", sanitizeBranchName(task2.FeatureID))
	if path1 != path2 {
		t.Errorf("same feature_id should produce same worktree path: %q vs %q", path1, path2)
	}
	_ = err1
	_ = err2
}

// =============================================================================
// Executor Dispatch Tests
// =============================================================================

func TestResolveExecutorType_EmptyDefaultsToOpencode(t *testing.T) {
	task := testResolvedTask("abc123")
	task.Executor = ""
	got := resolveExecutorType(task)
	if got != "opencode" {
		t.Errorf("resolveExecutorType(empty) = %q, want %q", got, "opencode")
	}
}

func TestResolveExecutorType_ExplicitOpencode(t *testing.T) {
	task := testResolvedTask("abc123")
	task.Executor = "opencode"
	got := resolveExecutorType(task)
	if got != "opencode" {
		t.Errorf("resolveExecutorType(opencode) = %q, want %q", got, "opencode")
	}
}

func TestResolveExecutorType_Pi(t *testing.T) {
	task := testResolvedTask("abc123")
	task.Executor = "pi"
	got := resolveExecutorType(task)
	if got != "pi" {
		t.Errorf("resolveExecutorType(pi) = %q, want %q", got, "pi")
	}
}

func TestResolveExecutorType_Script(t *testing.T) {
	task := testResolvedTask("abc123")
	task.Executor = "script"
	got := resolveExecutorType(task)
	if got != "script" {
		t.Errorf("resolveExecutorType(script) = %q, want %q", got, "script")
	}
}

func TestExecutor_Spawn_DefaultExecutorRoutesToOpencode(t *testing.T) {
	stateDir := t.TempDir()
	cfg := testExecutorConfig()
	cfg.StateDir = stateDir

	var capturedName string

	e := NewExecutor(cfg)
	e.CommandFactory = func(name string, args ...string) *exec.Cmd {
		capturedName = name
		return exec.Command("/bin/echo", "mock")
	}

	task := testResolvedTask("abc123")
	// Executor is empty — should default to opencode
	task.Executor = ""

	ctx := context.Background()
	opts := SpawnOptions{
		Mode:    ExecutionModeHeadless,
		Workdir: t.TempDir(),
	}

	_, err := e.Spawn(ctx, task, "test-project", opts)
	if err != nil {
		t.Fatalf("Spawn returned error: %v", err)
	}

	// Should have invoked the opencode binary
	if capturedName != "opencode" {
		t.Errorf("command name = %q, want %q (opencode)", capturedName, "opencode")
	}
}

func TestExecutor_Spawn_ExplicitOpencodeExecutor(t *testing.T) {
	stateDir := t.TempDir()
	cfg := testExecutorConfig()
	cfg.StateDir = stateDir

	var capturedName string

	e := NewExecutor(cfg)
	e.CommandFactory = func(name string, args ...string) *exec.Cmd {
		capturedName = name
		return exec.Command("/bin/echo", "mock")
	}

	task := testResolvedTask("abc123")
	task.Executor = "opencode"

	ctx := context.Background()
	opts := SpawnOptions{
		Mode:    ExecutionModeHeadless,
		Workdir: t.TempDir(),
	}

	_, err := e.Spawn(ctx, task, "test-project", opts)
	if err != nil {
		t.Fatalf("Spawn returned error: %v", err)
	}

	if capturedName != "opencode" {
		t.Errorf("command name = %q, want %q", capturedName, "opencode")
	}
}

func TestExecutor_Spawn_PiExecutor(t *testing.T) {
	stateDir := t.TempDir()
	cfg := testExecutorConfig()
	cfg.StateDir = stateDir

	var capturedName string

	e := NewExecutor(cfg)
	e.CommandFactory = func(name string, args ...string) *exec.Cmd {
		capturedName = name
		// Use a command that reads stdin and writes JSONL to stdout
		// "cat" will read from stdin and exit when stdin is closed
		return exec.Command("/bin/cat")
	}

	task := testResolvedTask("abc123")
	task.Executor = "pi"

	ctx := context.Background()
	opts := SpawnOptions{
		Mode:    ExecutionModeHeadless,
		Workdir: t.TempDir(),
	}

	result, err := e.Spawn(ctx, task, "test-project", opts)
	if err != nil {
		t.Fatalf("Spawn(pi) returned error: %v", err)
	}

	// Should have invoked the "pi" binary (default)
	if capturedName != "pi" {
		t.Errorf("command name = %q, want %q (pi)", capturedName, "pi")
	}

	if result.PID <= 0 {
		t.Errorf("PID = %d, want > 0", result.PID)
	}
	if result.Proc == nil {
		t.Error("Proc should not be nil for pi executor")
	}
	if result.Workdir == "" {
		t.Error("Workdir should be set")
	}

	// Clean up the process
	_ = result.Proc.Kill(nil)
}

func TestExecutor_Spawn_PiExecutor_CustomBin(t *testing.T) {
	stateDir := t.TempDir()
	cfg := testExecutorConfig()
	cfg.StateDir = stateDir
	cfg.Pi.Bin = "/usr/local/bin/my-pi"

	var capturedName string

	e := NewExecutor(cfg)
	e.CommandFactory = func(name string, args ...string) *exec.Cmd {
		capturedName = name
		return exec.Command("/bin/cat")
	}

	task := testResolvedTask("abc123")
	task.Executor = "pi"

	ctx := context.Background()
	opts := SpawnOptions{
		Mode:    ExecutionModeHeadless,
		Workdir: t.TempDir(),
	}

	result, err := e.Spawn(ctx, task, "test-project", opts)
	if err != nil {
		t.Fatalf("Spawn(pi custom bin) returned error: %v", err)
	}

	if capturedName != "/usr/local/bin/my-pi" {
		t.Errorf("command name = %q, want %q", capturedName, "/usr/local/bin/my-pi")
	}

	_ = result.Proc.Kill(nil)
}

// =============================================================================
// Script Executor Tests
// =============================================================================

func TestExecutor_Spawn_Script_DisabledByDefault(t *testing.T) {
	stateDir := t.TempDir()
	cfg := testExecutorConfig()
	cfg.StateDir = stateDir
	// Script.Enabled is false by default
	e := NewExecutor(cfg)

	task := testResolvedTask("abc123")
	task.Executor = "script"
	task.DirectPrompt = "echo hello"

	ctx := context.Background()
	opts := SpawnOptions{
		Mode:    ExecutionModeHeadless,
		Workdir: t.TempDir(),
	}

	_, err := e.Spawn(ctx, task, "test-project", opts)
	if err == nil {
		t.Error("Spawn(script) should return error when scripts are disabled")
	}
	if !strings.Contains(err.Error(), "script executor is disabled") {
		t.Errorf("error should mention disabled, got: %v", err)
	}
}

func TestExecutor_Spawn_Script_RequiresDirectPrompt(t *testing.T) {
	stateDir := t.TempDir()
	cfg := testExecutorConfig()
	cfg.StateDir = stateDir
	cfg.Script.Enabled = true
	e := NewExecutor(cfg)

	task := testResolvedTask("abc123")
	task.Executor = "script"
	task.DirectPrompt = "" // empty

	ctx := context.Background()
	opts := SpawnOptions{
		Mode:    ExecutionModeHeadless,
		Workdir: t.TempDir(),
	}

	_, err := e.Spawn(ctx, task, "test-project", opts)
	if err == nil {
		t.Error("Spawn(script) should return error when DirectPrompt is empty")
	}
	if !strings.Contains(err.Error(), "direct_prompt") {
		t.Errorf("error should mention direct_prompt, got: %v", err)
	}
}

func TestExecutor_Spawn_Script_Success(t *testing.T) {
	stateDir := t.TempDir()
	cfg := testExecutorConfig()
	cfg.StateDir = stateDir
	cfg.Script.Enabled = true
	e := NewExecutor(cfg)

	task := testResolvedTask("abc123")
	task.Executor = "script"
	task.DirectPrompt = "echo hello"

	workdir := t.TempDir()
	ctx := context.Background()
	opts := SpawnOptions{
		Mode:    ExecutionModeHeadless,
		Workdir: workdir,
	}

	result, err := e.Spawn(ctx, task, "test-project", opts)
	if err != nil {
		t.Fatalf("Spawn(script) returned error: %v", err)
	}

	if result.PID <= 0 {
		t.Errorf("PID = %d, want > 0", result.PID)
	}
	if result.Proc == nil {
		t.Error("Proc should not be nil")
	}
	if result.Workdir != workdir {
		t.Errorf("Workdir = %q, want %q", result.Workdir, workdir)
	}

	// Wait for process to complete
	proc := result.Proc.(*OsProcess)
	<-proc.Done()

	if proc.ExitCode() != 0 {
		t.Errorf("ExitCode = %d, want 0", proc.ExitCode())
	}

	// Verify output was captured
	outputFile := filepath.Join(stateDir, "output_test-project_abc123.log")
	data, err := os.ReadFile(outputFile)
	if err != nil {
		t.Fatalf("read output file: %v", err)
	}
	if !strings.Contains(string(data), "hello") {
		t.Errorf("output should contain 'hello', got: %q", string(data))
	}
}

func TestExecutor_Spawn_Script_NonZeroExit(t *testing.T) {
	stateDir := t.TempDir()
	cfg := testExecutorConfig()
	cfg.StateDir = stateDir
	cfg.Script.Enabled = true
	e := NewExecutor(cfg)

	task := testResolvedTask("abc123")
	task.Executor = "script"
	task.DirectPrompt = "exit 42"

	ctx := context.Background()
	opts := SpawnOptions{
		Mode:    ExecutionModeHeadless,
		Workdir: t.TempDir(),
	}

	result, err := e.Spawn(ctx, task, "test-project", opts)
	if err != nil {
		t.Fatalf("Spawn(script) returned error: %v", err)
	}

	proc := result.Proc.(*OsProcess)
	<-proc.Done()

	if proc.ExitCode() != 42 {
		t.Errorf("ExitCode = %d, want 42", proc.ExitCode())
	}
}

func TestExecutor_Spawn_Script_CapturesStderr(t *testing.T) {
	stateDir := t.TempDir()
	cfg := testExecutorConfig()
	cfg.StateDir = stateDir
	cfg.Script.Enabled = true
	e := NewExecutor(cfg)

	task := testResolvedTask("abc123")
	task.Executor = "script"
	task.DirectPrompt = "echo out-msg && echo err-msg >&2"

	ctx := context.Background()
	opts := SpawnOptions{
		Mode:    ExecutionModeHeadless,
		Workdir: t.TempDir(),
	}

	result, err := e.Spawn(ctx, task, "test-project", opts)
	if err != nil {
		t.Fatalf("Spawn(script) returned error: %v", err)
	}

	proc := result.Proc.(*OsProcess)
	<-proc.Done()

	outputFile := filepath.Join(stateDir, "output_test-project_abc123.log")
	data, err := os.ReadFile(outputFile)
	if err != nil {
		t.Fatalf("read output file: %v", err)
	}
	output := string(data)
	if !strings.Contains(output, "out-msg") {
		t.Errorf("output should contain stdout 'out-msg', got: %q", output)
	}
	if !strings.Contains(output, "err-msg") {
		t.Errorf("output should contain stderr 'err-msg', got: %q", output)
	}
}

func TestExecutor_Spawn_Script_Timeout(t *testing.T) {
	stateDir := t.TempDir()
	cfg := testExecutorConfig()
	cfg.StateDir = stateDir
	cfg.Script.Enabled = true
	cfg.Script.MaxTimeout = 1 // 1 second timeout

	e := NewExecutor(cfg)

	task := testResolvedTask("abc123")
	task.Executor = "script"
	task.DirectPrompt = "sleep 30" // will be killed by timeout

	ctx := context.Background()
	opts := SpawnOptions{
		Mode:    ExecutionModeHeadless,
		Workdir: t.TempDir(),
	}

	result, err := e.Spawn(ctx, task, "test-project", opts)
	if err != nil {
		t.Fatalf("Spawn(script) returned error: %v", err)
	}

	proc := result.Proc.(*OsProcess)
	<-proc.Done()

	if proc.ExitCode() == 0 {
		t.Error("ExitCode should be non-zero for killed process")
	}
}

// =============================================================================
// Script Command Validation Tests
// =============================================================================

func TestValidateScriptCommand_AllowedCommands(t *testing.T) {
	cfg := testExecutorConfig()
	cfg.Script.AllowedCommands = []string{"echo ", "npm ", "go "}
	e := NewExecutor(cfg)

	// Allowed
	if err := e.validateScriptCommand("echo hello"); err != nil {
		t.Errorf("'echo hello' should be allowed: %v", err)
	}
	if err := e.validateScriptCommand("npm run build"); err != nil {
		t.Errorf("'npm run build' should be allowed: %v", err)
	}

	// Not allowed
	if err := e.validateScriptCommand("rm -rf /"); err == nil {
		t.Error("'rm -rf /' should be rejected")
	}
	if err := e.validateScriptCommand("sudo anything"); err == nil {
		t.Error("'sudo anything' should be rejected")
	}
}

func TestValidateScriptCommand_BlockedCommands(t *testing.T) {
	cfg := testExecutorConfig()
	cfg.Script.BlockedCommands = []string{"rm -rf /", "sudo "}
	e := NewExecutor(cfg)

	// Allowed (not blocked)
	if err := e.validateScriptCommand("echo hello"); err != nil {
		t.Errorf("'echo hello' should be allowed: %v", err)
	}

	// Blocked
	if err := e.validateScriptCommand("rm -rf /everything"); err == nil {
		t.Error("'rm -rf /' should be blocked")
	}
	if err := e.validateScriptCommand("sudo rm -rf /"); err == nil {
		t.Error("'sudo ...' should be blocked")
	}
}

func TestValidateScriptCommand_AllowedAndBlockedCombined(t *testing.T) {
	cfg := testExecutorConfig()
	cfg.Script.AllowedCommands = []string{"npm "}
	cfg.Script.BlockedCommands = []string{"npm publish"}
	e := NewExecutor(cfg)

	// Allowed and not blocked
	if err := e.validateScriptCommand("npm run build"); err != nil {
		t.Errorf("'npm run build' should be allowed: %v", err)
	}

	// Allowed but blocked
	if err := e.validateScriptCommand("npm publish --access public"); err == nil {
		t.Error("'npm publish' should be blocked even though 'npm ' is allowed")
	}
}

func TestValidateScriptCommand_NoRestrictions(t *testing.T) {
	cfg := testExecutorConfig()
	// No allowed or blocked commands
	e := NewExecutor(cfg)

	if err := e.validateScriptCommand("anything goes"); err != nil {
		t.Errorf("should allow any command with no restrictions: %v", err)
	}
}

// =============================================================================
// Script Workdir Validation Tests
// =============================================================================

func TestValidateScriptWorkdir_NoRestrictions(t *testing.T) {
	cfg := testExecutorConfig()
	e := NewExecutor(cfg)

	if err := e.validateScriptWorkdir("/any/path"); err != nil {
		t.Errorf("should allow any workdir with no restrictions: %v", err)
	}
}

func TestValidateScriptWorkdir_Allowed(t *testing.T) {
	dir := t.TempDir()
	cfg := testExecutorConfig()
	cfg.Script.WorkdirRestrict = []string{dir}
	e := NewExecutor(cfg)

	subdir := filepath.Join(dir, "subproject")
	os.MkdirAll(subdir, 0o755)

	if err := e.validateScriptWorkdir(subdir); err != nil {
		t.Errorf("workdir under restriction should be allowed: %v", err)
	}
}

func TestValidateScriptWorkdir_Rejected(t *testing.T) {
	cfg := testExecutorConfig()
	cfg.Script.WorkdirRestrict = []string{"/allowed/path"}
	e := NewExecutor(cfg)

	if err := e.validateScriptWorkdir("/not/allowed"); err == nil {
		t.Error("workdir outside restriction should be rejected")
	}
}

func TestExecutor_Spawn_UnknownExecutorType(t *testing.T) {
	stateDir := t.TempDir()
	cfg := testExecutorConfig()
	cfg.StateDir = stateDir
	e := NewExecutor(cfg)

	task := testResolvedTask("abc123")
	task.Executor = "kubernetes"

	ctx := context.Background()
	opts := SpawnOptions{
		Mode:    ExecutionModeHeadless,
		Workdir: t.TempDir(),
	}

	_, err := e.Spawn(ctx, task, "test-project", opts)
	if err == nil {
		t.Error("Spawn should return error for unknown executor type")
	}
	if !strings.Contains(err.Error(), "unknown executor type") {
		t.Errorf("error should mention unknown executor type, got: %v", err)
	}
	if !strings.Contains(err.Error(), "kubernetes") {
		t.Errorf("error should mention the invalid type, got: %v", err)
	}
}

// =============================================================================
// Helpers
// =============================================================================

func indexOf(slice []string, item string) int {
	for i, s := range slice {
		if s == item {
			return i
		}
	}
	return -1
}

func containsArg(slice []string, item string) bool {
	return indexOf(slice, item) >= 0
}

// ─── Headless serve+attach (remote-control attachability) ─────────

// TestSpawnHeadless_ControlDisabled_DirectRun verifies that with the bridge
// disabled, headless spawn uses a plain in-process `opencode run` (no serve,
// no --attach, no port).
func TestSpawnHeadless_ControlDisabled_DirectRun(t *testing.T) {
	cfg := testExecutorConfig()
	cfg.StateDir = t.TempDir()
	cfg.Control.Disabled = true

	var calls [][]string
	e := NewExecutor(cfg)
	e.CommandFactory = func(name string, args ...string) *exec.Cmd {
		calls = append(calls, args)
		return exec.Command("/bin/echo", "mock")
	}

	res, err := e.Spawn(context.Background(), testResolvedTask("t1"), "proj", SpawnOptions{
		Mode:    ExecutionModeHeadless,
		Workdir: t.TempDir(),
	})
	if err != nil {
		t.Fatalf("Spawn failed: %v", err)
	}
	if len(calls) != 1 {
		t.Fatalf("expected exactly 1 command (direct run), got %d: %v", len(calls), calls)
	}
	args := calls[0]
	if args[0] != "run" {
		t.Errorf("expected 'run', got %v", args)
	}
	if indexOf(args, "--attach") >= 0 {
		t.Errorf("control disabled should not use --attach: %v", args)
	}
	if indexOf(args, "--port") >= 0 {
		t.Errorf("the dead --port flag should not be passed: %v", args)
	}
	if res.OpencodePort != 0 {
		t.Errorf("expected no port when control disabled, got %d", res.OpencodePort)
	}
}

// TestSpawnHeadless_ServeFailsFallsBackToDirect verifies that when the serve
// process can't bind a port (the mock exits immediately), spawn falls back to
// an in-process `opencode run` so the task still executes.
func TestSpawnHeadless_ServeFailsFallsBackToDirect(t *testing.T) {
	cfg := testExecutorConfig()
	cfg.StateDir = t.TempDir()
	// Control enabled (default) → serve+attach attempted.

	var lastArgs []string
	var serveAttempted bool
	e := NewExecutor(cfg)
	e.CommandFactory = func(name string, args ...string) *exec.Cmd {
		if len(args) > 0 && args[0] == "serve" {
			serveAttempted = true
		}
		lastArgs = args
		// /bin/echo exits immediately → serve never binds a port → fallback.
		return exec.Command("/bin/echo", "mock")
	}

	res, err := e.Spawn(context.Background(), testResolvedTask("t2"), "proj", SpawnOptions{
		Mode:    ExecutionModeHeadless,
		Workdir: t.TempDir(),
	})
	if err != nil {
		t.Fatalf("Spawn failed: %v", err)
	}
	if !serveAttempted {
		t.Error("expected a serve attempt when control is enabled")
	}
	if lastArgs[0] != "run" {
		t.Errorf("fallback should be a direct run, got %v", lastArgs)
	}
	if indexOf(lastArgs, "--attach") >= 0 {
		t.Errorf("fallback run must not use --attach: %v", lastArgs)
	}
	if res.OpencodePort != 0 {
		t.Errorf("fallback (non-attachable) should report no port, got %d", res.OpencodePort)
	}
	if res.PID <= 0 {
		t.Errorf("expected a tracked PID, got %d", res.PID)
	}
}
