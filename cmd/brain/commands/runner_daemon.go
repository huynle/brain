package commands

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"syscall"
	"text/tabwriter"
	"time"

	"github.com/huynle/brain-api/internal/lifecycle"
	"github.com/huynle/brain-api/internal/runner"
)

// RunnerDaemonCommand implements `brain runner <start|stop|status>` — a
// background (daemonized) headless runner that registers with the Brain API and
// claims/executes tasks. This is the "go to a machine and start a runner" path;
// the granular `brain run ...` subcommands are unchanged.
//
// Several runners can share a machine. Each one is addressed by --name, which
// selects its state dir (hence its persisted runner id) and its pid/log files.
// The unnamed runner keeps the historical single-runner paths, so upgrading
// does not strand an existing deployment's id.
type RunnerDaemonCommand struct {
	Subcommand string
	Project    string
	Config     *UnifiedConfig
	Flags      *RunnerFlags
}

// Type returns the command type identifier.
func (c *RunnerDaemonCommand) Type() string { return "runner_" + c.Subcommand }

// Execute dispatches the runner daemon subcommand.
func (c *RunnerDaemonCommand) Execute() error {
	switch c.Subcommand {
	case "start":
		return c.start()
	case "stop":
		return c.stop()
	case "status":
		return c.status()
	default:
		return fmt.Errorf("unknown runner subcommand: %q (try: start, stop, status)", c.Subcommand)
	}
}

// runnerStateFile returns a state-dir path for the runner daemon, respecting
// XDG_STATE_HOME (same convention as the API daemon).
func runnerStateFile(name string) string {
	return filepath.Join(runnerDaemonStateDir(), name)
}

// runnerDaemonStateDir is where the daemon's pid and log files live.
func runnerDaemonStateDir() string {
	stateHome := os.Getenv("XDG_STATE_HOME")
	if stateHome == "" {
		homeDir, _ := os.UserHomeDir()
		stateHome = filepath.Join(homeDir, ".local", "state")
	}
	return filepath.Join(stateHome, "brain-api")
}

// daemonFileSuffix maps a runner name onto its pid/log file suffix. The default
// runner keeps the original bare names so an in-place upgrade still finds (and
// can stop) a runner started by the previous version.
func daemonFileSuffix(name string) string {
	if name == "" || name == runner.DefaultRunnerName {
		return ""
	}
	return "-" + name
}

func runnerPIDFile(name string) string {
	return runnerStateFile("brain-runner" + daemonFileSuffix(name) + ".pid")
}

func runnerLogFile(name string) string {
	return runnerStateFile("brain-runner" + daemonFileSuffix(name) + ".log")
}

// runnerPIDFilePattern matches the daemon pid files of every local runner,
// capturing the name (empty for the default runner).
var runnerPIDFilePattern = regexp.MustCompile(`^brain-runner(?:-([A-Za-z0-9][A-Za-z0-9._-]*))?\.pid$`)

// runnerName resolves the runner this invocation addresses, validating it the
// same way the runner process itself does so a typo fails here rather than
// silently starting a second, differently-named runner.
func (c *RunnerDaemonCommand) runnerName() (string, error) {
	name := ""
	if c.Flags != nil {
		name = c.Flags.Name
	}
	return runner.NormalizeRunnerName(name)
}

// allocateRunnerName returns the name a `start` should use: the one given with
// --name, or — with --new — the lowest free auto name.
//
// "Free" means no live daemon pid file AND no live runner in that state dir, so
// --new can never hand out a name a foreground or TUI runner is already using.
func (c *RunnerDaemonCommand) allocateRunnerName() (string, error) {
	if c.Flags != nil && c.Flags.New {
		if strings.TrimSpace(c.Flags.Name) != "" {
			return "", fmt.Errorf("--new and --name are mutually exclusive: --new picks the name for you")
		}
		return runner.NextFreeRunnerName(c.baseStateDir(), func(name string) bool {
			pid, err := lifecycle.ReadPID(runnerPIDFile(name))
			return err == nil && lifecycle.IsProcessRunning(pid)
		})
	}
	return c.runnerName()
}

// rejectNewFlag guards the subcommands --new says nothing about. Silently
// ignoring a flag is how `--executor` went missing for a release.
func (c *RunnerDaemonCommand) rejectNewFlag() error {
	if c.Flags != nil && c.Flags.New {
		return fmt.Errorf("--new applies to `brain runner start`, not `brain runner %s`", c.Subcommand)
	}
	return nil
}

