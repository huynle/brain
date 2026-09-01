package runner

import (
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/huynle/brain-api/internal/types"
)

func realCommandFactory(name string, args ...string) *exec.Cmd {
	return exec.Command(name, args...)
}

// initGitRepo creates a real repository, because isGitRepo and the worktree
// helpers shell out to git — a fake would test the mock, not the resolution.
func initGitRepo(t *testing.T, dir string) {
	t.Helper()
	for _, args := range [][]string{
		{"init", "-q"},
		{"config", "user.email", "test@example.com"},
		{"config", "user.name", "Test"},
		{"commit", "-q", "--allow-empty", "-m", "init"},
	} {
		cmd := exec.Command("git", append([]string{"-C", dir}, args...)...)
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v: %s", args, err, out)
		}
	}
}

func TestTaskOriginWorkdir_Gating(t *testing.T) {
	tests := []struct {
		name       string
		task       types.ResolvedTask
		runnerHost string
		want       string
	}{
		{
			name:       "same machine, absolute path",
			task:       types.ResolvedTask{OriginMachineID: "machine_a", OriginPath: "/repos/app"},
			runnerHost: "machine_a",
			want:       "/repos/app",
		},
		{
			// The core safety property: an absolute path from another host
			// must never be opened here, however plausible it looks.
			name:       "different machine is refused",
			task:       types.ResolvedTask{OriginMachineID: "machine_a", OriginPath: "/repos/app"},
			runnerHost: "machine_b",
			want:       "",
		},
		{
			// Fail closed: an unknown runner machine cannot establish that
			// this is the origin host.
			name:       "runner with no machine id is refused",
			task:       types.ResolvedTask{OriginMachineID: "machine_a", OriginPath: "/repos/app"},
			runnerHost: "",
			want:       "",
		},
		{
			name:       "task with no origin machine is refused",
			task:       types.ResolvedTask{OriginPath: "/repos/app"},
			runnerHost: "machine_a",
			want:       "",
		},
		{
			name:       "empty origin path",
			task:       types.ResolvedTask{OriginMachineID: "machine_a"},
			runnerHost: "machine_a",
			want:       "",
		},
		{
			// A relative path has no meaningful base on the runner side.
			name:       "relative origin path is refused",
			task:       types.ResolvedTask{OriginMachineID: "machine_a", OriginPath: "projects/app"},
			runnerHost: "machine_a",
			want:       "",
		},
		{
			// Both empty must not compare equal and match.
			name:       "empty origin and empty runner do not match",
			task:       types.ResolvedTask{OriginPath: "/repos/app"},
			runnerHost: "",
			want:       "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := taskOriginWorkdir(&tt.task, RunnerConfig{MachineID: tt.runnerHost})
			if got != tt.want {
				t.Errorf("taskOriginWorkdir() = %q, want %q", got, tt.want)
			}
		})
	}
}

// TestResolveRepoContext_PrefersOriginOverHomeRelativeWorkdir is the bug this
// change exists to fix: the home-relative workdir resolves against the
// runner's home and can name a different checkout than the author meant.
func TestResolveRepoContext_PrefersOriginOverHomeRelativeWorkdir(t *testing.T) {
	origin := t.TempDir()
	initGitRepo(t, origin)

	task := &types.ResolvedTask{
		OriginMachineID: "machine_a",
		OriginPath:      origin,
		// A workdir that would resolve somewhere else entirely.
		Workdir: "some/other/checkout",
	}

	got, err := resolveRepoContext(task, RunnerConfig{MachineID: "machine_a"}, realCommandFactory)
	if err != nil {
		t.Fatalf("resolveRepoContext: %v", err)
	}
	// macOS temp dirs are symlinked (/var -> /private/var), and git reports
	// the resolved form.
	if filepath.Base(got) != filepath.Base(origin) {
		t.Fatalf("resolveRepoContext = %q, want the origin repo %q", got, origin)
	}
}

// TestResolveRepoContext_IgnoresOriginFromAnotherMachine proves the gate holds
// through the real resolution path, not just the helper.
func TestResolveRepoContext_IgnoresOriginFromAnotherMachine(t *testing.T) {
	origin := t.TempDir()
	initGitRepo(t, origin)

	task := &types.ResolvedTask{
		OriginMachineID: "machine_a",
		OriginPath:      origin,
	}

	got, err := resolveRepoContext(task, RunnerConfig{MachineID: "machine_b"}, realCommandFactory)
	if err != nil {
		t.Fatalf("resolveRepoContext: %v", err)
	}
	if got != "" {
		t.Fatalf("resolveRepoContext = %q, want \"\" — origin belongs to another machine", got)
	}
}

