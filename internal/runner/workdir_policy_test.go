package runner

import (
	"context"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/huynle/brain-api/internal/types"
)

func TestWorkdirPolicyRawCacheSymlinkParent(t *testing.T) {
	for _, allowed := range []bool{false, true} {
		t.Run(map[bool]string{false: "refuse escape before mutation", true: "create authorized physical cache"}[allowed], func(t *testing.T) {
			root, outside := t.TempDir(), t.TempDir()
			if err := os.Mkdir(filepath.Join(outside, "deep"), 0755); err != nil {
				t.Fatal(err)
			}
			if err := os.Symlink(filepath.Join(outside, "deep"), filepath.Join(root, "link")); err != nil {
				t.Fatal(err)
			}
			cfg := RunnerConfig{RepoCacheDir: root + "/link/../cache", AllowUnauthenticatedHTTPS: true,
				GitAllowedHosts: []string{"example.com"},
				Control:         ControlConfig{AllowedWorkdirRoots: []string{root}}}
			if allowed {
				cfg.Control.AllowedWorkdirRoots = []string{outside}
			}
			calls := 0
			var clonePath string
			factory := func(name string, args ...string) *exec.Cmd {
				calls++
				if clone := gitOperationArgs(args, "clone"); name == "git" && len(clone) == 5 {
					clonePath = clone[4]
				}
				return exec.Command("false")
			}
			_, err := ensureCachedRemoteRepo("https://example.com/repo.git", cfg, factory)
			physicalCache := filepath.Join(outside, "cache")
			if !allowed {
				if err == nil || !strings.Contains(err.Error(), "allowed root") {
					t.Errorf("expected policy refusal, got %v", err)
				}
				if calls != 0 {
					t.Errorf("git called %d times before policy refusal", calls)
				}
				if _, statErr := os.Stat(physicalCache); !os.IsNotExist(statErr) {
					t.Errorf("outside cache created before refusal: %v", statErr)
				}
			} else {
				if err == nil || !strings.Contains(err.Error(), "git clone failed") {
					t.Errorf("expected fake clone failure after authorization, got %v", err)
				}
				canonicalOutside, evalErr := filepath.EvalSymlinks(outside)
				if evalErr != nil {
					t.Fatal(evalErr)
				}
				remote, _ := url.Parse("https://example.com/repo.git")
				want := filepath.Join(canonicalOutside, "cache", cacheDirNameForRemote(remote))
				if clonePath != want {
					t.Errorf("clone path=%q want authorized physical path=%q", clonePath, want)
				}
				if info, statErr := os.Stat(physicalCache); statErr != nil || !info.IsDir() {
					t.Errorf("authorized cache not created: %v", statErr)
				}
			}
			if _, statErr := os.Stat(filepath.Join(root, "cache")); !os.IsNotExist(statErr) {
				t.Errorf("lexically cleaned cache was created: %v", statErr)
			}
		})
	}
}

