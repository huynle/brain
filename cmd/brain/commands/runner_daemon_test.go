package commands

import (
	"os"
	"path/filepath"
	"strconv"
	"testing"

	"github.com/huynle/brain-api/internal/runner"
)

// withStateHome points the daemon's pid/log files at a temp dir.
func withStateHome(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	t.Setenv("XDG_STATE_HOME", dir)
	return filepath.Join(dir, "brain-api")
}

func TestRunnerDaemonFilePaths(t *testing.T) {
	stateDir := withStateHome(t)

	// The unnamed runner must keep the historical paths: an in-place upgrade
	// has to still find (and be able to stop) a runner the old binary started.
	if got, want := runnerPIDFile(""), filepath.Join(stateDir, "brain-runner.pid"); got != want {
		t.Errorf("default pid file = %q, want %q", got, want)
	}
	if got, want := runnerPIDFile(runner.DefaultRunnerName), filepath.Join(stateDir, "brain-runner.pid"); got != want {
		t.Errorf("%q pid file = %q, want %q", runner.DefaultRunnerName, got, want)
	}
	if got, want := runnerLogFile(runner.DefaultRunnerName), filepath.Join(stateDir, "brain-runner.log"); got != want {
		t.Errorf("default log file = %q, want %q", got, want)
	}

	if got, want := runnerPIDFile("worker-a"), filepath.Join(stateDir, "brain-runner-worker-a.pid"); got != want {
		t.Errorf("named pid file = %q, want %q", got, want)
	}
	if got, want := runnerLogFile("worker-a"), filepath.Join(stateDir, "brain-runner-worker-a.log"); got != want {
		t.Errorf("named log file = %q, want %q", got, want)
	}
	if runnerPIDFile("worker-a") == runnerPIDFile("worker-b") {
		t.Error("two named runners share a pid file")
	}
}

func TestRunnerDaemonName(t *testing.T) {
	cmd := &RunnerDaemonCommand{Flags: &RunnerFlags{}}
	name, err := cmd.runnerName()
	if err != nil || name != runner.DefaultRunnerName {
		t.Fatalf("runnerName() = %q, %v; want %q, nil", name, err, runner.DefaultRunnerName)
	}

	cmd = &RunnerDaemonCommand{Flags: &RunnerFlags{Name: "worker-a"}}
	if name, err = cmd.runnerName(); err != nil || name != "worker-a" {
		t.Fatalf("runnerName() = %q, %v; want worker-a, nil", name, err)
	}

	// A name that could escape the state dir must fail loudly rather than be
	// sanitized into some other runner's identity.
	cmd = &RunnerDaemonCommand{Flags: &RunnerFlags{Name: "../evil"}}
	if name, err = cmd.runnerName(); err == nil {
		t.Fatalf("runnerName() accepted %q -> %q", "../evil", name)
	}
}

func TestDiscoverLocalRunners(t *testing.T) {
	stateDir := withStateHome(t)
	if err := os.MkdirAll(stateDir, 0o755); err != nil {
		t.Fatal(err)
	}
	base := t.TempDir()

	// A live runner (this test process), a dead one, and files that must not
	// be mistaken for runner pid files.
	write := func(name, body string) {
		if err := os.WriteFile(filepath.Join(stateDir, name), []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	write("brain-runner.pid", "999999")
	write("brain-runner-worker-a.pid", strconv.Itoa(os.Getpid()))
	write("brain-api.pid", "12345")
	write("brain-runner-worker-a.log", "logs")

	// worker-a has already persisted a runner id in its own state dir.
	workerDir := runner.RunnerStateDir(base, "worker-a")
	if err := os.MkdirAll(workerDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(workerDir, "runner-id"), []byte("runner_abc123\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	found := discoverLocalRunners(base)
	if len(found) != 2 {
		t.Fatalf("discovered %d runners, want 2: %+v", len(found), found)
	}
	if found[0].Name != runner.DefaultRunnerName || found[1].Name != "worker-a" {
		t.Fatalf("names = %q, %q; want default, worker-a (sorted)", found[0].Name, found[1].Name)
	}
	if found[0].Running {
		t.Error("dead PID reported as running")
	}
	if !found[1].Running {
		t.Error("live PID reported as stopped")
	}
	if found[1].RunnerID != "runner_abc123" {
		t.Errorf("worker-a runner id = %q, want runner_abc123", found[1].RunnerID)
	}
	if found[0].RunnerID != "" {
		t.Errorf("default runner id = %q, want empty (never started)", found[0].RunnerID)
	}
}

func TestAllocateRunnerName(t *testing.T) {
	stateDir := withStateHome(t)
	if err := os.MkdirAll(stateDir, 0o755); err != nil {
		t.Fatal(err)
	}
	base := t.TempDir()
	cfg := &UnifiedConfig{}
	cfg.Runner.StateDir = base

	newCmd := func(flags *RunnerFlags) *RunnerDaemonCommand {
		return &RunnerDaemonCommand{Subcommand: "start", Config: cfg, Flags: flags}
	}

	// No --new: the explicit name (or the default) comes straight through.
	name, err := newCmd(&RunnerFlags{}).allocateRunnerName()
	if err != nil || name != runner.DefaultRunnerName {
		t.Fatalf("allocateRunnerName = %q, %v; want %q", name, err, runner.DefaultRunnerName)
	}
	name, err = newCmd(&RunnerFlags{Name: "worker-a"}).allocateRunnerName()
	if err != nil || name != "worker-a" {
		t.Fatalf("allocateRunnerName = %q, %v; want worker-a", name, err)
	}

	// --new allocates, and skips a slot whose daemon is alive.
	name, err = newCmd(&RunnerFlags{New: true}).allocateRunnerName()
	if err != nil || name != "runner-2" {
		t.Fatalf("allocateRunnerName = %q, %v; want runner-2", name, err)
	}
	if err := os.WriteFile(runnerPIDFile("runner-2"), []byte(strconv.Itoa(os.Getpid())), 0o644); err != nil {
		t.Fatal(err)
	}
	name, err = newCmd(&RunnerFlags{New: true}).allocateRunnerName()
	if err != nil || name != "runner-3" {
		t.Fatalf("allocateRunnerName = %q, %v; want runner-3 (runner-2 daemon is alive)", name, err)
	}

	// A stale pid file frees the slot again — reusing it keeps that runner id.
	if err := os.WriteFile(runnerPIDFile("runner-2"), []byte("999999"), 0o644); err != nil {
		t.Fatal(err)
	}
	name, err = newCmd(&RunnerFlags{New: true}).allocateRunnerName()
	if err != nil || name != "runner-2" {
		t.Fatalf("allocateRunnerName = %q, %v; want runner-2 (stale pid)", name, err)
	}

	// --new plus an explicit name is a contradiction, not a silent preference.
	if _, err = newCmd(&RunnerFlags{New: true, Name: "worker-a"}).allocateRunnerName(); err == nil {
		t.Fatal("allocateRunnerName accepted --new together with --name")
	}
}

func TestRejectNewFlagOnStopAndStatus(t *testing.T) {
	withStateHome(t)
	for _, sub := range []string{"stop", "status"} {
		cmd := &RunnerDaemonCommand{Subcommand: sub, Config: &UnifiedConfig{}, Flags: &RunnerFlags{New: true}}
		if err := cmd.Execute(); err == nil {
			t.Errorf("brain runner %s --new was accepted; want an error", sub)
		}
	}
}
