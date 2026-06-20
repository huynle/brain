package commands

import (
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"syscall"
	"time"

	"github.com/huynle/brain-api/internal/lifecycle"
)

// RunnerDaemonCommand implements `brain runner <start|stop|status>` — a
// background (daemonized) headless runner that registers with the Brain API and
// claims/executes tasks. This is the "go to a machine and start a runner" path;
// the granular `brain run ...` subcommands are unchanged.
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
	stateHome := os.Getenv("XDG_STATE_HOME")
	if stateHome == "" {
		homeDir, _ := os.UserHomeDir()
		stateHome = filepath.Join(homeDir, ".local", "state")
	}
	return filepath.Join(stateHome, "brain-api", name)
}

func runnerPIDFile() string { return runnerStateFile("brain-runner.pid") }
func runnerLogFile() string { return runnerStateFile("brain-runner.log") }

// start launches the runner. By default it daemonizes (detaches into the
// background); --foreground runs it headless in the current terminal.
func (c *RunnerDaemonCommand) start() error {
	// Foreground: run a headless runner in this process (delegates to `run start`).
	if c.Flags.Foreground {
		rc := &RunCommand{
			Subcommand: "start",
			Project:    c.Project,
			Config:     c.Config,
			Flags:      c.Flags,
		}
		rc.Flags.Headless = true
		return rc.Execute()
	}

	pidFile := runnerPIDFile()
	logFile := runnerLogFile()

	// Already running?
	if pid, err := lifecycle.ReadPID(pidFile); err == nil {
		if lifecycle.IsProcessRunning(pid) {
			return fmt.Errorf("runner already running (PID %d) — stop it with `brain runner stop`", pid)
		}
		_ = lifecycle.ClearPID(pidFile) // stale
	}

	bin := brainBinaryPath()
	args := []string{"run", "start", c.Project, "--headless"}
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
	fmt.Printf("Project(s): %s\n", c.Project)
	fmt.Printf("Logs:       %s\n", logFile)
	fmt.Printf("Stop with:  brain runner stop\n")
	return nil
}

// stop sends SIGTERM to the background runner and waits for it to exit.
func (c *RunnerDaemonCommand) stop() error {
	pidFile := runnerPIDFile()
	pid, err := lifecycle.ReadPID(pidFile)
	if err != nil {
		if os.IsNotExist(err) {
			return fmt.Errorf("runner not running (no PID file)")
		}
		return fmt.Errorf("failed to read runner PID file: %w", err)
	}
	if !lifecycle.IsProcessRunning(pid) {
		_ = lifecycle.ClearPID(pidFile)
		return fmt.Errorf("runner not running (stale PID %d)", pid)
	}

	proc, err := os.FindProcess(pid)
	if err != nil {
		return fmt.Errorf("failed to find runner process: %w", err)
	}
	fmt.Printf("Stopping runner (PID %d)…\n", pid)
	if err := proc.Signal(syscall.SIGTERM); err != nil {
		return fmt.Errorf("failed to signal runner: %w", err)
	}

	deadline := time.Now().Add(15 * time.Second)
	for time.Now().Before(deadline) {
		if !lifecycle.IsProcessRunning(pid) {
			_ = lifecycle.ClearPID(pidFile)
			fmt.Println("Runner stopped")
			return nil
		}
		time.Sleep(200 * time.Millisecond)
	}
	return fmt.Errorf("runner (PID %d) did not stop within 15s", pid)
}

// status reports whether the background runner is alive.
func (c *RunnerDaemonCommand) status() error {
	pidFile := runnerPIDFile()
	pid, err := lifecycle.ReadPID(pidFile)
	if err != nil || !lifecycle.IsProcessRunning(pid) {
		fmt.Println("Runner: not running")
		return nil
	}
	fmt.Printf("Runner: running (PID %d)\n", pid)
	fmt.Printf("Logs:   %s\n", runnerLogFile())
	return nil
}

// brainBinaryPath resolves the brain executable to re-exec for the daemon.
func brainBinaryPath() string {
	if p, err := os.Executable(); err == nil && p != "" {
		return p
	}
	return "brain"
}
