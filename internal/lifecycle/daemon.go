package lifecycle

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"syscall"
)

// Daemonize spawns a detached background process with the given command and arguments.
// Returns the PID of the spawned process.
//
// The process is fully detached:
// - Runs in a new process group
// - Redirects stdout/stderr to log files
// - Closes stdin
// - Continues running after parent exits
//
// The PID is written to opts.PIDFile for later management.
func Daemonize(command string, args []string, opts DaemonOptions) (int, error) {
	// Validate options
	if opts.PIDFile == "" {
		return 0, fmt.Errorf("PIDFile is required")
	}
	if opts.LogFile == "" {
		return 0, fmt.Errorf("LogFile is required")
	}

	// Ensure log directory exists
	logDir := filepath.Dir(opts.LogFile)
	if err := os.MkdirAll(logDir, 0o755); err != nil {
		return 0, fmt.Errorf("failed to create log directory: %w", err)
	}

	// If ErrorLogFile not specified, use LogFile for both stdout and stderr
	errorLogFile := opts.ErrorLogFile
	if errorLogFile == "" {
		errorLogFile = opts.LogFile
	}

	// Create command
	cmd := exec.Command(command, args...)

	// Set working directory
	if opts.WorkDir != "" {
		cmd.Dir = opts.WorkDir
	}

	// Spawn detached process
	pid, err := SpawnDetached(cmd, opts.LogFile, errorLogFile)
	if err != nil {
		return 0, fmt.Errorf("failed to spawn daemon: %w", err)
	}

	// Write PID file
	if err := WritePID(opts.PIDFile, pid); err != nil {
		return 0, fmt.Errorf("failed to write PID file: %w", err)
	}

	return pid, nil
}

// SpawnDetached spawns a command as a detached background process.
// Returns the PID of the spawned process.
//
// The process is detached using:
//   - Setpgid: Creates new process group
//   - Process released after start (don't wait)
//
// Stdout and stderr are redirected to the specified log files.
// Stdin is closed to prevent blocking on input.
//
// Note: On Linux, Setsid can be used for full session detachment,
// but it requires appropriate permissions. This implementation uses
// Setpgid which works reliably across Unix systems including macOS.
func SpawnDetached(cmd *exec.Cmd, stdoutLog, stderrLog string) (int, error) {
	// Open log files
	stdout, err := os.OpenFile(stdoutLog, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o644)
	if err != nil {
		return 0, fmt.Errorf("failed to open stdout log: %w", err)
	}
	defer stdout.Close()

	stderr, err := os.OpenFile(stderrLog, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o644)
	if err != nil {
		return 0, fmt.Errorf("failed to open stderr log: %w", err)
	}
	defer stderr.Close()

	// Redirect stdout and stderr
	cmd.Stdout = stdout
	cmd.Stderr = stderr
	cmd.Stdin = nil // Close stdin

	// Set process group attributes for detachment
	cmd.SysProcAttr = &syscall.SysProcAttr{
		Setpgid: true, // Create new process group
		Pgid:    0,    // Use PID as PGID
	}

	// Start the process
	if err := cmd.Start(); err != nil {
		return 0, fmt.Errorf("failed to start process: %w", err)
	}

	// Get the PID
	pid := cmd.Process.Pid

	// Release the process (don't wait for it)
	go func() {
		cmd.Wait()
	}()

	return pid, nil
}