func TestWorkdirPolicyParity(t *testing.T) {
	base := t.TempDir()
	root := filepath.Join(base, "allowed")
	outside := filepath.Join(base, "allowed-sibling")
	for _, p := range []string{root, outside} {
		if err := os.Mkdir(p, 0755); err != nil {
			t.Fatal(err)
		}
	}
	alias := filepath.Join(base, "alias")
	if err := os.Symlink(root, alias); err != nil {
		t.Fatal(err)
	}
	escape := filepath.Join(root, "escape")
	if err := os.Symlink(outside, escape); err != nil {
		t.Fatal(err)
	}
	file := filepath.Join(root, "file")
	if err := os.WriteFile(file, nil, 0600); err != nil {
		t.Fatal(err)
	}
	for _, p := range []string{filepath.Join(outside, "deep"), filepath.Join(outside, "destination"), filepath.Join(root, "destination")} {
		if err := os.Mkdir(p, 0755); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.Symlink(filepath.Join(outside, "deep"), filepath.Join(root, "link")); err != nil {
		t.Fatal(err)
	}
	// Do not filepath.Join: it would erase the symlink/.. traversal under test.
	traversal := root + "/link/../destination"
	for _, tc := range []struct {
		name, path, root string
		ok               bool
	}{
		{"allowed", root, root, true}, {"sibling", outside, root, false},
		{"escape", escape, root, false}, {"candidate alias", alias, root, true},
		{"root alias", root, alias, true}, {"file", file, root, false},
		{"relative", "relative/path", root, false},
		{"invalid root", root, filepath.Join(base, "absent-root"), false},
		{"file root", root, file, false},
		{"symlink parent escape", traversal, root, false},
		{"root symlink parent", filepath.Join(outside, "destination"), traversal, true},
		{"missing forbidden target", filepath.Join(outside, "missing"), root, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			cfg := RunnerConfig{WorkDir: root, Control: ControlConfig{AllowedWorkdirRoots: []string{tc.root}}}
			bc := &BridgeClient{runner: &TaskRunner{config: cfg}}
			adhocErr := bc.validateSpawnWorkdir(tc.path)
			_, taskErr := CommonResolveWorkdir(&types.ResolvedTask{TargetWorkdir: tc.path, ExecutionMode: "current_branch"}, cfg, exec.Command)
			if (adhocErr == nil) != tc.ok || (taskErr == nil) != tc.ok {
				t.Fatalf("want allowed=%v; adhoc=%v task=%v", tc.ok, adhocErr, taskErr)
			}
		})
	}
}