// start launches the runner. By default it daemonizes (detaches into the
// background); --foreground runs it headless in the current terminal.
func (c *RunnerDaemonCommand) start() error {
	name, err := c.allocateRunnerName()
	if err != nil {
		return err
	}

	// Foreground: run a headless runner in this process (delegates to `run start`).
	// Hand it the already-allocated name so --new is resolved once, not twice.
	if c.Flags.Foreground {
		c.Flags.Name = name
		c.Flags.New = false
		rc := &RunCommand{
			Subcommand: "start",
			Project:    c.Project,
			Config:     c.Config,
			Flags:      c.Flags,
		}
		rc.Flags.Headless = true
		return rc.Execute()
	}

	pidFile := runnerPIDFile(name)
	logFile := runnerLogFile(name)

	// Already running? Only this name conflicts — a differently-named runner on
	// the same host is the point of the feature, not a collision.
	if pid, err := lifecycle.ReadPID(pidFile); err == nil {
		if lifecycle.IsProcessRunning(pid) {
			return fmt.Errorf("runner %q already running (PID %d) — stop it with `brain runner stop%s`, or start another with `brain runner start --new` (auto-named) or `--name <other>`",
				name, pid, stopFlagSuffix(name))
		}
		_ = lifecycle.ClearPID(pidFile) // stale
	}

	bin := brainBinaryPath()
	args := []string{"run", "start", c.Project, "--headless"}
	if name != runner.DefaultRunnerName {
		args = append(args, "--name", name)
	}
	if c.Flags.MaxParallel > 0 {
		args = append(args, "--max-parallel", strconv.Itoa(c.Flags.MaxParallel))
	}
	for _, inc := range c.Flags.Include {
		args = append(args, "--include", inc)
	}
	for _, exc := range c.Flags.Exclude {
		args = append(args, "--exclude", exc)
	}
	if c.Flags.Executor != "" {
		args = append(args, "--executor", c.Flags.Executor)
	}

	pid, err := lifecycle.Daemonize(bin, args, lifecycle.DaemonOptions{PIDFile: pidFile, LogFile: logFile})
	if err != nil {
		return fmt.Errorf("failed to start runner: %w", err)
	}

	fmt.Printf("Runner started (PID %d)\n", pid)
	fmt.Printf("Name:       %s\n", name)
	fmt.Printf("Project(s): %s\n", c.Project)
	fmt.Printf("Logs:       %s\n", logFile)
	fmt.Printf("Stop with:  brain runner stop%s\n", stopFlagSuffix(name))
	return nil
}

// stopFlagSuffix renders the `--name` argument a follow-up command needs, or
// nothing at all for the default runner.
func stopFlagSuffix(name string) string {
	if name == "" || name == runner.DefaultRunnerName {
		return ""
	}
	return " --name " + name
}

// stop sends SIGTERM to the background runner and waits for it to exit.
// With --all it stops every runner started on this machine.
func (c *RunnerDaemonCommand) stop() error {
	if err := c.rejectNewFlag(); err != nil {
		return err
	}
	if c.Flags != nil && c.Flags.All {
		return c.stopAll()
	}
	name, err := c.runnerName()
	if err != nil {
		return err
	}
	return stopRunnerNamed(name)
}

// stopAll stops every local runner, reporting per-runner outcomes instead of
// aborting on the first failure — one wedged runner must not leave the rest up.
func (c *RunnerDaemonCommand) stopAll() error {
	runners := discoverLocalRunners("")
	stopped := 0
	var failures []string
	for _, r := range runners {
		if !r.Running {
			continue
		}
		if err := stopRunnerNamed(r.Name); err != nil {
			failures = append(failures, fmt.Sprintf("%s: %v", r.Name, err))
			continue
		}
		stopped++
	}
	if len(failures) > 0 {
		return fmt.Errorf("stopped %d runner(s); failed: %s", stopped, strings.Join(failures, "; "))
	}
	if stopped == 0 {
		return fmt.Errorf("no runners running")
	}
	fmt.Printf("Stopped %d runner(s)\n", stopped)
	return nil
}