// TestResolveRepoContext_TargetWorkdirStillWins keeps the explicit override
// ahead of the inferred origin.
func TestResolveRepoContext_TargetWorkdirStillWins(t *testing.T) {
	origin := t.TempDir()
	initGitRepo(t, origin)
	explicit := t.TempDir()
	initGitRepo(t, explicit)

	task := &types.ResolvedTask{
		TargetWorkdir:   explicit,
		OriginMachineID: "machine_a",
		OriginPath:      origin,
	}

	got, err := resolveRepoContext(task, RunnerConfig{MachineID: "machine_a"}, realCommandFactory)
	if err != nil {
		t.Fatalf("resolveRepoContext: %v", err)
	}
	if got != explicit {
		t.Fatalf("resolveRepoContext = %q, want explicit target_workdir %q", got, explicit)
	}
}

// TestResolveRepoContext_KeepsLinkedWorktree: when no new worktree is being
// created, a task authored inside a linked worktree must run THERE. Rewriting
// it to the main checkout would silently move the work off the branch its
// author was on.
func TestResolveRepoContext_KeepsLinkedWorktree(t *testing.T) {
	main := t.TempDir()
	initGitRepo(t, main)

	linked := filepath.Join(t.TempDir(), "feature-wt")
	cmd := exec.Command("git", "-C", main, "worktree", "add", "-q", "-b", "feature-x", linked)
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Skipf("git worktree add unavailable: %v: %s", err, out)
	}

	task := &types.ResolvedTask{
		OriginMachineID: "machine_a",
		OriginPath:      linked,
	}

	got, err := resolveRepoContext(task, RunnerConfig{MachineID: "machine_a"}, realCommandFactory)
	if err != nil {
		t.Fatalf("resolveRepoContext: %v", err)
	}
	if filepath.Base(got) != filepath.Base(linked) {
		t.Fatalf("resolveRepoContext = %q, want the linked worktree %q", got, linked)
	}
}

// TestEnsureWorktree_AnchorsNewWorktreeOnMainRepo is the other half: worktree
// CREATION must anchor on the main repo, because worktrees live at
// {repo}/.worktrees and anchoring on a linked worktree nests them one level
// deeper on every hop.
func TestEnsureWorktree_AnchorsNewWorktreeOnMainRepo(t *testing.T) {
	main := t.TempDir()
	initGitRepo(t, main)

	linked := filepath.Join(t.TempDir(), "feature-wt")
	cmd := exec.Command("git", "-C", main, "worktree", "add", "-q", "-b", "feature-x", linked)
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Skipf("git worktree add unavailable: %v: %s", err, out)
	}

	// Author sat in the linked worktree; the task wants a DIFFERENT branch,
	// so a new worktree has to be created.
	task := &types.ResolvedTask{
		OriginMachineID: "machine_a",
		OriginPath:      linked,
		GitBranch:       "feature-y",
		ExecutionMode:   "worktree",
	}

	got, err := CommonResolveWorkdir(task, RunnerConfig{MachineID: "machine_a"}, realCommandFactory)
	if err != nil {
		t.Fatalf("CommonResolveWorkdir: %v", err)
	}

	// It must land under the MAIN repo's .worktrees, never nested inside the
	// linked worktree.
	if strings.Contains(got, filepath.Join("feature-wt", ".worktrees")) {
		t.Fatalf("new worktree nested inside the linked worktree: %q", got)
	}
	if filepath.Base(got) != "feature-y" {
		t.Fatalf("worktree path = %q, want one ending in feature-y", got)
	}
	if !strings.Contains(got, ".worktrees") {
		t.Fatalf("worktree path = %q, want it under .worktrees", got)
	}
}

// TestNewExecutorRegistry_FillsMachineID guards the wiring: an empty
// MachineID silently disables origin resolution everywhere.
func TestNewExecutorRegistry_FillsMachineID(t *testing.T) {
	reg := NewExecutorRegistry(RunnerConfig{})
	if reg.config.MachineID == "" {
		t.Fatal("NewExecutorRegistry left MachineID empty; origin workdir resolution would never engage")
	}
}

// TestNewExecutorRegistry_KeepsExplicitMachineID lets tests and callers pin it.
func TestNewExecutorRegistry_KeepsExplicitMachineID(t *testing.T) {
	reg := NewExecutorRegistry(RunnerConfig{MachineID: "machine_explicit"})
	if reg.config.MachineID != "machine_explicit" {
		t.Fatalf("MachineID = %q, want machine_explicit", reg.config.MachineID)
	}
}