func TestWorkdirPolicyScriptLinkedWorktree(t *testing.T) {
	root := t.TempDir()
	repo := filepath.Join(root, "repo")
	if err := os.Mkdir(repo, 0755); err != nil {
		t.Fatal(err)
	}
	// Fixture-only history; never commit in the project under development.
	for _, args := range [][]string{
		{"init", "-q", "-b", "main"},
		{"-c", "user.name=Test", "-c", "user.email=test@example.com", "-c", "commit.gpgsign=false", "-c", "core.hooksPath=/dev/null", "commit", "-q", "--allow-empty", "-m", "fixture"},
	} {
		if out, err := exec.Command("git", append([]string{"-C", repo}, args...)...).CombinedOutput(); err != nil {
			t.Fatalf("git fixture: %v: %s", err, out)
		}
	}
	cfg := RunnerConfig{StateDir: t.TempDir(), Control: ControlConfig{AllowedWorkdirRoots: []string{root}}, Script: ScriptConfig{Enabled: true}}
	task := &types.ResolvedTask{ID: "isolated", Executor: "script", ExecutionMode: "worktree", TargetWorkdir: repo, GitBranch: "phase2-child", DirectPrompt: `set -eu; test -f .git; test "$(git branch --show-current)" = phase2-child; pwd -P > child-result; printf 'isolated-child-ok\n'`}
	e := NewExecutor(cfg)
	result, err := e.Spawn(context.Background(), task, "policy", SpawnOptions{Mode: ExecutionModeHeadless})
	if err != nil {
		t.Fatal(err)
	}
	proc := result.Proc.(*OsProcess)
	select {
	case <-proc.Done():
	case <-time.After(10 * time.Second):
		_ = proc.Kill(nil)
		t.Fatal("script child timed out")
	}
	if proc.ExitCode() != 0 {
		t.Fatalf("child exit=%d", proc.ExitCode())
	}
	canonical, err := filepath.EvalSymlinks(result.Workdir)
	if err != nil {
		t.Fatal(err)
	}
	expected, err := filepath.EvalSymlinks(filepath.Join(repo, ".worktrees", "phase2-child"))
	if err != nil {
		t.Fatal(err)
	}
	if canonical != expected {
		t.Fatalf("workdir=%s want=%s", canonical, expected)
	}
	bc := &BridgeClient{runner: &TaskRunner{config: cfg}}
	if err := bc.validateSpawnWorkdir(result.Workdir); err != nil {
		t.Fatalf("ad-hoc rejects legitimate worktree: %v", err)
	}
	data, err := os.ReadFile(filepath.Join(result.Workdir, "child-result"))
	if err != nil {
		t.Fatal(err)
	}
	if strings.TrimSpace(string(data)) != canonical {
		t.Fatalf("child cwd=%q want=%s", data, canonical)
	}
	if _, err := os.Stat(filepath.Join(repo, "child-result")); !os.IsNotExist(err) {
		t.Fatalf("child wrote into main checkout: %v", err)
	}
	out, err := exec.Command("git", "-C", repo, "branch", "--show-current").Output()
	if err != nil {
		t.Fatal(err)
	}
	if strings.TrimSpace(string(out)) != "main" {
		t.Fatalf("main checkout branch changed: %s", out)
	}
	log, err := os.ReadFile(filepath.Join(cfg.StateDir, "output_policy_isolated.log"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(log), "isolated-child-ok") {
		t.Fatalf("missing child output: %s", log)
	}
	t.Logf("actual script child pid=%d exit=0 cwd=%s; linked .git file, branch and main isolation verified", result.PID, canonical)
}

func TestWorkdirPolicyFallbacks(t *testing.T) {
	root, outside := t.TempDir(), t.TempDir()
	for _, field := range []string{"origin", "workdir", "resolved", "default"} {
		for _, allowed := range []bool{true, false} {
			t.Run(field+map[bool]string{true: " allowed", false: " forbidden"}[allowed], func(t *testing.T) {
				p := outside
				if allowed {
					p = root
				}
				cfg := RunnerConfig{WorkDir: root, MachineID: "host", Control: ControlConfig{AllowedWorkdirRoots: []string{root}}}
				task := &types.ResolvedTask{ExecutionMode: "current_branch"}
				switch field {
				case "origin":
					task.OriginPath = p
					task.OriginMachineID = "host"
				case "workdir":
					task.Workdir = p
				case "resolved":
					task.ResolvedWorkdir = p
				case "default":
					cfg.WorkDir = p
				}
				_, err := CommonResolveWorkdir(task, cfg, exec.Command)
				if (err == nil) != allowed {
					t.Fatalf("allowed=%v err=%v", allowed, err)
				}
			})
		}
	}
}

func TestWorkdirPolicyHomeOnly(t *testing.T) {
	home, outside := t.TempDir(), t.TempDir()
	t.Setenv("HOME", home)
	cfg := RunnerConfig{}
	for _, p := range []string{home, outside} {
		_, err := CommonResolveWorkdir(&types.ResolvedTask{TargetWorkdir: p, ExecutionMode: "current_branch"}, cfg, exec.Command)
		adhocErr := (&BridgeClient{runner: &TaskRunner{config: cfg}}).validateSpawnWorkdir(p)
		if (err == nil) != (p == home) || (adhocErr == nil) != (p == home) {
			t.Fatalf("home-only path %s: task=%v adhoc=%v", p, err, adhocErr)
		}
	}
	t.Setenv("HOME", "")
	_, err := CommonResolveWorkdir(&types.ResolvedTask{TargetWorkdir: home, ExecutionMode: "current_branch"}, cfg, exec.Command)
	if err == nil {
		t.Fatal("missing home must fail closed")
	}
	if err := (&BridgeClient{runner: &TaskRunner{config: cfg}}).validateSpawnWorkdir(home); err == nil {
		t.Fatal("ad-hoc missing home must fail closed")
	}
}

func TestWorkdirPolicyHomeRelativeFallback(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	repo := filepath.Join(home, "repo")
	if err := os.Mkdir(repo, 0755); err != nil {
		t.Fatal(err)
	}
	cfg := RunnerConfig{Control: ControlConfig{AllowedWorkdirRoots: []string{repo}}}
	got, err := CommonResolveWorkdir(&types.ResolvedTask{ExecutionMode: "current_branch", Workdir: "repo"}, cfg, exec.Command)
	if err != nil || got != repo {
		t.Fatalf("home-relative fallback: got=%s err=%v", got, err)
	}
}

func TestWorkdirPolicySpawnOverride(t *testing.T) {
	root, outside := t.TempDir(), t.TempDir()
	cfg := RunnerConfig{StateDir: t.TempDir(), Control: ControlConfig{AllowedWorkdirRoots: []string{root}}}
	for _, executor := range []TaskExecutor{NewExecutor(cfg), NewPiExecutor(cfg)} {
		_, err := executor.Spawn(context.Background(), &types.ResolvedTask{ID: "policy", Executor: "script", DirectPrompt: "true"}, "test", SpawnOptions{Workdir: outside})
		if err == nil || !strings.Contains(err.Error(), "allowed root") {
			t.Fatalf("override not rejected by roots policy: %v", err)
		}
	}
}

func TestWorkdirPolicySpawnOverrideCannotHideForbiddenTarget(t *testing.T) {
	root, outside := t.TempDir(), t.TempDir()
	cfg := RunnerConfig{StateDir: t.TempDir(), Control: ControlConfig{AllowedWorkdirRoots: []string{root}}}
	for _, executor := range []TaskExecutor{NewExecutor(cfg), NewPiExecutor(cfg)} {
		_, err := executor.Spawn(context.Background(), &types.ResolvedTask{ID: "policy", Executor: "script", TargetWorkdir: outside}, "test", SpawnOptions{Workdir: root, Mode: "invalid"})
		if err == nil || !strings.Contains(err.Error(), "allowed root") {
			t.Errorf("forbidden target hidden by override: %v", err)
		}
	}
}

func TestWorkdirPolicyPreflight(t *testing.T) {
	for _, kind := range []string{"cache", "worktree", "repo"} {
		t.Run(kind, func(t *testing.T) {
			root, outside := t.TempDir(), t.TempDir()
			cfg := RunnerConfig{Control: ControlConfig{AllowedWorkdirRoots: []string{root}}, AllowUnauthenticatedHTTPS: true, RepoCacheDir: filepath.Join(outside, "cache")}
			cfg.GitAllowedHosts = []string{"example.com"}
			task := &types.ResolvedTask{ExecutionMode: "worktree", GitBranch: "feature", TargetWorkdir: root}
			if kind == "cache" {
				task.TargetWorkdir = ""
				task.GitRemote = "https://example.com/repo.git"
			}
			if kind == "repo" {
				task.TargetWorkdir = outside
			}
			if kind == "worktree" {
				if err := os.Symlink(outside, filepath.Join(root, ".worktrees")); err != nil {
					t.Fatal(err)
				}
			}
			mutations := 0
			factory := func(name string, args ...string) *exec.Cmd {
				joined := strings.Join(args, " ")
				if strings.Contains(joined, " clone ") || strings.Contains(joined, "worktree add") || (len(args) > 0 && args[0] == "clone") {
					mutations++
				}
				// Simulate a repository with no existing worktrees/current branch.
				return exec.Command("true")
			}
			_, err := CommonResolveWorkdir(task, cfg, factory)
			if err == nil || !strings.Contains(err.Error(), "allowed root") {
				t.Errorf("expected roots refusal, got %v", err)
			}
			if mutations != 0 {
				t.Errorf("performed %d git mutations before refusal", mutations)
			}
			if _, err := os.Stat(filepath.Join(outside, "cache")); !os.IsNotExist(err) {
				t.Errorf("cache created outside root: %v", err)
			}
			if _, err := os.Stat(filepath.Join(root, ".gitignore")); !os.IsNotExist(err) {
				t.Errorf("gitignore changed before refusal: %v", err)
			}
		})
	}
}