func stopRunnerNamed(name string) error {
	pidFile := runnerPIDFile(name)
	pid, err := lifecycle.ReadPID(pidFile)
	if err != nil {
		if os.IsNotExist(err) {
			return fmt.Errorf("runner %q not running (no PID file)", name)
		}
		return fmt.Errorf("failed to read runner PID file: %w", err)
	}
	if !lifecycle.IsProcessRunning(pid) {
		_ = lifecycle.ClearPID(pidFile)
		return fmt.Errorf("runner %q not running (stale PID %d)", name, pid)
	}

	proc, err := os.FindProcess(pid)
	if err != nil {
		return fmt.Errorf("failed to find runner process: %w", err)
	}
	fmt.Printf("Stopping runner %q (PID %d)…\n", name, pid)
	if err := proc.Signal(syscall.SIGTERM); err != nil {
		return fmt.Errorf("failed to signal runner: %w", err)
	}

	deadline := time.Now().Add(15 * time.Second)
	for time.Now().Before(deadline) {
		if !lifecycle.IsProcessRunning(pid) {
			_ = lifecycle.ClearPID(pidFile)
			fmt.Printf("Runner %q stopped\n", name)
			return nil
		}
		time.Sleep(200 * time.Millisecond)
	}
	return fmt.Errorf("runner %q (PID %d) did not stop within 15s", name, pid)
}

// baseStateDir is the configured state-dir root (before the per-name segment),
// so `status` peeks at the same runner-id files the runner itself would use.
func (c *RunnerDaemonCommand) baseStateDir() string {
	if c.Config != nil && strings.TrimSpace(c.Config.Runner.StateDir) != "" {
		return c.Config.Runner.StateDir
	}
	return runner.DefaultStateDir()
}

// localRunner describes one runner daemon started on this machine.
type localRunner struct {
	Name     string
	PID      int
	Running  bool
	RunnerID string
	LogFile  string
}

// discoverLocalRunners lists the runners this machine has started, from their
// pid files. Stale entries (dead PID) are reported rather than hidden, so a
// crashed runner is visible instead of merely absent.
func discoverLocalRunners(baseStateDir string) []localRunner {
	entries, err := os.ReadDir(runnerDaemonStateDir())
	if err != nil {
		return nil
	}
	var found []localRunner
	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		m := runnerPIDFilePattern.FindStringSubmatch(entry.Name())
		if m == nil {
			continue
		}
		name := m[1]
		if name == "" {
			name = runner.DefaultRunnerName
		}
		lr := localRunner{Name: name, LogFile: runnerLogFile(name)}
		if pid, err := lifecycle.ReadPID(runnerPIDFile(name)); err == nil {
			lr.PID = pid
			lr.Running = lifecycle.IsProcessRunning(pid)
		}
		if baseStateDir != "" {
			lr.RunnerID = runner.PeekRunnerID(runner.RunnerStateDir(baseStateDir, name))
		}
		found = append(found, lr)
	}
	sort.Slice(found, func(i, j int) bool { return found[i].Name < found[j].Name })
	return found
}

// status reports the background runners on this machine. With --name it reports
// just that one; otherwise every runner this host has started.
func (c *RunnerDaemonCommand) status() error {
	if err := c.rejectNewFlag(); err != nil {
		return err
	}
	if c.Flags != nil && c.Flags.Name != "" {
		name, err := c.runnerName()
		if err != nil {
			return err
		}
		pid, err := lifecycle.ReadPID(runnerPIDFile(name))
		if err != nil || !lifecycle.IsProcessRunning(pid) {
			fmt.Printf("Runner %q: not running\n", name)
			return nil
		}
		fmt.Printf("Runner %q: running (PID %d)\n", name, pid)
		fmt.Printf("Logs:   %s\n", runnerLogFile(name))
		return nil
	}

	runners := discoverLocalRunners(c.baseStateDir())
	if len(runners) == 0 {
		fmt.Println("Runner: not running")
		return nil
	}

	w := tabwriter.NewWriter(os.Stdout, 0, 4, 2, ' ', 0)
	fmt.Fprintln(w, "NAME\tSTATUS\tPID\tRUNNER_ID\tLOGS")
	for _, r := range runners {
		status := "stopped"
		pid := "-"
		if r.Running {
			status = "running"
			pid = strconv.Itoa(r.PID)
		}
		runnerID := r.RunnerID
		if runnerID == "" {
			runnerID = "-"
		}
		fmt.Fprintf(w, "%s\t%s\t%s\t%s\t%s\n", r.Name, status, pid, runnerID, r.LogFile)
	}
	return w.Flush()
}

// brainBinaryPath resolves the brain executable to re-exec for the daemon.
func brainBinaryPath() string {
	if p, err := os.Executable(); err == nil && p != "" {
		return p
	}
	return "brain"
}
